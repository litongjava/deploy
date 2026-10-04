//go:build !windows

package runner

import (
	"os/exec"
	"syscall"
	"time"
)

// setProcAttr 让子进程成为新进程组的组长（pgid == pid）。
// 这样 kill(-pgid) 能一次性带走整棵进程树，
// 而不是只杀 shell、把子进程留成孤儿。
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killTree 先 SIGTERM 整组（给进程机会自己清理），2 秒后 SIGKILL 兜底。
// 两个信号都对整个进程组发，所以不会漏掉后台起的子进程。
func killTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	_ = syscall.Kill(pid, syscall.SIGTERM)

	go func() {
		time.Sleep(2 * time.Second)
		// 进程已退出时这两个调用是无害的（ESRCH），不需要判断。
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}()
}
