//go:build windows

package runner

import (
	"os/exec"
	"strconv"
	"syscall"
)

// CREATE_NEW_PROCESS_GROUP：让子进程独立成组，便于整组发送信号。
const createNewProcessGroup = 0x00000200

func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

// killTree 用 taskkill /T /F 杀掉整棵进程树。
//
// Windows 上没有等价于 Unix `kill(-pgid)` 的简单做法：Job Object 需要在
// 创建进程时就绑定，且 Go 标准库不直接暴露。taskkill /T 是系统自带、
// 覆盖绝大多数场景的方案，这里再补一次 Process.Kill 作为兜底。
func killTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	_ = exec.Command("taskkill", "/T", "/F", "/PID", pid).Run()
	_ = cmd.Process.Kill()
}
