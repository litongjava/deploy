package runner

import (
	"os/exec"
	"runtime"
	"strings"

	"deploy-server/internal/store"
)

// IsWindows 便于其他地方判断平台差异。
func IsWindows() bool { return runtime.GOOS == "windows" }

// resolveShell 决定用哪个解释器执行用户脚本。
//
// 优先级：命令级覆盖 > 项目级设置 > 服务端全局设置 > auto。
// auto 的行为：Unix 优先 bash（用 -l 让 /etc/profile 生效，这样 systemd 里
// 最小 PATH 也能找到 node/go），退回 sh；Windows 优先 pwsh，退回 powershell、cmd。
func resolveShell(cmdOverride, projectShell, globalShell string) (string, []string) {
	choice := firstNonEmpty(cmdOverride, projectShell, globalShell, "auto")

	if choice != "" && choice != "auto" {
		switch choice {
		case "bash":
			if p, err := exec.LookPath("bash"); err == nil {
				return p, []string{"-lc"}
			}
		case "sh":
			if p, err := exec.LookPath("sh"); err == nil {
				return p, []string{"-c"}
			}
		case "pwsh":
			if p, err := exec.LookPath("pwsh"); err == nil {
				return p, []string{"-NoProfile", "-NonInteractive", "-Command"}
			}
		case "cmd":
			if p, err := exec.LookPath("cmd"); err == nil {
				return p, []string{"/c"}
			}
		}
		// 指定了但找不到，落到 auto 逻辑，避免直接失败。
	}

	if IsWindows() {
		if p, err := exec.LookPath("pwsh"); err == nil {
			return p, []string{"-NoProfile", "-NonInteractive", "-Command"}
		}
		if p, err := exec.LookPath("powershell"); err == nil {
			return p, []string{"-NoProfile", "-NonInteractive", "-Command"}
		}
		if p, err := exec.LookPath("cmd"); err == nil {
			return p, []string{"/c"}
		}
		return "cmd", []string{"/c"}
	}

	if p, err := exec.LookPath("bash"); err == nil {
		return p, []string{"-lc"}
	}
	if p, err := exec.LookPath("sh"); err == nil {
		return p, []string{"-c"}
	}
	return "sh", []string{"-c"}
}

// ShellDescription 给自检页展示：当前会选中哪个 shell。
func ShellDescription(globalShell string) (selected, path string, available []string) {
	candidates := []string{"bash", "sh"}
	if IsWindows() {
		candidates = []string{"pwsh", "powershell", "cmd", "bash", "sh"}
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			available = append(available, c)
			if path == "" {
				path = p
			}
		}
	}
	p, _ := resolveShell("", "", globalShell)
	selected = strings.TrimSuffix(strings.ToLower(baseName(p)), ".exe")
	if selected == "" {
		selected = "unknown"
	}
	return selected, path, available
}

func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// 确保 store 包被引用（ShellDescription 与项目设置同属执行链）。
var _ = store.ConcurrencyQueue
