// Package exitcode 定义 CLI 的退出码语义与错误分类。
//
// 退出码约定（脚本会依赖，勿随意更改）：
//
//	0  成功
//	1  部署失败（run 状态为 failed / timeout / error / canceled）
//	2  用法错误（参数不合法、命令不存在、本地配置损坏等）
//	3  鉴权失败（HTTP 401 / errorCode 40100、40300 无权限、未登录）
//	4  网络或服务不可用（连接被拒、超时、5xx / errorCode 50000、42900 限流）
package exitcode

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// 退出码常量。
const (
	OK           = 0
	DeployFailed = 1
	Usage        = 2
	Auth         = 3
	Network      = 4
)

// Coder 由任何能自行声明退出码的错误实现（api.Error、Error）。
type Coder interface {
	ExitCode() int
}

// Error 是一个带退出码的错误包装。
type Error struct {
	Code int
	Msg  string
	Err  error
	// silent 为 true 时 main 不再重复打印错误信息（用于已经把
	// 细节打印到 stdout/stderr 的运行结果类失败）。
	silent bool
}

func (e *Error) Error() string {
	switch {
	case e.Msg != "" && e.Err != nil:
		return e.Msg + ": " + e.Err.Error()
	case e.Msg != "":
		return e.Msg
	case e.Err != nil:
		return e.Err.Error()
	}
	return fmt.Sprintf("command exited with code %d", e.Code)
}

// Unwrap 支持 errors.Is / errors.As 穿透。
func (e *Error) Unwrap() error { return e.Err }

// ExitCode 实现 Coder。
func (e *Error) ExitCode() int { return e.Code }

// Silent 报告 main 是否应跳过错误打印。
func (e *Error) Silent() bool { return e.silent }

// New 构造一个带退出码的错误。
func New(code int, format string, args ...any) *Error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// Fail 构造一个静默的退出码错误（信息已由调用方输出）。
func Fail(code int) *Error {
	return &Error{Code: code, silent: true}
}

// Wrap 在 err 外层附上退出码与上下文信息；err 为 nil 时返回 nil。
func Wrap(code int, err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	return &Error{Code: code, Msg: fmt.Sprintf(format, args...), Err: err}
}

// Usagef 用法错误（退出码 2）。
func Usagef(format string, args ...any) error { return New(Usage, format, args...) }

// Authf 鉴权错误（退出码 3）。
func Authf(format string, args ...any) error { return New(Auth, format, args...) }

// Netf 网络/服务错误（退出码 4）。
func Netf(format string, args ...any) error { return New(Network, format, args...) }

// Of 把任意错误映射成进程退出码。
//
// 判定顺序：显式 Coder → context → net.Error / url.Error → 兜底 Usage(2)。
// 兜底为 2 的原因：所有可预期的运行期失败（HTTP 错误、传输错误、运行失败）
// 都在各自的类型里显式声明了退出码，剩下无法归类的几乎只有用法/参数问题。
func Of(err error) int {
	if err == nil {
		return OK
	}
	var c Coder
	if errors.As(err, &c) {
		return c.ExitCode()
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return Network
	}
	var nerr net.Error
	if errors.As(err, &nerr) {
		return Network
	}
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return Network
	}
	if isNetworkMessage(err.Error()) {
		return Network
	}
	return Usage
}

var networkHints = []string{
	"connection refused",
	"connection reset",
	"connection timed out",
	"i/o timeout",
	"no such host",
	"network is unreachable",
	"tls handshake timeout",
	"server misbehaving",
	"unexpected eof",
	"wsarecv",
	"wsasend",
	"forcibly closed",
}

func isNetworkMessage(msg string) bool {
	low := strings.ToLower(msg)
	for _, h := range networkHints {
		if strings.Contains(low, h) {
			return true
		}
	}
	return false
}
