package api

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"deploy-server/internal/ids"
	"deploy-server/internal/store"
)

// healthz 是公开的健康检查。
func (s *Server) healthz(c *gin.Context) {
	OK(c, gin.H{
		"status": "ok", "version": s.version,
		"time":      time.Now().Format(time.RFC3339),
		"uptimeSec": int(time.Since(s.startedAt).Seconds()),
	})
}

// bootstrapStatus 告诉前端要不要走初始化向导。
func (s *Server) bootstrapStatus(c *gin.Context) {
	var n int64
	s.st.DB().Model(&store.User{}).Count(&n)
	OK(c, gin.H{"initialized": n > 0})
}

type bootstrapReq struct {
	Username string `json:"username" binding:"required,min=3,max=32"`
	Password string `json:"password" binding:"required,min=8,max=128"`
}

// bootstrap 只在系统一个用户都没有时可用，用来创建第一个管理员。
func (s *Server) bootstrap(c *gin.Context) {
	var n int64
	s.st.DB().Model(&store.User{}).Count(&n)
	if n > 0 {
		Conflict(c, "系统已初始化，无法重复创建管理员")
		return
	}
	var req bootstrapReq
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "用户名至少 3 位、密码至少 8 位")
		return
	}
	hash, err := HashPassword(req.Password)
	if err != nil {
		Internal(c, "生成密码哈希失败")
		return
	}
	u := store.User{
		Username: strings.TrimSpace(req.Username), PasswordHash: hash,
		Nickname: "管理员", Role: "admin", Status: "active",
	}
	if err := s.st.DB().Create(&u).Error; err != nil {
		Conflict(c, "创建管理员失败："+err.Error())
		return
	}
	token, exp, err := s.auth.Issue(&u)
	if err != nil {
		Internal(c, "签发令牌失败")
		return
	}
	s.st.Audit(u.ID, u.Username, "bootstrap", "system", "创建初始管理员", ClientIP(c))
	OK(c, gin.H{"token": token, "expiresAt": exp.Unix(), "user": toUserDTO(&u)})
}

type loginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (s *Server) login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "请填写用户名与密码")
		return
	}
	if locked, wait := guard.locked(req.Username); locked {
		RateLimited(c, "登录失败次数过多，请 "+wait.Round(time.Second).String()+" 后再试")
		return
	}

	var u store.User
	if err := s.st.DB().Where("username = ?", strings.TrimSpace(req.Username)).First(&u).Error; err != nil {
		guard.fail(req.Username)
		Unauthorized(c, "用户名或密码错误")
		return
	}
	if !CheckPassword(u.PasswordHash, req.Password) {
		guard.fail(req.Username)
		Unauthorized(c, "用户名或密码错误")
		return
	}
	if u.Status != "active" {
		Forbidden(c, "账号已被禁用，请联系管理员")
		return
	}
	guard.reset(req.Username)

	now := time.Now()
	s.st.DB().Model(&store.User{}).Where("id = ?", u.ID).Update("last_login_at", now)
	u.LastLoginAt = &now

	token, exp, err := s.auth.Issue(&u)
	if err != nil {
		Internal(c, "签发令牌失败")
		return
	}
	s.st.Audit(u.ID, u.Username, "login", "user", "", ClientIP(c))
	OK(c, gin.H{
		"token": token, "expiresAt": exp.Unix(),
		"mustChangePassword": u.MustChangePassword, "user": toUserDTO(&u),
	})
}

func (s *Server) me(c *gin.Context) {
	OK(c, toUserDTO(CurrentUser(c)))
}

func (s *Server) logout(c *gin.Context) {
	s.audit(c, "logout", "user", "")
	OK(c, nil)
}

type changePasswordReq struct {
	OldPassword string `json:"oldPassword" binding:"required"`
	NewPassword string `json:"newPassword" binding:"required,min=8,max=128"`
}

