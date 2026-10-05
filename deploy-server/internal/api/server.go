package api

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"deploy-server/internal/config"
	"deploy-server/internal/runner"
	"deploy-server/internal/secret"
	"deploy-server/internal/store"
	"deploy-server/internal/web"
)

// Server 聚合 HTTP 层需要的全部依赖。
type Server struct {
	cfg       *config.Config
	st        *store.Store
	box       *secret.Box
	auth      *AuthManager
	exec      *runner.Executor
	version   string
	startedAt time.Time
}

// NewServer 构造 HTTP 服务。
func NewServer(cfg *config.Config, st *store.Store, box *secret.Box, exec *runner.Executor, version string) *Server {
	return &Server{
		cfg:       cfg,
		st:        st,
		box:       box,
		auth:      NewAuthManager(st, cfg.JWTSecret, time.Duration(cfg.TokenTTLHours)*time.Hour),
		exec:      exec,
		version:   version,
		startedAt: time.Now(),
	}
}

// Router 装配所有路由。
//
// 注意一处路径取舍：公开的 Hook 触发是 `POST /hooks/:token`，
// 而重置 Go 的 gin 路由器不允许同一层级出现两个不同名的通配符
// （`:token` 与 `:id` 会 panic），所以 Hook 的重置接口放在
// `POST /hook-tokens/:id/reset`。契约文档已同步。
func (s *Server) Router() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), requestLogger(), corsMiddleware())
	r.MaxMultipartMemory = 8 << 20

	v1 := r.Group("/api/v1")

	// ---------- 公开接口 ----------
	v1.GET("/healthz", s.healthz)
	v1.GET("/auth/bootstrap-status", s.bootstrapStatus)
	v1.POST("/auth/bootstrap", s.bootstrap)
	v1.POST("/auth/login", s.login)
	v1.POST("/hooks/:token", s.hookTrigger)

	// 只带请求头、路径里没有令牌的触发方式。
	//
	// 这条路由是必须的：handler 里有「路径里没有 token 就退回读 X-Deploy-Token 头」
	// 的逻辑，但如果只注册 /hooks/:token，那么 `/hooks/` 会 404、
	// 而 `/hooks/xxx` 的路径参数又永远非空 —— 于是那段退回逻辑成了**永远走不到的死代码**，
	// 契约里写的「token 也可放请求头」实际不可用（实测：仅请求头返回 404，路径错+头对返回 401）。
	//
	// 头部方式还有个实际好处：令牌不出现在 URL 里，也就不会落进 nginx 的 access_log。
	v1.POST("/hooks", s.hookTrigger)

	// WebSocket 的令牌只能走 query（浏览器 WS 不能自定义 Header），
	// 所以它自己鉴权，不挂中间件。
	v1.GET("/ws/runs/:id", s.runLogWS)

	// ---------- 需要登录 ----------
	a := v1.Group("", s.auth.Middleware())
	{
		a.GET("/auth/me", s.me)
		a.POST("/auth/logout", s.logout)
		a.POST("/auth/change-password", s.changePassword)

		a.GET("/tokens", s.listTokens)
		a.POST("/tokens", s.createToken)
		a.DELETE("/tokens/:id", s.deleteToken)

		a.GET("/credentials", s.listCredentials)
		a.POST("/credentials", s.createCredential)
		a.PUT("/credentials/:id", s.updateCredential)
		a.DELETE("/credentials/:id", s.deleteCredential)
		a.POST("/credentials/test-git", s.testGit)

		a.GET("/projects", s.listProjects)
		a.POST("/projects", s.createProject)
		a.GET("/projects/:key", s.getProject)
		a.PUT("/projects/:key", s.updateProject)
		a.DELETE("/projects/:key", s.deleteProject)
		a.POST("/projects/:key/archive", s.archiveProject)

		a.GET("/projects/:key/commands", s.listCommands)
		a.POST("/projects/:key/commands", s.createCommand)
		a.POST("/projects/:key/commands/reorder", s.reorderCommands)
		a.PUT("/commands/:id", s.updateCommand)
		a.DELETE("/commands/:id", s.deleteCommand)

		a.GET("/projects/:key/envs", s.listEnvs)
		a.PUT("/projects/:key/envs", s.replaceEnvs)
		a.POST("/projects/:key/preview", s.previewProject)

		a.GET("/projects/:key/hooks", s.listHooks)
		a.POST("/projects/:key/hooks", s.createHook)
		a.DELETE("/hooks/:id", s.deleteHook)
		a.POST("/hook-tokens/:id/reset", s.resetHook)

		a.POST("/projects/:key/run", s.triggerRun)
		a.GET("/runs", s.listRuns)
		a.GET("/runs/:id", s.getRun)
		a.GET("/runs/:id/log", s.getRunLog)
		a.GET("/runs/:id/log/download", s.downloadRunLog)
		a.POST("/runs/:id/cancel", s.cancelRun)
		a.POST("/runs/:id/rerun", s.rerun)

		a.GET("/system/doctor", s.doctor)
		// 服务器基本信息与资源占用（CPU / 内存 / 磁盘）。
		// 读的是后台采样好的快照，前端可以放心高频轮询。
		a.GET("/system/info", s.serverInfo)
		a.GET("/system/settings", s.getSettings)
		// 环境工具链一键安装：安装以普通 Run 的形式执行（见 handlers_toolchain.go）。
		// 只读的清单给所有登录用户看，真正动手安装仅限 admin。
		a.GET("/system/toolchains", s.listToolchains)

		admin := a.Group("", RequireAdmin())
		{
			admin.GET("/users", s.listUsers)
			admin.POST("/users", s.createUser)
			admin.PUT("/users/:id", s.updateUser)
			admin.DELETE("/users/:id", s.deleteUser)
			admin.POST("/users/:id/reset-password", s.resetUserPassword)
			admin.PUT("/system/settings", s.updateSettings)
			admin.GET("/audit-logs", s.listAuditLogs)
			admin.POST("/system/toolchains/install", s.installToolchains)
			admin.PUT("/system/toolchains/:key", s.updateToolchainFields)
			admin.GET("/system/toolchains/script", s.downloadToolchainScript)
		}
	}

	if err := web.Register(r); err != nil {
		slog.Error("failed to register static assets", "err", err)
	}
	return r
}

// requestLogger 记录慢请求与 5xx，正常请求不刷屏。
func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		cost := time.Since(start)
		status := c.Writer.Status()
		if status >= 500 || cost > 3*time.Second {
			slog.Warn("http",
				"method", c.Request.Method, "path", c.Request.URL.Path,
				"status", status, "cost", cost.String(), "ip", c.ClientIP())
		}
	}
}

// corsMiddleware 只对显式带 Origin 的请求放开，且不使用通配符，
// 避免把「带 Authorization 的接口」暴露给任意站点。
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && isAllowedOrigin(origin) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Authorization,Content-Type,X-Deploy-Token")
			c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
			c.Header("Access-Control-Max-Age", "600")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// isAllowedOrigin 允许本机开发端口与同源访问。
// 生产部署时前端由本服务托管，属于同源，根本不会触发 CORS。
func isAllowedOrigin(origin string) bool {
	if strings.HasPrefix(origin, "http://localhost:") ||
		strings.HasPrefix(origin, "http://127.0.0.1:") ||
		strings.HasPrefix(origin, "http://192.168.") ||
		strings.HasPrefix(origin, "http://10.") {
		return true
	}
	return false
}
