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
//	history  连接建立时补发的历史日志
//	log      增量日志（data 是原始字节片段，可能不含完整行）
//	status   状态/步骤变更
//	exit     运行结束（随后服务端关闭连接）
//	error    服务端错误
//
// 每帧都带 nextOffset。客户端断线后用 `?...&offset=<nextOffset>` 重连即可无缝续上，
// 这是「刷新页面/断网后不丢日志」的实现方式。
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
		var p store.Project
		if err := s.st.DB().First(&p, run.ProjectID).Error; err == nil && p.OwnerID != u.ID {
			NotFound(c, "run not found")
			return
		}
	}

	offset, _ := strconv.ParseInt(c.DefaultQuery("offset", "0"), 10, 64)
	if offset < 0 {
		offset = 0
	}
	logPath := runlog.PathFor(s.cfg.LogDir(), run.ProjectKey, run.ID)

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

	sendLog := func(kind string, data []byte, next int64) bool {
		if len(data) == 0 {
			sent = next
			return true
		}
		seq++
		if err := writeFrame(map[string]any{
			"type": kind, "runId": runID, "seq": seq,
			"data": string(data), "nextOffset": next,
		}); err != nil {
			return false
		}
		sent = next
		return true
	}

	// drainFile 把文件里从 sent 开始的内容全部补发。
	drainFile := func(kind string) bool {
		for {
			data, next, eof, err := runlog.ReadRange(logPath, sent, 128*1024)
			if err != nil {
				_ = writeFrame(map[string]any{
					"type": "error", "runId": runID, "message": "failed to read the log: " + err.Error(),
				})
				return false
			}
			if !sendLog(kind, data, next) {
				return false
			}
			if eof || next <= sent {
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
				// 两种情况都先把文件补完再发 exit；客户端如需继续会带
				// nextOffset 重连，不会丢数据。
				if !drainFile("log") {
					return
				}
				final := currentStatus()
				sendExit(final)
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
			if !sendLog("log", data, off+int64(len(data))) {
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
