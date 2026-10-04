// Package api 是 HTTP 层：路由、鉴权中间件、请求/响应编解码。
//
// 响应封装刻意贴合前端沿用的 Ant Design Pro 模板
// （src/requestErrorConfig.ts 里已经写死 success/errorCode/errorMessage/showType），
// 这样前端不用为了适配后端去改请求层。
package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// 错误码，与 docs/API设计.md §0.2 一致。
const (
	CodeOK           = 0
	CodeBadRequest   = 40000
	CodeUnauthorized = 40100
	CodeForbidden    = 40300
	CodeNotFound     = 40400
	CodeConflict     = 40900
	CodeRateLimited  = 42900
	CodeInternal     = 50000
)

// Envelope 是所有接口的统一响应体。
type Envelope struct {
	Success      bool   `json:"success"`
	Data         any    `json:"data"`
	ErrorCode    int    `json:"errorCode,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
	ShowType     int    `json:"showType,omitempty"`
}

// OK 返回成功响应。
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Envelope{Success: true, Data: data})
}

// bizFail 是「业务失败」：HTTP 200 + success=false。
//
// 为什么不用 HTTP 4xx：前端模板的 errorThrower 只在 HTTP 200 的响应体上
// 判断 success 字段并抛出 BizError，用 4xx 会走到另一条分支。
// 唯一的例外是 401 —— 前端需要靠它统一跳登录页。
//
// **必须 Abort**：gin 的 handler 链是由引擎最外层的 Next() 循环驱动的，
// 中间件里单纯 `return` 并不会阻止后面的 handler 执行 ——
// 实测漏掉 Abort 时，非管理员请求 /users 会先写一个 403 信封，
// 紧接着又把完整用户列表追加到同一个响应体里（越权泄漏）。
func bizFail(c *gin.Context, code int, msg string, showType int) {
	c.Abort()
	c.JSON(http.StatusOK, Envelope{
		Success: false, ErrorCode: code, ErrorMessage: msg, ShowType: showType,
	})
}

// BadRequest 参数错误。
func BadRequest(c *gin.Context, msg string) { bizFail(c, CodeBadRequest, msg, 2) }

// Unauthorized 未登录/令牌失效，用 HTTP 401 让前端跳登录。
func Unauthorized(c *gin.Context, msg string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, Envelope{
		Success: false, ErrorCode: CodeUnauthorized, ErrorMessage: msg, ShowType: 0,
	})
}

// Forbidden 无权限。
func Forbidden(c *gin.Context, msg string) { bizFail(c, CodeForbidden, msg, 2) }

// NotFound 资源不存在。
func NotFound(c *gin.Context, msg string) { bizFail(c, CodeNotFound, msg, 2) }

// Conflict 冲突（key 重复、被引用等）。
func Conflict(c *gin.Context, msg string) { bizFail(c, CodeConflict, msg, 2) }

// RateLimited 触发过频。
func RateLimited(c *gin.Context, msg string) { bizFail(c, CodeRateLimited, msg, 1) }

// Internal 服务端错误。真实原因写日志，返回给前端的是脱敏后的说明。
func Internal(c *gin.Context, msg string) { bizFail(c, CodeInternal, msg, 2) }

// PageResult 是分页响应的统一形状。
type PageResult struct {
	List     any   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}

// NewPage 构造分页响应。list 为 nil 时替换成空数组，
// 免得前端拿到 null 还要判空。
func NewPage(list any, total int64, page, size int) PageResult {
	if list == nil {
		list = []any{}
	}
	return PageResult{List: list, Total: total, Page: page, PageSize: size}
}

// pageParams 解析 ?page & ?pageSize，并夹到合理范围。
func pageParams(c *gin.Context) (page, size, offset int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ = strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	return page, size, (page - 1) * size
}

// boolQuery 解析 ?x=true/false，缺省时返回 def。
func boolQuery(c *gin.Context, key string, def bool) bool {
	v := c.Query(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

// idParam 解析路径上的 :id。
func idParam(c *gin.Context, name string) (uint, bool) {
	v, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || v == 0 {
		BadRequest(c, "path parameter "+name+" is not a valid ID")
		return 0, false
	}
	return uint(v), true
}
