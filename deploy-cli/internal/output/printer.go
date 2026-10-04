package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// ANSI 颜色码。
const (
	cReset   = "\x1b[0m"
	cBold    = "\x1b[1m"
	cDim     = "\x1b[2m"
	cRed     = "\x1b[31m"
	cGreen   = "\x1b[32m"
	cYellow  = "\x1b[33m"
	cBlue    = "\x1b[34m"
	cMagenta = "\x1b[35m"
	cCyan    = "\x1b[36m"
)

// Printer 统一处理 stdout 的机器可读输出与 stderr 的人类提示。
//
// 约定（验收项）：
//   - --json 时 stdout 只出现 JSON，人类可读的提示一律走 stderr；
//   - 非 TTY / --no-color / NO_COLOR=1 时不产生任何颜色。
type Printer struct {
	Out     io.Writer
	Err     io.Writer
	JSON    bool
	Color   bool
	Verbose bool
}

// New 构造 Printer。out 用于数据输出，errw 用于提示、警告与调试信息。
func New(out, errw io.Writer, jsonMode, noColor, verbose bool) *Printer {
	if out == nil {
		out = io.Discard
	}
	if errw == nil {
		errw = io.Discard
	}
	color := false
	if !jsonMode && !noColor && os.Getenv("NO_COLOR") == "" {
		color = isTerminal(out)
	}
	return &Printer{Out: out, Err: errw, JSON: jsonMode, Color: color, Verbose: verbose}
}

func isTerminal(w io.Writer) bool {
	type fder interface{ Fd() uintptr }
	f, ok := w.(fder)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd())) //nolint:gosec // fd 来自 os.File
}

// ColorEnabled 报告当前是否会输出颜色。
func (p *Printer) ColorEnabled() bool { return p != nil && p.Color }

func (p *Printer) wrap(code, s string) string {
	if !p.ColorEnabled() {
		return s
	}
	return code + s + cReset
}

// 各种颜色包装；未启用颜色时原样返回。
func (p *Printer) Bold(s string) string    { return p.wrap(cBold, s) }
func (p *Printer) Dim(s string) string     { return p.wrap(cDim, s) }
func (p *Printer) Red(s string) string     { return p.wrap(cRed, s) }
func (p *Printer) Green(s string) string   { return p.wrap(cGreen, s) }
func (p *Printer) Yellow(s string) string  { return p.wrap(cYellow, s) }
func (p *Printer) Blue(s string) string    { return p.wrap(cBlue, s) }
func (p *Printer) Magenta(s string) string { return p.wrap(cMagenta, s) }
func (p *Printer) Cyan(s string) string    { return p.wrap(cCyan, s) }

// Printf 写 stdout（人类可读结果）。
func (p *Printer) Printf(format string, args ...any) {
	fmt.Fprintf(p.Out, format, args...)
}

// Println 写 stdout 并换行。
func (p *Printer) Println(args ...any) {
	fmt.Fprintln(p.Out, args...)
}

// Infof 写 stderr（提示类信息，不污染 stdout）。
func (p *Printer) Infof(format string, args ...any) {
	fmt.Fprintf(p.Err, format+"\n", args...)
}

// Warnf 写 stderr 并加 [警告] 前缀。
func (p *Printer) Warnf(format string, args ...any) {
	fmt.Fprintf(p.Err, "%s %s\n", p.Yellow("[警告]"), fmt.Sprintf(format, args...))
}

// Errorf 写 stderr 并加 [错误] 前缀。
func (p *Printer) Errorf(format string, args ...any) {
	fmt.Fprintf(p.Err, "%s %s\n", p.Red("[错误]"), fmt.Sprintf(format, args...))
}

// Verbosef 仅在 --verbose 时写 stderr。
func (p *Printer) Verbosef(format string, args ...any) {
	if p == nil || !p.Verbose {
		return
	}
	fmt.Fprintf(p.Err, "%s %s\n", p.Dim("[debug]"), fmt.Sprintf(format, args...))
}

// JSONOut 把 v 以缩进 JSON 写到 stdout；失败时返回错误。
func (p *Printer) JSONOut(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 JSON 失败: %w", err)
	}
	if _, err := p.Out.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

// JSONLine 把 v 以单行 JSON 写到 stdout。
// 用于需要与 JSONL 日志事件混排的场景（deploy run -f --json），
// 保证 stdout 每行都是一个完整的 JSON 值。
func (p *Printer) JSONLine(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("序列化 JSON 失败: %w", err)
	}
	if _, err := p.Out.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

// String 输出一段已经渲染好的文本（表格等）。
func (p *Printer) String(s string) {
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	io.WriteString(p.Out, s)
}

// LogWriter 返回用于写运行日志的 writer：
//   - --json：每块日志包成一行 JSON（{"type":"log","data":"..."}），保证 stdout 是 JSONL；
//   - 有颜色：原样透传（保留 ANSI）；
//   - 其余情况：剥离 ANSI（并正确处理跨块被切分的转义序列）。
func (p *Printer) LogWriter() io.Writer {
	switch {
	case p.JSON:
		return &jsonLogWriter{w: p.Out}
	case p.Color:
		return p.Out
	default:
		return NewStripWriter(p.Out)
	}
}

// FlushLog 刷新 LogWriter 可能缓存的尾部字节。
func (p *Printer) FlushLog(w io.Writer) {
	if s, ok := w.(*StripWriter); ok {
		_ = s.Flush()
	}
}

// jsonLogWriter 把日志块写成 JSONL。
type jsonLogWriter struct {
	w io.Writer
}

func (j *jsonLogWriter) Write(p []byte) (int, error) {
	line, err := json.Marshal(map[string]string{"type": "log", "data": string(p)})
	if err != nil {
		return 0, err
	}
	line = append(line, '\n')
	if _, err := j.w.Write(line); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Status 给运行状态上色。
func (p *Printer) Status(status string) string {
	switch status {
	case "success":
		return p.Green(status)
	case "failed", "error":
		return p.Red(status)
	case "timeout", "canceled", "cancelled":
		return p.Yellow(status)
	case "running":
		return p.Cyan(status)
	case "queued":
		return p.Blue(status)
	case "":
		return p.Dim("-")
	default:
		return status
	}
}

// OK 返回彩色对勾/叉号，用于 doctor 输出。
func (p *Printer) OK(found bool) string {
	if found {
		return p.Green("✔")
	}
	return p.Red("✘")
}
