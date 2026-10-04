package api

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"deploy-server/internal/store"
)

func (s *Server) listCredentials(c *gin.Context) {
	u := CurrentUser(c)
	q := s.st.DB().Model(&store.Credential{})
	if u.Role != "admin" {
		// 非管理员只能看到自己的凭据与全局凭据（user_id = 0）。
		q = q.Where("user_id IN ?", []uint{0, u.ID})
	}
	var list []store.Credential
	q.Order("id desc").Find(&list)
	out := make([]credDTO, 0, len(list))
	for _, c := range list {
		out = append(out, toCredDTO(c))
	}
	OK(c, out)
}

type credentialReq struct {
	Name       string `json:"name" binding:"required,max=64"`
	Type       string `json:"type" binding:"required"`
	Username   string `json:"username"`
	Secret     string `json:"secret"`
	Passphrase string `json:"passphrase"`
	Global     bool   `json:"global"`
}

func (s *Server) createCredential(c *gin.Context) {
	var req credentialReq
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "请填写凭据名称与类型")
		return
	}
	if req.Type != "https_token" && req.Type != "ssh_key" {
		BadRequest(c, "凭据类型只能是 https_token 或 ssh_key")
		return
	}
	if strings.TrimSpace(req.Secret) == "" {
		BadRequest(c, "凭据内容不能为空")
		return
	}
	u := CurrentUser(c)
	enc, err := s.box.Encrypt(req.Secret)
	if err != nil {
		Internal(c, "加密凭据失败")
		return
	}
	passEnc, err := s.box.Encrypt(req.Passphrase)
	if err != nil {
		Internal(c, "加密凭据失败")
		return
	}
	owner := u.ID
	if u.Role == "admin" && req.Global {
		owner = 0
	}
	cred := store.Credential{
		UserID: owner, Name: req.Name, Type: req.Type,
		Username: req.Username, SecretEnc: enc, Passphrase: passEnc,
	}
	if err := s.st.DB().Create(&cred).Error; err != nil {
		Internal(c, "创建凭据失败")
		return
	}
	s.audit(c, "create-credential", cred.Name, cred.Type)
	OK(c, toCredDTO(cred))
}

func (s *Server) updateCredential(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var cred store.Credential
	if err := s.st.DB().First(&cred, id).Error; err != nil {
		NotFound(c, "凭据不存在")
		return
	}
	if !s.canUseCredential(CurrentUser(c), &cred) {
		Forbidden(c, "无权修改该凭据")
		return
	}
	var req credentialReq
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "请求体不是合法 JSON")
		return
	}
	updates := map[string]any{
		"name":     defaultStr(req.Name, cred.Name),
		"username": req.Username,
	}
	// secret 为空表示「不改」，这是前端编辑凭据时的默认行为。
	if req.Secret != "" {
		enc, err := s.box.Encrypt(req.Secret)
		if err != nil {
			Internal(c, "加密凭据失败")
			return
		}
		updates["secret_enc"] = enc
	}
	if req.Passphrase != "" {
		enc, err := s.box.Encrypt(req.Passphrase)
		if err != nil {
			Internal(c, "加密凭据失败")
			return
		}
		updates["passphrase"] = enc
	}
	if req.Type == "https_token" || req.Type == "ssh_key" {
		updates["type"] = req.Type
	}
	s.st.DB().Model(&store.Credential{}).Where("id = ?", id).Updates(updates)
	s.st.DB().First(&cred, id)
	s.audit(c, "update-credential", cred.Name, "")
	OK(c, toCredDTO(cred))
}

func (s *Server) deleteCredential(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var cred store.Credential
	if err := s.st.DB().First(&cred, id).Error; err != nil {
		NotFound(c, "凭据不存在")
		return
	}
	if !s.canUseCredential(CurrentUser(c), &cred) {
		Forbidden(c, "无权删除该凭据")
		return
	}
	// 被项目引用时禁止删除，否则项目会在下次部署时才炸。
	var refs int64
	s.st.DB().Model(&store.Project{}).Where("git_credential_id = ?", id).Count(&refs)
	if refs > 0 {
		Conflict(c, "该凭据正被 "+itoa(int(refs))+" 个项目引用，请先解除引用")
		return
	}
	s.st.DB().Delete(&store.Credential{}, id)
	s.audit(c, "delete-credential", cred.Name, "")
	OK(c, nil)
}

func (s *Server) canUseCredential(u *store.User, cred *store.Credential) bool {
	if u == nil || cred == nil {
		return false
	}
	if u.Role == "admin" {
		return true
	}
	return cred.UserID == u.ID
}

type testGitReq struct {
	GitURL       string `json:"gitUrl" binding:"required"`
	CredentialID *uint  `json:"credentialId"`
	Branch       string `json:"branch"`
}

// testGit 让服务端真的去 ls-remote 一次，验证地址与凭据可用。
// 这是「配置完就能确定能拉」的关键一步，省得等到部署时才发现。
func (s *Server) testGit(c *gin.Context) {
	var req testGitReq
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "请填写仓库地址")
		return
	}
	u := CurrentUser(c)
	if req.CredentialID != nil {
		var cred store.Credential
		if err := s.st.DB().First(&cred, *req.CredentialID).Error; err != nil {
			NotFound(c, "凭据不存在")
			return
		}
		if !s.canUseCredential(u, &cred) {
			Forbidden(c, "无权使用该凭据")
			return
		}
	}
	ctx, cancel := timeoutCtx(30 * time.Second)
	defer cancel()

	refs, err := s.exec.TestGitAccess(ctx, req.GitURL, req.CredentialID)
	if err != nil {
		OK(c, gin.H{"ok": false, "message": err.Error()})
		return
	}
	lines := strings.Split(strings.TrimSpace(refs), "\n")
	if len(lines) > 20 {
		lines = lines[:20]
	}
	OK(c, gin.H{"ok": true, "message": "连接成功", "sampleRefs": lines})
}

func itoa(n int) string { return strconvItoa(n) }
