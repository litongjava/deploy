package api

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"deploy-server/internal/store"
)

// 权限模型（对应决策 D3：多用户 + admin/user 两种角色）：
//   - admin：全部项目的读写与执行
//   - owner：自己项目的读写与执行
//   - 其他登录用户：可见性由系统设置 visibility 决定
//     （allReadable = 只读可见；ownerOnly = 完全不可见）
//
// 所有以 :key 进入的接口都必须先过 canView/canEdit，
// 不允许直接按 key 查库后就用。

// canEdit 判断能否修改/执行该项目。
func (s *Server) canEdit(u *store.User, p *store.Project) bool {
	if u == nil || p == nil {
		return false
	}
	if u.Role == "admin" {
		return true
	}
	return p.OwnerID == u.ID
}

// canView 判断能否看到该项目。
func (s *Server) canView(u *store.User, p *store.Project) bool {
	if s.canEdit(u, p) {
		return true
	}
	return s.visibilityMode() != "ownerOnly"
}

// visibilityMode 取生效的可见性设置（系统设置优先于启动配置）。
func (s *Server) visibilityMode() string {
	if v, ok := s.st.GetSetting("visibility"); ok && v != "" {
		return v
	}
	if s.cfg.Visibility == "" {
		return "allReadable"
	}
	return s.cfg.Visibility
}

// loadProjectFor 按 key 取项目并做可见性校验，失败时已经写好响应。
func (s *Server) loadProjectFor(c *gin.Context, key string) (*store.Project, *store.User, bool) {
	u := CurrentUser(c)
	var p store.Project
	if err := s.st.DB().Where("key = ?", key).First(&p).Error; err != nil {
		NotFound(c, "项目不存在: "+key)
		return nil, nil, false
	}
	if !s.canView(u, &p) {
		NotFound(c, "项目不存在: "+key) // 不可见时也返回 404，不泄露项目是否存在
		return nil, nil, false
	}
	return &p, u, true
}

// loadProjectForEdit 同上，但要求可写权限。
func (s *Server) loadProjectForEdit(c *gin.Context, key string) (*store.Project, *store.User, bool) {
	p, u, ok := s.loadProjectFor(c, key)
	if !ok {
		return nil, nil, false
	}
	if !s.canEdit(u, p) {
		Forbidden(c, "只有项目负责人或管理员可以修改/执行该项目")
		return nil, nil, false
	}
	return p, u, true
}

// audit 记一条审计日志。
func (s *Server) audit(c *gin.Context, action, target, detail string) {
	u := CurrentUser(c)
	uid, name := uint(0), "anonymous"
	if u != nil {
		uid, name = u.ID, u.Username
	}
	s.st.Audit(uid, name, action, target, detail, ClientIP(c))
}

// ---------- 登录失败限流 ----------

// loginGuard 是极简的登录失败计数：同一用户名连续失败 5 次，锁 10 分钟。
// 放内存里足够（重启即清零），目的是挡住暴力尝试，不是做审计系统。
type loginGuard struct {
	entries map[string]*loginEntry
}

type loginEntry struct {
	fails    int
	lockedTo time.Time
}

var guard = &loginGuard{entries: map[string]*loginEntry{}}

func (g *loginGuard) locked(key string) (bool, time.Duration) {
	e, ok := g.entries[strings.ToLower(key)]
	if !ok {
		return false, 0
	}
	if time.Now().Before(e.lockedTo) {
		return true, time.Until(e.lockedTo)
	}
	return false, 0
}

func (g *loginGuard) fail(key string) {
	key = strings.ToLower(key)
	e, ok := g.entries[key]
	if !ok {
		e = &loginEntry{}
		g.entries[key] = e
	}
	e.fails++
	if e.fails >= 5 {
		e.lockedTo = time.Now().Add(10 * time.Minute)
		e.fails = 0
	}
}

func (g *loginGuard) reset(key string) {
	delete(g.entries, strings.ToLower(key))
}
