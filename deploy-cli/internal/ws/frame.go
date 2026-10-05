// Package ws 实现契约 7.4 的实时日志 WebSocket 客户端，以及
// 「HTTP 历史补齐 + WebSocket 增量 + 断线按 offset 续拉」的跟随逻辑。
package ws

// 帧类型常量（契约 7.4）。
const (
	FrameHistory   = "history"
	FrameLog       = "log"
	FrameStatus    = "status"
	FrameExit      = "exit"
	FrameError     = "error"
	FrameTruncated = "truncated"
)

// Step 是 status 帧里的当前步骤。
type Step struct {
	Name      string `json:"name"`
	SortOrder int    `json:"sortOrder"`
}

// Frame 是服务端 → 客户端的 JSON 文本帧。
//
//	data 是原始字节片段（可能不含完整行，前端自行按 \n 缓冲切分），
//	日志保留 ANSI 转义序列。
type Frame struct {
	Type  string `json:"type"`
	RunID int64  `json:"runId"`
	Data  string `json:"data"`
	Seq   int64  `json:"seq"`
	// Offset 是本帧数据在日志文件里的起始字节偏移。
	// 老版本服务端不发这个字段（nil），此时退回 nextOffset - len(data)。
	Offset     *int64 `json:"offset"`
	NextOffset int64  `json:"nextOffset"`
	Status     string `json:"status"`
	ExitCode   *int   `json:"exitCode"`
	DurationMs int64  `json:"durationMs"`
	Message    string `json:"message"`
	Step       *Step  `json:"step"`
}

// StartOffset 给出本帧数据在日志文件里的起始偏移。
func (f *Frame) StartOffset() int64 {
	if f.Offset != nil {
		return *f.Offset
	}
	return f.NextOffset - int64(len(f.Data))
}