func (s *Server) changePassword(c *gin.Context) {
	var req changePasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "新密码长度至少 8 位")
		return
	}
	u := CurrentUser(c)
	if !CheckPassword(u.PasswordHash, req.OldPassword) {
		BadRequest(c, "原密码不正确")
		return
	}
	if req.OldPassword == req.NewPassword {
		BadRequest(c, "新密码不能与原密码相同")
		return
	}
	hash, err := HashPassword(req.NewPassword)
	if err != nil {
		Internal(c, "生成密码哈希失败")
		return
	}
	s.st.DB().Model(&store.User{}).Where("id = ?", u.ID).Updates(map[string]any{
		"password_hash": hash, "must_change_password": false,
	})
	s.audit(c, "change-password", "user", "")
	OK(c, nil)
}

// ---------- 用户管理（admin） ----------

func (s *Server) listUsers(c *gin.Context) {
	page, size, offset := pageParams(c)
	q := s.st.DB().Model(&store.User{})
	if kw := strings.TrimSpace(c.Query("keyword")); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("username LIKE ? OR nickname LIKE ? OR email LIKE ?", like, like, like)
	}
	var total int64
	q.Count(&total)

	var users []store.User
	q.Order("id asc").Limit(size).Offset(offset).Find(&users)
	list := make([]userDTO, 0, len(users))
	for i := range users {
		list = append(list, toUserDTO(&users[i]))
	}
	OK(c, NewPage(list, total, page, size))
}

type createUserReq struct {
	Username string `json:"username" binding:"required,min=3,max=32"`
	Password string `json:"password" binding:"required,min=8,max=128"`
	Nickname string `json:"nickname"`
	Email    string `json:"email"`
	Role     string `json:"role"`
}

func (s *Server) createUser(c *gin.Context) {
	var req createUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "用户名至少 3 位、密码至少 8 位")
		return
	}
	role := req.Role
	if role != "admin" {
		role = "user"
	}
	hash, err := HashPassword(req.Password)
	if err != nil {
		Internal(c, "生成密码哈希失败")
		return
	}
	u := store.User{
		Username: strings.TrimSpace(req.Username), PasswordHash: hash,
		Nickname: req.Nickname, Email: req.Email, Role: role, Status: "active",
		MustChangePassword: true,
	}
	if err := s.st.DB().Create(&u).Error; err != nil {
		Conflict(c, "创建失败，用户名可能已存在")
		return
	}
	s.audit(c, "create-user", u.Username, "role="+role)
	OK(c, toUserDTO(&u))
}

