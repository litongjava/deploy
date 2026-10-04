package ws

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"deploy-cli/internal/api"
	"deploy-cli/internal/exitcode"
)

// DefaultChunkLimit 是单次 HTTP 拉取历史日志的字节上限。
const DefaultChunkLimit = 65536

// LogSource 是 HTTP 侧的日志与运行状态来源（*api.Client 实现）。
type LogSource interface {
	RunLog(ctx context.Context, id, offset int64, limit int) (*api.LogChunk, error)
	GetRun(ctx context.Context, id int64) (*api.Run, error)
}

// StreamSource 建立实时日志流（*Dialer 实现）。
type StreamSource interface {
	StreamLogs(ctx context.Context, runID, offset int64) (Conn, error)
}

// Options 控制一次日志跟随。
type Options struct {
	RunID  int64
	Offset int64 // 起始字节偏移，通常为 0
	Limit  int   // 单次 HTTP 拉取上限，0 → DefaultChunkLimit
	Writer io.Writer

	// Verbose 用于输出重连等诊断信息（通常接 stderr）。
	Verbose func(format string, args ...any)
	// OnStatus 在收到 status 帧时回调（可为 nil）。
	OnStatus func(status string, step *Step)

	// MaxRetries <= 0 表示不限次数（靠 ctx 取消退出）。
	MaxRetries int
	MinBackoff time.Duration
	MaxBackoff time.Duration

	// Sleep 可注入，便于测试；nil → 带 ctx 的定时等待。
	Sleep func(ctx context.Context, d time.Duration) error
}

// Result 是一次跟随的最终结论。
type Result struct {
	RunID       int64
	BuildNumber int // 项目内构建号（第几次部署），0 表示未知
	FinalOffset int64
	Status      string
	ExitCode    *int
	DurationMs  int64
	Terminated  bool // 是否拿到了终态（exit 帧或运行状态已结束）
	Interrupted bool // 是否因为 ctx 取消（Ctrl+C）而停止
	Reconnects  int
	LastError   string
}

// ProcessExitCode 把跟随结果映射为 CLI 退出码：
// success → 0；其它终态（failed/timeout/error/canceled）→ 1；未结束 → 0。
func (r *Result) ProcessExitCode() int {
	if !r.Terminated {
		return exitcode.OK
	}
	if r.Status == api.StatusSuccess {
		return exitcode.OK
	}
	return exitcode.DeployFailed
}

// Follower 组合 HTTP 与 WebSocket 两个来源实现「不丢日志、不重复刷屏」。
type Follower struct {
	Logs   LogSource
	Stream StreamSource
}

// NewFollower 构造 Follower。
func NewFollower(logs LogSource, stream StreamSource) *Follower {
	return &Follower{Logs: logs, Stream: stream}
}

