// Package api 实现 API 契约（v1，冻结）的 HTTP 客户端与类型定义。
//
// 契约要点：
//   - Base path: /api/v1，字段 camelCase；
//   - 成功: HTTP 200 {"success":true,"data":{...}}
//   - 业务失败: HTTP 200 {"success":false,"errorCode":40001,"errorMessage":"..."}
//   - 认证失败: HTTP 401 {"success":false,"errorCode":40100,...}
//   - 请求头 Authorization 直接放裸 token（服务端同时接受 Bearer 前缀）。
package api

import (
	"errors"
	"fmt"
	"net/http"

	"deploy-cli/internal/exitcode"
)

// 契约里的业务错误码。
const (
	CodeBadRequest   = 40000
	CodeUnauthorized = 40100
	CodeForbidden    = 40300
	CodeNotFound     = 40400
	CodeConflict     = 40900
	CodeRateLimited  = 42900
	CodeInternal     = 50000
)

// Error 同时表示传输层错误（HTTPStatus == 0）与业务错误。
type Error struct {
	// HTTPStatus 为 0 表示压根没拿到 HTTP 响应（连接失败、超时、解析失败）。
	HTTPStatus int
	Code       int
	Message    string
	ShowType   int
	cause      error
}

// NewError 构造业务错误。
func NewError(status, code int, message string) *Error {
	return &Error{HTTPStatus: status, Code: code, Message: message}
}

// TransportError 把底层网络错误包成 *Error（HTTPStatus=0 → 退出码 4）。
func TransportError(op string, err error) *Error {
	return &Error{
		Message: fmt.Sprintf("%s failed: %v", op, err),
		cause:   err,
	}
}

// ProtocolError 表示响应无法按契约解析。
//
// HTTPStatus 故意留 0：拿不到契约约定的响应体属于「服务不可用」而非业务错误，
// 退出码因此是 4（网络或服务不可用）。原始 HTTP 状态码写进 message 保留信息。
func ProtocolError(op string, status int, err error) *Error {
	return &Error{
		Message: fmt.Sprintf("%s returned a response that violates the API contract (HTTP %d): %v", op, status, err),
		cause:   err,
	}
}

func (e *Error) Error() string {
	switch {
	case e.Message != "":
		return e.Message
	case e.Code != 0:
		return fmt.Sprintf("server returned error code %d", e.Code)
	case e.HTTPStatus != 0:
		return fmt.Sprintf("HTTP %d", e.HTTPStatus)
	default:
		return "unknown API error"
	}
}

// Unwrap 让 errors.Is/As 能穿透到底层错误。
func (e *Error) Unwrap() error { return e.cause }

// ExitCode 实现 exitcode.Coder，把契约错误映射到 CLI 退出码。
//
//	401 / 40100 / 403 / 40300            → 3 鉴权失败
//	5xx / 50000 / 42900 / 无响应（传输） → 4 网络或服务不可用
//	40000 / 40400 / 40900 / 其它 4xx     → 2 用法（请求参数）错误
func (e *Error) ExitCode() int {
	if e.HTTPStatus == 0 {
		return exitcode.Network
	}
	switch e.Code {
	case CodeUnauthorized, CodeForbidden:
		return exitcode.Auth
	case CodeInternal, CodeRateLimited:
		return exitcode.Network
	case CodeBadRequest, CodeNotFound, CodeConflict:
		return exitcode.Usage
	}
	switch {
	case e.HTTPStatus == http.StatusUnauthorized || e.HTTPStatus == http.StatusForbidden:
		return exitcode.Auth
	case e.HTTPStatus >= 500 || e.HTTPStatus == http.StatusTooManyRequests:
		return exitcode.Network
	default:
		return exitcode.Usage
	}
}

// IsAuth 报告错误是否为鉴权类（401/403）。
func (e *Error) IsAuth() bool { return e.ExitCode() == exitcode.Auth }

// IsNotFound 报告是否资源不存在。
func (e *Error) IsNotFound() bool {
	return e.Code == CodeNotFound || e.HTTPStatus == http.StatusNotFound
}

// IsTransport 报告是否属于传输层失败（可重试）。
func (e *Error) IsTransport() bool { return e.HTTPStatus == 0 }

// IsRetriable 报告该错误是否值得重试（传输失败、5xx、限流）。
func (e *Error) IsRetriable() bool {
	if e.HTTPStatus == 0 {
		return true
	}
	if e.HTTPStatus >= 500 || e.Code == CodeInternal || e.Code == CodeRateLimited {
		return true
	}
	return false
}

// AsError 提取 *Error。
func AsError(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
