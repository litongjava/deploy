package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"deploy-server/internal/ids"
	"deploy-server/internal/runner"
	"deploy-server/internal/store"
)

// strconvItoa 是个别名，避免各处重复引入 strconv。
func strconvItoa(n int) string { return strconv.Itoa(n) }

// timeoutCtx 是 context.WithTimeout 的短别名，让 handler 里少写一行。
func timeoutCtx(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// ---------- Hook 管理 ----------

func (s *Server) listHooks(c *gin.Context) {
	p, _, ok := s.loadProjectFor(c, c.Param("key"))
	if !ok {
		return
	}
	var list []store.HookToken
	s.st.DB().Where("project_id = ?", p.ID).Order("id desc").Find(&list)
	out := make([]hookDTO, 0, len(list))
	for _, h := range list {
		out = append(out, toHookDTO(h))
	}
	OK(c, out)
}

type createHookReq struct {
	Name          string `json:"name"`
	ExpiresInDays int    `json:"expiresInDays"`
}

func (s *Server) createHook(c *gin.Context) {
	p, _, ok := s.loadProjectForEdit(c, c.Param("key"))
	if !ok {
		return
	}
	var req createHookReq
	_ = c.ShouldBindJSON(&req)

	plain, prefix := ids.NewHookToken()
	h := store.HookToken{
		ProjectID: p.ID, Name: defaultStr(req.Name, "默认令牌"),
		Prefix: prefix, TokenHash: ids.HashToken(plain), Enabled: true,
	}
	if req.ExpiresInDays > 0 {
		exp := time.Now().AddDate(0, 0, req.ExpiresInDays)
		h.ExpiresAt = &exp
	}
	if err := s.st.DB().Create(&h).Error; err != nil {
		Internal(c, "创建 Hook 令牌失败")
		return
	}
	s.audit(c, "create-hook", p.Key, h.Name)

	dto := toHookDTO(h)
	OK(c, gin.H{
		"id": dto.ID, "projectId": dto.ProjectID, "name": dto.Name,
		"prefix": dto.Prefix, "enabled": dto.Enabled, "expiresAt": dto.ExpiresAt,
		"createdAt": dto.CreatedAt,
		// 明文只在创建/重置的这一次响应里出现。
		"token":    plain,
		"hookPath": "/api/v1/hooks/" + plain,
	})
}

func (s *Server) deleteHook(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var h store.HookToken
	if err := s.st.DB().First(&h, id).Error; err != nil {
		NotFound(c, "Hook 令牌不存在")
		return
	}
	if _, _, ok := s.loadProjectForEdit(c, keyOf(h.ProjectID, s)); !ok {
		return
	}
	s.st.DB().Delete(&store.HookToken{}, id)
	s.audit(c, "delete-hook", "", h.Name)
	OK(c, nil)
}

func (s *Server) resetHook(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var h store.HookToken
	if err := s.st.DB().First(&h, id).Error; err != nil {
		NotFound(c, "Hook 令牌不存在")
		return
	}
	p, _, ok := s.loadProjectForEdit(c, keyOf(h.ProjectID, s))
	if !ok {
		return
	}
	plain, prefix := ids.NewHookToken()
	s.st.DB().Model(&store.HookToken{}).Where("id = ?", id).
		Updates(map[string]any{"token_hash": ids.HashToken(plain), "prefix": prefix})
	s.audit(c, "reset-hook", p.Key, h.Name)
	OK(c, gin.H{"id": id, "token": plain, "hookPath": "/api/v1/hooks/" + plain})
}

// ---------- Hook 触发（免登录） ----------

type hookTriggerReq struct {
	Branch         string            `json:"branch"`
	Env            map[string]string `json:"env"`
	CommandIDs     []uint            `json:"commandIds"`
	Async          *bool             `json:"async"`
	IdempotencyKey string            `json:"idempotencyKey"`
}

type rateBucket struct {
	mu      sync.Mutex
	entries map[uint]*rateEntry
}

type rateEntry struct {
	count   int
	resetAt time.Time
}

var hookLimiter = &rateBucket{entries: map[uint]*rateEntry{}}

// allow 是每令牌每分钟的滑动窗口计数。默认 60 次/分钟，可在系统设置里改。
func (r *rateBucket) allow(tokenID uint, limit int) bool {
	if limit <= 0 {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	e, ok := r.entries[tokenID]
	if !ok || now.After(e.resetAt) {
		r.entries[tokenID] = &rateEntry{count: 1, resetAt: now.Add(time.Minute)}
		return true
	}
	if e.count >= limit {
		return false
	}
	e.count++
	return true
}

func (s *Server) hookTrigger(c *gin.Context) {
	raw := c.Param("token")
	if raw == "" {
		raw = c.GetHeader("X-Deploy-Token")
	}
	if raw == "" {
		Unauthorized(c, "缺少 Hook 令牌")
		return
	}

	var h store.HookToken
	if err := s.st.DB().Where("token_hash = ?", ids.HashToken(raw)).First(&h).Error; err != nil {
		Unauthorized(c, "Hook 令牌无效")
		return
	}
	if !h.Enabled {
		Forbidden(c, "Hook 令牌已被禁用")
		return
	}
	if h.ExpiresAt != nil && time.Now().After(*h.ExpiresAt) {
		Unauthorized(c, "Hook 令牌已过期")
		return
	}
	if !hookLimiter.allow(h.ID, s.effectiveHookLimit()) {
		RateLimited(c, "触发过于频繁，请稍后再试")
		return
	}

	var p store.Project
	if err := s.st.DB().First(&p, h.ProjectID).Error; err != nil {
		NotFound(c, "项目不存在")
		return
	}
	if !p.HookEnabled {
		Forbidden(c, "该项目的 Hook 触发已被关闭")
		return
	}
	if p.Archived {
		Forbidden(c, "项目已归档，不能触发部署")
		return
	}

	var req hookTriggerReq
	_ = c.ShouldBindJSON(&req) // body 可以完全省略

	async := true
	if req.Async != nil {
		async = *req.Async
	}
	if c.Query("wait") == "true" || c.Query("wait") == "1" {
		async = false
	}

	ip := ClientIP(c)
	now := time.Now()
	s.st.DB().Model(&store.HookToken{}).Where("id = ?", h.ID).Updates(map[string]any{
		"last_used_at": now, "last_used_ip": ip, "call_count": h.CallCount + 1,
	})

	run, err := s.exec.Submit(runner.SubmitRequest{
		ProjectID:   p.ID,
		Ref:         req.Branch,
		Params:      req.Env,
		CommandIDs:  req.CommandIDs,
		TriggerType: store.TriggerHook,
		TriggerUser: "hook:" + h.Name,
		TriggerIP:   ip,
		TriggerUA:   c.GetHeader("User-Agent"),
	})
	if err != nil {
		if errors.Is(err, runner.ErrProjectArchived) {
			Forbidden(c, err.Error())
			return
		}
		Internal(c, "触发失败: "+err.Error())
		return
	}

	payload := gin.H{
		"runId": run.ID, "projectKey": p.Key, "buildNumber": run.BuildNumber,
		"status":    run.Status,
		"async":     async,
		"statusUrl": "/api/v1/runs/" + strconvItoa(int(run.ID)),
		"logUrl":    "/api/v1/runs/" + strconvItoa(int(run.ID)) + "/log",
	}

	if async {
		// 202：已受理，未完成。这是给 curl / CI 看的语义。
		c.JSON(http.StatusAccepted, Envelope{Success: true, Data: payload})
		return
	}

	// 同步模式：等运行结束再返回，方便 CI 直接卡住等结果。
	final := s.waitRun(run.ID, 30*time.Minute)
	payload["status"] = final.Status
	payload["exitCode"] = final.ExitCode
	payload["durationMs"] = final.DurationMs
	payload["errorMsg"] = final.ErrorMsg
	OK(c, payload)
}

// waitRun 轮询等待 Run 结束。轮询间隔从 200ms 逐渐放宽到 2s，
// 既能让短任务快速返回，也不会长任务把数据库打爆。
func (s *Server) waitRun(runID uint, max time.Duration) store.Run {
	deadline := time.Now().Add(max)
	interval := 200 * time.Millisecond
	var run store.Run
	for {
		if err := s.st.DB().First(&run, runID).Error; err != nil {
			return run
		}
		switch run.Status {
		case store.RunSuccess, store.RunFailed, store.RunCanceled, store.RunTimeout, store.RunError:
			return run
		}
		if time.Now().After(deadline) {
			return run
		}
		time.Sleep(interval)
		if interval < 2*time.Second {
			interval *= 2
		}
	}
}

// effectiveHookLimit 取生效的 Hook 限流阈值。
func (s *Server) effectiveHookLimit() int {
	if v, ok := s.st.GetSetting("hookRateLimitPerMinute"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return s.cfg.HookRateLimit
}
