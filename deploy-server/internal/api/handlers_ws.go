package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"deploy-server/internal/runlog"
	"deploy-server/internal/store"
)

// upgrader 的 CheckOrigin 直接放行。
//
// 安全性说明：本服务的凭据是 localStorage 里的 token，靠 URL query 传给 WS，
// 浏览器不会跨站自动携带它；因此不存在「恶意页面借用户 Cookie 连上来」的风险。
// 真正把关的是下面的 token 校验。
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 32 * 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// resolveAnyToken 同时接受 JWT 与 API Token。
func (s *Server) resolveAnyToken(raw string) (*store.User, error) {
	if raw == "" {
		return nil, errors.New("missing token")
	}
	if strings.HasPrefix(raw, "dk_") {
		return s.auth.userByAPIToken(raw)
	}
	claims, err := s.auth.Parse(raw)
	if err != nil {
		return nil, err
	}
	u, err := findUserByID(s.st, claims.UserID)
	if err != nil {
		return nil, err
	}
	if u.Status != "active" {
		return nil, errors.New("account disabled")
	}
	return u, nil
}

// runLogWS 推送某个 Run 的实时日志。
//
// 帧协议见 docs/API设计.md §7.4：
//
//	history    连接建立时补发的历史日志
//	log        增量日志（data 是原始字节片段，可能不含完整行）
//	status     状态/步骤变更
//	truncated  本连接被截断（订阅者读得太慢被踢），运行未结束，需带 nextOffset 重连
//	exit       运行已结束（随后服务端关闭连接）
//	error      服务端错误
//
// 每帧都带 offset（本帧起始字节偏移）与 nextOffset。客户端断线后用
// `?...&offset=<nextOffset>` 重连即可无缝续上，这是「刷新页面/断网后不丢日志」的实现方式。
// 注意 exit 帧只在运行真正终态时发：它一旦发错，客户端就会永久停流。
func (s *Server) runLogWS(c *gin.Context) {
	u, err := s.resolveAnyToken(c.Query("token"))
	if err != nil {
		c.JSON(http.StatusUnauthorized, Envelope{
			Success: false, ErrorCode: CodeUnauthorized,
			ErrorMessage: "not logged in or session expired", ShowType: 0,
		})
		return
	}

	id64, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id64 == 0 {
		BadRequest(c, "invalid run ID")
		return
	}
	runID := uint(id64)

	var run store.Run
	if err := s.st.DB().First(&run, runID).Error; err != nil {
		NotFound(c, "run not found")
		return
	}
	if u.Role != "admin" && s.visibilityMode() == "ownerOnly" {
		if run.TargetKind == store.TargetTask {
			var t store.Task
			if err := s.st.DB().First(&t, run.TaskID).Error; err == nil && t.OwnerID != u.ID {
				NotFound(c, "run not found")
				return
			}
		} else {
			var p store.Project
			if err := s.st.DB().First(&p, run.ProjectID).Error; err == nil && p.OwnerID != u.ID {
				NotFound(c, "run not found")
				return
			}
		}
	}

	offset, _ := strconv.ParseInt(c.DefaultQuery("offset", "0"), 10, 64)
	if offset < 0 {
		offset = 0
	}
	logPath := s.logPathOf(&run)

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// Upgrade 失败时它已经写过响应了，这里只记日志。
		slog.Debug("websocket upgrade failed", "err", err, "run", runID)
		return
	}
	defer conn.Close()

	// 先订阅、再读历史：反过来的话，两步之间新产生的日志会永久丢失。
	// 重叠部分靠 chunk 自带的 offset 去重。
	sub := s.exec.Broker().Subscribe(runID)
	defer s.exec.Broker().Unsubscribe(runID, sub)

	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	sent := offset
	seq := 0

	writeFrame := func(v any) error {
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(v)
	}

	// sendLog 发一个日志帧。start 是这段数据在日志文件里的起始字节偏移：
	// 必须显式带给客户端，否则客户端只能用「nextOffset - data.length」去猜，
	// 而 JS 的 string.length 是 UTF-16 码元数、日志偏移是字节数，
	// 一旦日志里有中文/emoji 就会算歪，去重时把不该裁的内容裁掉（表现为日志缺行）。
	sendLog := func(kind string, data []byte, start, next int64) bool {
		if len(data) == 0 {
			sent = next
			return true
		}
		seq++
		if err := writeFrame(map[string]any{
			"type": kind, "runId": runID, "seq": seq,
			"offset": start, "data": string(data), "nextOffset": next,
		}); err != nil {
			return false
		}
		sent = next
		return true
	}

	// drainFile 把文件里从 sent 开始的内容**全部**补发（一块 128KB，直到 EOF）。
	//
	// 注意这里的 `from`：sendLog 成功后会推进 sent，所以「有没有读到新数据」
	// 只能拿本次读取的起始位置 from 去比。早先写成 `next <= sent` 时，
	// 因为 sent 已经被赋成 next，条件恒为真 —— 循环只发一块就返回，
	// 于是任何超过 128KB 的日志在补历史/重连/收尾时都会被截断。
	drainFile := func(kind string) bool {
		for {
			from := sent
			data, next, eof, err := runlog.ReadRange(logPath, from, 128*1024)
			if err != nil {
				_ = writeFrame(map[string]any{
					"type": "error", "runId": runID, "message": "failed to read the log: " + err.Error(),
				})
				return false
			}
			if !sendLog(kind, data, next-int64(len(data)), next) {
				return false
			}
			if eof || next <= from {
				return true
			}
		}
	}

	currentStatus := func() store.Run {
		var cur store.Run
		_ = s.st.DB().First(&cur, runID).Error
		return cur
	}

	sendStatus := func(cur store.Run) bool {
		var steps []store.RunStep
		s.st.DB().Where("run_id = ?", runID).Order("phase asc, sort_order asc").Find(&steps)
		return writeFrame(map[string]any{
			"type": "status", "runId": runID, "status": cur.Status,
			"steps": func() []stepDTO {
				out := make([]stepDTO, 0, len(steps))
				for _, st := range steps {
					out = append(out, toStepDTO(st))
				}
				return out
			}(),
		}) == nil
	}

	sendExit := func(cur store.Run) {
		_ = writeFrame(map[string]any{
			"type": "exit", "runId": runID, "status": cur.Status,
			"exitCode": cur.ExitCode, "durationMs": cur.DurationMs,
			"errorMsg": cur.ErrorMsg, "nextOffset": sent,
		})
	}

	// finishStream 是「本连接到此为止」的唯一收尾逻辑：
	// 先把文件里剩下的内容补完，再判断运行是否真的结束了。
	//
	// exit 帧的语义是「运行已结束，客户端可以收工了」——客户端收到它就
	// 不再重连。所以只有运行确实处于终态才能发；本订阅者因为读得慢被
	// 服务端踢掉时（见 runlog.Broker.Publish）绝不能发 exit，
	// 否则客户端会永久停流，表现就是「运行结束后日志不全」。
	finishStream := func() {
		if !drainFile("log") {
			return
		}
		cur := currentStatus()
		if isFinalStatus(cur.Status) {
			sendExit(cur)
			return
		}
		_ = writeFrame(map[string]any{
			"type": "truncated", "runId": runID, "nextOffset": sent,
			"status": cur.Status, "reason": "subscriber too slow",
		})
	}

	// 先把已有日志补发一遍：既覆盖「运行早就结束、现在才打开页面」，
	// 也覆盖「刷新页面后重连」。
	if !drainFile("history") {
		return
	}

	cur := currentStatus()
	if !sendStatus(cur) {
		return
	}
	if isFinalStatus(cur.Status) {
		sendExit(cur)
		return
	}

	ticker := time.NewTicker(700 * time.Millisecond)
	defer ticker.Stop()

	lastStatus := cur.Status
	for {
		select {
		case <-closed:
			return

		case chunk, ok := <-sub.Ch:
			if !ok {
				// 订阅被关闭：执行已结束，或本订阅者跟不上被服务端断开。
				// finishStream 会区分这两种情况（后者发 truncated，不发 exit）。
				finishStream()
				return
			}
			// 去重：这个 chunk 的起始位置可能已经通过 history 发过了。
			end := chunk.Offset + int64(len(chunk.Data))
			if end <= sent {
				continue
			}
			data, off := chunk.Data, chunk.Offset
			if off < sent {
				data = data[sent-off:]
				off = sent
			}
			if !sendLog("log", data, off, off+int64(len(data))) {
				return
			}

		case <-ticker.C:
			cur = currentStatus()
			if cur.Status != lastStatus {
				lastStatus = cur.Status
				if !sendStatus(cur) {
					return
				}
			}
			if isFinalStatus(cur.Status) {
				// 执行器保证「日志写完」早于「状态落库」，所以这里直接补完即可。
				if !drainFile("log") {
					return
				}
				sendExit(cur)
				return
			}
			// 心跳，防止中间的代理把空闲连接掐掉。
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func isFinalStatus(s string) bool {
	switch s {
	case store.RunSuccess, store.RunFailed, store.RunCanceled, store.RunTimeout, store.RunError:
		return true
	}
	return false
}
