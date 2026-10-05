package api

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"deploy-server/internal/runlog"
	"deploy-server/internal/store"
)

// chattyScript 造一个「短时间输出海量行」的脚本，用于制造服务端推送背压。
func chattyScript(lines int) string {
	if runtime.GOOS == "windows" {
		// 一次性拼成一个大字符串再输出：比逐行 Write-Output 快一个数量级，
		// 才能在几秒内把 socket 缓冲区填满。
		return fmt.Sprintf(`(1..%d | ForEach-Object { "log line $_" }) -join "%s"`,
			lines, "`n")
	}
	return fmt.Sprintf(`seq 1 %d | sed 's/^/log line /'`, lines)
}

// wideScript 输出超过一块（128KB）的**中文**日志：既验证补历史要循环读完整文件，
// 也验证帧里带的 offset 是字节偏移（中文一个字 3 字节，码元数对不上）。
func wideScript(lines int) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf(`(1..%d | ForEach-Object { "中文日志行 $_ — 字节偏移必须按 UTF-8 字节算" }) -join "%s"`,
			lines, "`n")
	}
	return fmt.Sprintf(`seq 1 %d | sed 's/^/中文日志行 /'`, lines)
}

// wsReadResult 记录一次 WebSocket 读取的观察结果。
type wsReadResult struct {
	log        strings.Builder
	exitSeen   bool
	exitStatus string
	// violation 记录协议违例：只有运行真正结束才能发 exit 帧。
	violation string
}

// TestWebSocketHistoryDrainsWholeFile 验证「运行早已结束，现在才连上来」时，
// 服务端必须把整个日志文件补完再发 exit。
//
// 这条用例专门锁死 drainFile 的循环条件：曾经写成 `next <= sent`，
// 而 sent 在 sendLog 里已经被赋成 next，条件恒为真 —— 只补一块 128KB 就收尾，
// 于是所有超过 128KB 的日志在补历史/重连/收尾时都被截断
// （前端表现就是「运行结束后日志不全」）。
func TestWebSocketHistoryDrainsWholeFile(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	const lines = 6000 // 每行约 80 字节 → 约 480KB，远超一块 128KB
	e.call(t, "POST", "/api/v1/projects", e.token, map[string]any{
		"key": "wideapp", "name": "大日志", "workdir": "${WORKSPACE_ROOT}/wideapp",
		"commands": []map[string]any{{"name": "中文输出", "script": wideScript(lines)}},
	})
	_, out := e.call(t, "POST", "/api/v1/projects/wideapp/run", e.token, nil)
	runID := int(dataMap(t, out)["runId"].(float64))
	e.waitRun(t, runID, 120*time.Second)

	// 运行已经结束，此时才连上来：只能靠 drainFile 补历史。
	wsURL := "ws" + strings.TrimPrefix(e.ts.URL, "http") +
		fmt.Sprintf("/api/v1/ws/runs/%d?token=%s&offset=0", runID, e.token)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("建立 WebSocket 失败: %v", err)
	}
	defer conn.Close()

	var got strings.Builder
	chunks := 0
	sawExit := false
	exitStatus := ""
	deadline := time.Now().Add(60 * time.Second)
	for !sawExit && time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}
		frame := map[string]any{}
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatalf("帧不是 JSON: %s", string(raw))
		}
		switch frame["type"] {
		case "history", "log":
			data, _ := frame["data"].(string)
			next, _ := frame["nextOffset"].(float64)
			start, ok := frame["offset"].(float64)
			if !ok {
				t.Fatalf("%v 帧必须带 offset（起始字节偏移），否则客户端只能靠 string.length 猜", frame["type"])
			}
			if int64(start)+int64(len(data)) != int64(next) {
				t.Fatalf("帧的 offset/nextOffset 与数据长度不自洽: offset=%v next=%v len=%d",
					start, next, len(data))
			}
			chunks++
			got.WriteString(data)
		case "exit":
			sawExit = true
			exitStatus, _ = frame["status"].(string)
		}
	}

	if !sawExit {
		t.Fatal("没有收到 exit 帧")
	}
	if exitStatus != store.RunSuccess {
		t.Fatalf("exit 帧状态应为 success，实际 %q", exitStatus)
	}
	if chunks < 2 {
		t.Errorf("补历史只发了 %d 块，说明没有循环读完整文件（每块上限 128KB）", chunks)
	}
	raw, err := os.ReadFile(runlog.PathFor(e.srv.cfg.LogDir(), "wideapp", uint(runID)))
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != string(raw) {
		t.Errorf("补历史内容被截断：客户端 %d 字节，文件 %d 字节", got.Len(), len(raw))
	}
}