// Follow 执行一次完整的日志跟随。
//
// 流程：
//  1. 先用 GET /runs/{id}/log 从 offset 增量拉取历史，直到 eof；
//  2. 若运行已结束则直接给出结论（不建连接）；
//  3. 否则连 WebSocket /ws/runs/{id}?offset=nextOffset 读增量帧；
//  4. 连接断开则退避重连，重连前重新用 HTTP 按 nextOffset 补齐，
//     因此不会丢日志；服务端从更早位置重发时按 offset 裁掉重复前缀。
func (f *Follower) Follow(ctx context.Context, opts Options) (*Result, error) {
	if opts.Limit <= 0 {
		opts.Limit = DefaultChunkLimit
	}
	if opts.Writer == nil {
		opts.Writer = io.Discard
	}
	if opts.MinBackoff <= 0 {
		opts.MinBackoff = 500 * time.Millisecond
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = 5 * time.Second
	}
	if opts.Sleep == nil {
		opts.Sleep = sleepCtx
	}
	logf := opts.Verbose
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if f.Logs == nil {
		return nil, exitcode.Netf("缺少日志来源")
	}

	res := &Result{RunID: opts.RunID, FinalOffset: opts.Offset}
	offset := opts.Offset
	backoff := opts.MinBackoff
	failures := 0

	for {
		if err := ctx.Err(); err != nil {
			res.Interrupted = true
			return res, err
		}

		// 1) HTTP 补齐历史（增量直到 eof）
		next, err := f.catchUp(ctx, opts, offset, logf)
		if err != nil {
			if ctx.Err() != nil {
				res.Interrupted = true
				return res, ctx.Err()
			}
			if !retriable(err) {
				return res, err
			}
			failures++
			res.LastError = err.Error()
			if opts.MaxRetries > 0 && failures > opts.MaxRetries {
				return res, exitcode.Wrap(exitcode.Network, err, "拉取历史日志连续失败 %d 次", failures)
			}
			logf("拉取历史日志失败（%v），%s 后重试", err, backoff)
			if werr := opts.Sleep(ctx, backoff); werr != nil {
				res.Interrupted = true
				return res, werr
			}
			backoff = nextBackoff(backoff, opts.MaxBackoff)
			continue
		}
		offset = next
		res.FinalOffset = offset
		failures = 0
		backoff = opts.MinBackoff

		// 2) 运行是否已经结束？
		run, err := f.Logs.GetRun(ctx, opts.RunID)
		switch {
		case err == nil:
			if api.IsTerminalStatus(run.Status) {
				// 终态后再补一次，确保 exit 之后写入的日志也被取回。
				if off, cerr := f.catchUp(ctx, opts, offset, logf); cerr == nil {
					offset = off
					res.FinalOffset = offset
				}
				res.Status = run.Status
				res.ExitCode = run.ExitCode
				res.DurationMs = run.DurationMs
				res.BuildNumber = run.BuildNumber
				res.Terminated = true
				return res, nil
			}
		case !retriable(err):
			return res, err
		}

		// 3) 连 WebSocket
		conn, err := f.Stream.StreamLogs(ctx, opts.RunID, offset)
		if err != nil {
			if ctx.Err() != nil {
				res.Interrupted = true
				return res, ctx.Err()
			}
			if !retriable(err) {
				return res, err
			}
			failures++
			res.Reconnects++
			res.LastError = err.Error()
			if opts.MaxRetries > 0 && failures > opts.MaxRetries {
				return res, exitcode.Wrap(exitcode.Network, err, "WebSocket 连续重连失败 %d 次", failures)
			}
			logf("WebSocket 连接失败（%v），%s 后重试", err, backoff)
			if werr := opts.Sleep(ctx, backoff); werr != nil {
				res.Interrupted = true
				return res, werr
			}
			backoff = nextBackoff(backoff, opts.MaxBackoff)
			continue
		}

		// 4) 读帧
		done, status, code, dur, err := readStream(ctx, conn, opts, res, &offset, logf)
		if done {
			res.Status = status
			res.ExitCode = code
			res.DurationMs = dur
			res.Terminated = true
			res.FinalOffset = offset
			return res, nil
		}
		if ctx.Err() != nil {
			res.Interrupted = true
			return res, ctx.Err()
		}
		if err != nil && !retriable(err) {
			return res, fmt.Errorf("读取日志流失败: %w", err)
		}
		failures++
		res.Reconnects++
		if err != nil {
			res.LastError = err.Error()
		}
		if opts.MaxRetries > 0 && failures > opts.MaxRetries {
			return res, exitcode.Wrap(exitcode.Network, err, "日志流连续断开 %d 次", failures)
		}
		logf("日志流已断开（%v），%s 后从 offset=%d 重连", err, backoff, offset)
		if werr := opts.Sleep(ctx, backoff); werr != nil {
			res.Interrupted = true
			return res, werr
		}
		backoff = nextBackoff(backoff, opts.MaxBackoff)
	}
}

// FetchHistory 只做一次 HTTP 历史补齐（deploy logs 不带 -f 时使用），
// 从 opts.Offset 开始增量拉取直到 eof，返回新的 offset。
func FetchHistory(ctx context.Context, logs LogSource, opts Options) (int64, error) {
	if opts.Limit <= 0 {
		opts.Limit = DefaultChunkLimit
	}
	if opts.Writer == nil {
		opts.Writer = io.Discard
	}
	f := &Follower{Logs: logs}
	return f.catchUp(ctx, opts, opts.Offset, logfOf(opts))
}