type updateUserReq struct {
	Nickname string `json:"nickname"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	Status   string `json:"status"`
}

func (s *Server) updateUser(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var req updateUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "参数不合法")
		return
	}
	var u store.User
	if err := s.st.DB().First(&u, id).Error; err != nil {
		NotFound(c, "用户不存在")
		return
	}
	cur := CurrentUser(c)

	updates := map[string]any{"nickname": req.Nickname, "email": req.Email}
	if req.Role == "admin" || req.Role == "user" {
		// 不允许把自己降级，避免把自己锁在外面。
		if u.ID == cur.ID && req.Role != "admin" {
			BadRequest(c, "不能取消自己的管理员权限")
			return
		}
		if u.Role == "admin" && req.Role != "admin" {
			if n := s.countActiveAdmins(); n <= 1 {
				BadRequest(c, "系统必须保留至少一个管理员")
				return
			}
		}
		updates["role"] = req.Role
	}
	if req.Status == "active" || req.Status == "disabled" {
		if u.ID == cur.ID && req.Status != "active" {
			BadRequest(c, "不能禁用自己")
			return
		}
		updates["status"] = req.Status
	}
	s.st.DB().Model(&store.User{}).Where("id = ?", id).Updates(updates)
	s.st.DB().First(&u, id)
	s.audit(c, "update-user", u.Username, "")
	OK(c, toUserDTO(&u))
}

func (s *Server) deleteUser(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	cur := CurrentUser(c)
	if id == cur.ID {
		BadRequest(c, "不能删除自己")
		return
	}
	var u store.User
	if err := s.st.DB().First(&u, id).Error; err != nil {
		NotFound(c, "用户不存在")
		return
	}
	if u.Role == "admin" && s.countActiveAdmins() <= 1 {
		BadRequest(c, "系统必须保留至少一个管理员")
		return
	}
	// 用户拥有的项目不删，只把归属转到当前管理员，避免项目变成孤儿。
	s.st.DB().Model(&store.Project{}).Where("owner_id = ?", id).
		Updates(map[string]any{"owner_id": cur.ID, "owner_name": cur.Username})
	s.st.DB().Where("user_id = ?", id).Delete(&store.APIToken{})
	s.st.DB().Delete(&store.User{}, id)
	s.audit(c, "delete-user", u.Username, "")
	OK(c, nil)
}

func (s *Server) countActiveAdmins() int64 {
	var n int64
	s.st.DB().Model(&store.User{}).Where("role = ? AND status = ?", "admin", "active").Count(&n)
	return n
}

type resetPasswordReq struct {
	NewPassword string `json:"newPassword" binding:"required,min=8,max=128"`
}

func (s *Server) resetUserPassword(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var req resetPasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "新密码长度至少 8 位")
		return
	}
	var u store.User
	if err := s.st.DB().First(&u, id).Error; err != nil {
		NotFound(c, "用户不存在")
		return
	}
	hash, err := HashPassword(req.NewPassword)
	if err != nil {
		Internal(c, "生成密码哈希失败")
		return
	}
	s.st.DB().Model(&store.User{}).Where("id = ?", id).Updates(map[string]any{
		"password_hash": hash, "must_change_password": true,
	})
	s.audit(c, "reset-password", u.Username, "")
	OK(c, nil)
}

// ---------- API Token ----------

func (s *Server) listTokens(c *gin.Context) {
	u := CurrentUser(c)
	var list []store.APIToken
	s.st.DB().Where("user_id = ?", u.ID).Order("id desc").Find(&list)
	out := make([]apiTokenDTO, 0, len(list))
	for _, t := range list {
		out = append(out, toAPITokenDTO(t))
	}
	OK(c, out)
}

type createTokenReq struct {
	Name          string `json:"name" binding:"required,max=64"`
	ExpiresInDays int    `json:"expiresInDays"`
}

func (s *Server) createToken(c *gin.Context) {
	var req createTokenReq
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "请填写令牌名称")
		return
	}
	u := CurrentUser(c)
	plain, prefix := ids.NewAPIToken()
	t := store.APIToken{
		UserID: u.ID, Name: req.Name, Prefix: prefix,
		TokenHash: ids.HashToken(plain),
	}
	if req.ExpiresInDays > 0 {
		exp := time.Now().AddDate(0, 0, req.ExpiresInDays)
		t.ExpiresAt = &exp
	}
	if err := s.st.DB().Create(&t).Error; err != nil {
		Internal(c, "创建令牌失败")
		return
	}
	s.audit(c, "create-token", req.Name, "")
	dto := toAPITokenDTO(t)
	// 明文只在这一次响应里出现。
	OK(c, gin.H{
		"id": dto.ID, "name": dto.Name, "prefix": dto.Prefix,
		"expiresAt": dto.ExpiresAt, "createdAt": dto.CreatedAt, "token": plain,
	})
}

func (s *Server) deleteToken(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	u := CurrentUser(c)
	res := s.st.DB().Where("id = ? AND user_id = ?", id, u.ID).Delete(&store.APIToken{})
	if res.RowsAffected == 0 {
		NotFound(c, "令牌不存在")
		return
	}
	s.audit(c, "delete-token", "", "")
	OK(c, nil)
}

// ---------- 审计日志 ----------

func (s *Server) listAuditLogs(c *gin.Context) {
	page, size, offset := pageParams(c)
	q := s.st.DB().Model(&store.AuditLog{})
	if v := c.Query("action"); v != "" {
		q = q.Where("action = ?", v)
	}
	if v := c.Query("username"); v != "" {
		q = q.Where("username LIKE ?", "%"+v+"%")
	}
	var total int64
	q.Count(&total)
	var list []store.AuditLog
	q.Order("id desc").Limit(size).Offset(offset).Find(&list)
	if list == nil {
		list = []store.AuditLog{}
	}
	OK(c, NewPage(list, total, page, size))
}

// 让 gorm 的导入在未直接使用时也保持（findUserByID 等辅助函数用到）。
var _ = gorm.ErrRecordNotFound