// TestWebSocketSlowSubscriberKeepsFullLog 复现「客户端读得慢 → 服务端把订阅者踢掉」时
// 会不会提前发 exit 帧、把日志截断。
//
// 这条用例里的测试客户端严格按 docs/API设计.md §7.4 的契约实现：
// 带 offset 断线重连，直到收到**运行已结束**的 exit 帧为止。
// 只要服务端在运行中途发 exit（哪怕是订阅者跟不上被踢），客户端就会永久停止补日志，
// 表现就是「运行结束后日志不全」。
func TestWebSocketSlowSubscriberKeepsFullLog(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实进程输出，-short 下跳过")
	}
	e := newTestEnv(t)
	e.mustBootstrap(t)

	const lines = 60000
	e.call(t, "POST", "/api/v1/projects", e.token, map[string]any{
		"key": "slowapp", "name": "背压测试", "workdir": "${WORKSPACE_ROOT}/slowapp",
		"commands": []map[string]any{
			{"name": "海量输出", "script": chattyScript(lines)},
		},
	})
	_, out := e.call(t, "POST", "/api/v1/projects/slowapp/run", e.token, nil)
	runID := int(dataMap(t, out)["runId"].(float64))

	res := &wsReadResult{}
	offset := int64(0)
	deadline := time.Now().Add(120 * time.Second)
	attempts := 0
	firstConn := true

	for !res.exitSeen && time.Now().Before(deadline) && attempts < 30 {
		attempts++
		wsURL := "ws" + strings.TrimPrefix(e.ts.URL, "http") +
			fmt.Sprintf("/api/v1/ws/runs/%d?token=%s&offset=%d", runID, e.token, offset)
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("建立 WebSocket 失败: %v", err)
		}
		if firstConn {
			// 模拟「浏览器忙于渲染/网络慢」：先晾着不读，让服务端的
			// socket 缓冲区与 Broker 的 256 深缓冲都被填满。
			firstConn = false
			time.Sleep(3 * time.Second)
		}

		closed := false
		for !closed && !res.exitSeen && time.Now().Before(deadline) {
			_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
			_, raw, err := conn.ReadMessage()
			if err != nil {
				closed = true
				break
			}
			frame := map[string]any{}
			if err := json.Unmarshal(raw, &frame); err != nil {
				t.Fatalf("帧不是 JSON: %s", string(raw))
			}
			switch frame["type"] {
			case "history", "log":
				data, _ := frame["data"].(string)
				next, _ := frame["nextOffset"].(float64)
				if len(data) == 0 {
					continue
				}
				start := int64(next) - int64(len(data))
				skip := offset - start
				if skip >= int64(len(data)) {
					continue
				}
				if skip > 0 {
					data = data[skip:]
				}
				res.log.WriteString(data)
				// 服务端的 nextOffset 是**字节**偏移，Go 里 len(string) 也是字节数，
				// 所以这里的续传 offset 永远精确。
				offset = int64(next)
			case "truncated":
				// 服务端承认本连接被截断：按契约带 offset 重连，不算结束。
			case "exit":
				res.exitSeen = true
				res.exitStatus, _ = frame["status"].(string)
				if !isFinalStatus(res.exitStatus) {
					// 运行还在跑却发 exit：客户端会据此永久停流（当前实现的真实缺陷）。
					res.violation = "exit 帧在运行结束前发出，status=" + res.exitStatus
				}
			}
		}
		_ = conn.Close()
		if !res.exitSeen {
			time.Sleep(200 * time.Millisecond)
		}
	}

	if !res.exitSeen {
		t.Fatal("重连若干次后仍未收到 exit 帧")
	}
	if res.violation != "" {
		t.Errorf("协议违例：%s", res.violation)
	}

	final := e.waitRun(t, runID, 60*time.Second)
	if res.exitStatus != final["status"] {
		t.Errorf("exit 帧状态 %q 与最终状态 %v 不一致", res.exitStatus, final["status"])
	}

	// 客户端按协议补齐后的内容必须与磁盘上的日志逐字节一致（不丢也不重）。
	raw, err := os.ReadFile(runlog.PathFor(e.srv.cfg.LogDir(), "slowapp", uint(runID)))
	if err != nil {
		t.Fatal(err)
	}
	got := res.log.String()
	if got != string(raw) {
		t.Errorf("日志流内容与文件不一致：客户端 %d 字节，文件 %d 字节\n"+
			"客户端尾部：%q\n文件尾部：%q",
			len(got), len(raw), tail(got, 120), tail(string(raw), 120))
	}
	if !strings.Contains(got, fmt.Sprintf("log line %d", lines)) {
		t.Error("日志尾部缺失：最后一行没有到达客户端")
	}
	if res.exitStatus != store.RunSuccess {
		t.Errorf("运行应成功，实际 %q（%v）", res.exitStatus, final["errorMsg"])
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