func logfOf(opts Options) func(string, ...any) {
	if opts.Verbose != nil {
		return opts.Verbose
	}
	return func(string, ...any) {}
}

// catchUp 用 HTTP 增量拉取日志直到 eof，返回新的 offset。
func (f *Follower) catchUp(ctx context.Context, opts Options, offset int64, logf func(string, ...any)) (int64, error) {
	stalls := 0
	for {
		if err := ctx.Err(); err != nil {
			return offset, err
		}
		chunk, err := f.Logs.RunLog(ctx, opts.RunID, offset, opts.Limit)
		if err != nil {
			return offset, err
		}
		if chunk == nil {
			return offset, nil
		}
		if text := dedup(chunk.Data, chunk.Offset, offset); text != "" {
			if _, werr := io.WriteString(opts.Writer, text); werr != nil {
				return offset, werr
			}
		}
		if chunk.NextOffset > offset {
			offset = chunk.NextOffset
			stalls = 0
		} else {
			stalls++
		}
		if chunk.EOF {
			return offset, nil
		}
		if stalls >= 3 {
			// 服务端既没有新数据也没有推进 offset：不再空转，交给上层重连/轮询。
			logf("日志无新增（offset=%d），暂停补齐", offset)
			return offset, nil
		}
	}
}

// readStream 读取 WebSocket 帧。
//
// 返回 done=true 表示收到 exit 帧（终态已确定）。
func readStream(ctx context.Context, conn Conn, opts Options, res *Result, offset *int64, logf func(string, ...any)) (done bool, status string, code *int, dur int64, err error) {
	// ctx 取消时主动关闭连接，让阻塞的 ReadFrame 返回。
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-stop:
		}
	}()
	defer conn.Close()

	for {
		frame, rerr := conn.ReadFrame()
		if rerr != nil {
			return false, "", nil, 0, rerr
		}
		if frame == nil {
			continue
		}
		switch frame.Type {
		case FrameHistory, FrameLog:
			start := frame.NextOffset - int64(len(frame.Data))
			if text := dedup(frame.Data, start, *offset); text != "" {
				if _, werr := io.WriteString(opts.Writer, text); werr != nil {
					return false, "", nil, 0, werr
				}
			}
			if frame.NextOffset > *offset {
				*offset = frame.NextOffset
			}
			res.FinalOffset = *offset
		case FrameStatus:
			if opts.OnStatus != nil {
				opts.OnStatus(frame.Status, frame.Step)
			}
			logf("运行状态: %s", frame.Status)
		case FrameExit:
			if frame.NextOffset > *offset {
				*offset = frame.NextOffset
			}
			return true, frame.Status, frame.ExitCode, frame.DurationMs, nil
		case FrameError:
			res.LastError = frame.Message
			logf("服务端推送错误帧: %s", frame.Message)
		default:
			logf("忽略未知帧类型 %q", frame.Type)
		}
	}
}

// dedup 裁掉服务端从更早位置重发造成的重复前缀。
//
// start 是该块数据在日志文件中的起始偏移（由 nextOffset - len(data) 推得），
// known 是我们已经输出到的位置。start < known 时说明有重复。
func dedup(data string, start, known int64) string {
	if start < 0 || known <= start {
		return data
	}
	skip := known - start
	if skip >= int64(len(data)) {
		return ""
	}
	return data[skip:]
}

func nextBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next > max {
		next = max
	}
	return next
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// retriable 判断错误是否值得重连重试。
//
// 契约层的确定性失败（401/403/404/400 等）不重试；传输失败、5xx、限流、
// 连接被关闭等一律重试，直到 ctx 取消或运行进入终态。
func retriable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if e, ok := api.AsError(err); ok {
		return e.IsRetriable()
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	var oe *net.OpError
	if errors.As(err, &oe) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	// WebSocket 正常/异常关闭（gorilla CloseError）以及其它临时故障：继续重试。
	return true
}
