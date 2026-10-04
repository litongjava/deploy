package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"deploy-cli/internal/api"
	"deploy-cli/internal/exitcode"
	"deploy-cli/internal/output"
)

// exactArgs 校验参数个数，错误归为用法错误（退出码 2）。
func exactArgs(n int, usage string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != n {
			return exitcode.Usagef("%s", usage)
		}
		return nil
	}
}

// parseRunID 解析运行 ID。
func parseRunID(s string) (int64, error) {
	s = strings.TrimSpace(s)
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, exitcode.Usagef("运行 ID 必须是正整数，收到 %q", s)
	}
	return id, nil
}

// parseEnvPairs 解析 -e K=V（可重复）。
func parseEnvPairs(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		i := strings.Index(p, "=")
		if i <= 0 {
			return nil, exitcode.Usagef("环境变量格式应为 K=V，收到 %q", p)
		}
		k := strings.TrimSpace(p[:i])
		if k == "" {
			return nil, exitcode.Usagef("环境变量名不能为空：%q", p)
		}
		out[k] = p[i+1:]
	}
	return out, nil
}

// formatTime 把 RFC3339 转成本地时间显示。
func formatTime(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "-"
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Local().Format("2006-01-02 15:04:05")
		}
	}
	return s
}

// maskToken 隐藏 token 中间部分。
func maskToken(s string) string {
	if s == "" {
		return "(未设置)"
	}
	if len(s) <= 10 {
		return strings.Repeat("*", len(s))
	}
	return s[:4] + strings.Repeat("*", 6) + s[len(s)-4:]
}

// interactiveFn 判断 stdin 是否为终端；测试可覆盖，避免阻塞在交互输入上。
var interactiveFn = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// isInteractive 报告 stdin 是否为终端。
func isInteractive() bool { return interactiveFn() }

// promptLine 逐字节读取一行输入（不做预读缓冲，避免把后续的密码行提前吃掉），
// 回车时返回 def。
func promptLine(label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(os.Stderr, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(os.Stderr, "%s: ", label)
	}
	var sb strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			switch buf[0] {
			case '\n':
				line := strings.TrimSpace(sb.String())
				if line == "" {
					return def, nil
				}
				return line, nil
			case '\r':
				// 忽略 CR，等 LF
			default:
				sb.WriteByte(buf[0])
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) && sb.Len() > 0 {
				return strings.TrimSpace(sb.String()), nil
			}
			return "", exitcode.Wrap(exitcode.Usage, err, "读取输入失败")
		}
	}
}

// readPassword 交互式读取密码，输入不回显（golang.org/x/term）。
func readPassword(label string) (string, error) {
	fmt.Fprintf(os.Stderr, "%s", label)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", exitcode.Wrap(exitcode.Usage, err, "读取密码失败")
	}
	pw := strings.TrimSpace(string(b))
	if pw == "" {
		return "", exitcode.Usagef("密码不能为空")
	}
	return pw, nil
}

// isTransient 判断错误是否值得继续等待（网络/服务端临时故障）。
func isTransient(err error) bool {
	if err == nil {
		return false
	}
	if e, ok := api.AsError(err); ok {
		return e.IsRetriable()
	}
	return exitcode.Of(err) == exitcode.Network
}

// runHead 组合运行的标识：全局 run ID + 项目内构建号（第几次部署）。
//
// 服务端日志头部打印的是 buildNumber（如 "=== Deploy Run #2 ==="），
// 而 API 里处处用的是全局 runId，两个数字含义不同，必须同时标注清楚。
func runHead(runID int64, buildNumber int) string {
	if buildNumber > 0 {
		return fmt.Sprintf("运行 ID #%d（构建号 #%d）", runID, buildNumber)
	}
	return fmt.Sprintf("运行 ID #%d", runID)
}

// statusLine 组合「运行标识 + 状态 + 退出码 + 耗时」的摘要文本。
func (a *App) statusLine(runID int64, buildNumber int, status string, exitCode *int, durationMs int64) string {
	line := runHead(runID, buildNumber) + "  " + a.Printer.Status(status)
	if exitCode != nil {
		line += fmt.Sprintf("  退出码 %d", *exitCode)
	}
	if durationMs > 0 {
		line += "  耗时 " + api.FormatDuration(durationMs)
	}
	return line
}

// verboseFn 把 Printer 的调试输出适配成 ws.Options.Verbose。
func verboseFn(p *output.Printer) func(string, ...any) {
	return func(format string, args ...any) { p.Verbosef(format, args...) }
}
