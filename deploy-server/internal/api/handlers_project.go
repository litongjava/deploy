package api

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"deploy-server/internal/ids"
	"deploy-server/internal/runner"
	"deploy-server/internal/store"
)

var keyRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ---------- 入参结构（用指针区分「未提供」与「零值」） ----------

type commandInput struct {
	ID              *uint  `json:"id"`
	Name            string `json:"name"`
	Script          string `json:"script"`
	SortOrder       int    `json:"sortOrder"`
	Enabled         *bool  `json:"enabled"`
	FailStrategy    string `json:"failStrategy"`
	WorkdirOverride string `json:"workdirOverride"`
	TimeoutSec      int    `json:"timeoutSec"`
	ShellOverride   string `json:"shellOverride"`
}

type envInput struct {
	ID       *uint  `json:"id"`
	Key      string `json:"key"`
	Value    string `json:"value"`
	IsSecret bool   `json:"isSecret"`
}

type projectInput struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Workdir     string `json:"workdir"`
	Shell       string `json:"shell"`
	TimeoutSec  int    `json:"timeoutSec"`
	Concurrency string `json:"concurrency"`

	HookEnabled      *bool `json:"hookEnabled"`
	Archived         *bool `json:"archived"`
	AllowEnvOverride *bool `json:"allowEnvOverride"`

	UseGit          *bool  `json:"useGit"`
	GitURL          string `json:"gitUrl"`
	GitBranch       string `json:"gitBranch"`
	GitCredentialID *uint  `json:"gitCredentialId"`
	GitSubdir       string `json:"gitSubdir"`
	GitDepth        int    `json:"gitDepth"`
	GitClean        *bool  `json:"gitClean"`
	GitSubmodule    *bool  `json:"gitSubmodule"`

	Commands *[]commandInput `json:"commands"`
	Envs     *[]envInput     `json:"envs"`
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// ---------- 列表 ----------

func (s *Server) listProjects(c *gin.Context) {
	u := CurrentUser(c)
	page, size, offset := pageParams(c)

	q := s.st.DB().Model(&store.Project{})
	if !boolQuery(c, "archived", false) {
		q = q.Where("archived = ?", false)
	}
	if boolQuery(c, "mine", false) {
		q = q.Where("owner_id = ?", u.ID)
	}
	if kw := strings.TrimSpace(c.Query("keyword")); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("key LIKE ? OR name LIKE ?", like, like)
	}
	// 可见性：ownerOnly 时非管理员只能看到自己的。
	if u.Role != "admin" && s.visibilityMode() == "ownerOnly" {
		q = q.Where("owner_id = ?", u.ID)
	}

	var total int64
	q.Count(&total)

	var projects []store.Project
	q.Order("updated_at desc, id desc").Limit(size).Offset(offset).Find(&projects)

	ids := make([]uint, 0, len(projects))
	for _, p := range projects {
		ids = append(ids, p.ID)
	}
	stats := s.projectStats(ids)

	list := make([]projectDTO, 0, len(projects))
	for i := range projects {
		p := &projects[i]
		d := toProjectDTO(p, s.canEdit(u, p), stats[p.ID])
		list = append(list, d)
	}
	OK(c, NewPage(list, total, page, size))
}

// projectStats 一次查出分页内所有项目的运行统计，避免 N+1。
func (s *Server) projectStats(ids []uint) map[uint]*projectStats {
	out := map[uint]*projectStats{}
	if len(ids) == 0 {
		return out
	}
	type row struct {
		ProjectID uint
		Cnt       int64
		LastAt    *time.Time
	}
	var rows []row
	s.st.DB().Model(&store.Run{}).
		Select("project_id, COUNT(*) as cnt, MAX(created_at) as last_at").
		Where("project_id IN ?", ids).Group("project_id").Scan(&rows)
	for _, r := range rows {
		out[r.ProjectID] = &projectStats{LastRunAt: r.LastAt, RunCount: r.Cnt}
	}

	// 最近一次运行的状态：取每个项目 id 最大的那条 run。
	sub := s.st.DB().Model(&store.Run{}).Select("MAX(id)").Where("project_id IN ?", ids).Group("project_id")
	var lastRuns []store.Run
	s.st.DB().Where("id IN (?)", sub).Find(&lastRuns)
	for i := range lastRuns {
		r := lastRuns[i]
		st, ok := out[r.ProjectID]
		if !ok {
			st = &projectStats{}
			out[r.ProjectID] = st
		}
		st.LastRunStatus = r.Status
		if r.CreatedAt.After(derefTime(st.LastRunAt)) {
			t := r.CreatedAt
			st.LastRunAt = &t
		}
	}
	return out
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// ---------- 详情 ----------

func (s *Server) getProject(c *gin.Context) {
	p, u, ok := s.loadProjectFor(c, c.Param("key"))
	if !ok {
		return
	}
	cmds := s.commandsOf(p.ID)
	envs := s.envsOf(p.ID)
	d := toProjectDTO(p, s.canEdit(u, p), s.projectStats([]uint{p.ID})[p.ID])
	d.Commands = cmds
	d.Envs = envs
	OK(c, d)
}

func (s *Server) commandsOf(projectID uint) []commandDTO {
	var list []store.ProjectCommand
	s.st.DB().Where("project_id = ?", projectID).Order("sort_order asc, id asc").Find(&list)
	out := make([]commandDTO, 0, len(list))
	for _, c := range list {
		out = append(out, toCommandDTO(c))
	}
	return out
}

func (s *Server) envsOf(projectID uint) []envDTO {
	var list []store.ProjectEnv
	s.st.DB().Where("project_id = ?", projectID).Order("id asc").Find(&list)
	out := make([]envDTO, 0, len(list))
	for _, e := range list {
		out = append(out, toEnvDTO(e))
	}
	return out
}

// ---------- 创建 / 更新 ----------

func (s *Server) createProject(c *gin.Context) {
	var req projectInput
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "请求体不是合法 JSON: "+err.Error())
		return
	}
	if msg := validateProjectInput(&req, true); msg != "" {
		BadRequest(c, msg)
		return
	}
	var dup int64
	s.st.DB().Model(&store.Project{}).Where("key = ?", req.Key).Count(&dup)
	if dup > 0 {
		Conflict(c, "项目 key 已存在: "+req.Key)
		return
	}
	u := CurrentUser(c)

	p := store.Project{
		Key: req.Key, Name: req.Name, Description: req.Description,
		OwnerID: u.ID, OwnerName: u.Username,
		Workdir:      strings.TrimSpace(req.Workdir),
		Shell:        defaultStr(req.Shell, "auto"),
		TimeoutSec:   req.TimeoutSec,
		Concurrency:  defaultStr(req.Concurrency, store.ConcurrencyQueue),
		HookEnabled:  boolOr(req.HookEnabled, true),
		Archived:     boolOr(req.Archived, false),
		AllowEnvOver: boolOr(req.AllowEnvOverride, true),

		UseGit:          boolOr(req.UseGit, false),
		GitURL:          strings.TrimSpace(req.GitURL),
		GitBranch:       defaultStr(req.GitBranch, "main"),
		GitCredentialID: req.GitCredentialID,
		GitSubdir:       req.GitSubdir,
		GitDepth:        req.GitDepth,
		GitClean:        boolOr(req.GitClean, false),
		GitSubmodule:    boolOr(req.GitSubmodule, false),
	}
	if err := s.st.DB().Create(&p).Error; err != nil {
		Internal(c, "创建项目失败: "+err.Error())
		return
	}

	if req.Commands != nil {
		s.syncCommands(&p, *req.Commands)
	}
	if req.Envs != nil {
		if msg := s.syncEnvs(&p, *req.Envs); msg != "" {
			BadRequest(c, msg)
			return
		}
	}

	s.audit(c, "create-project", p.Key, "")
	s.respondProject(c, &p)
}

func (s *Server) updateProject(c *gin.Context) {
	p, _, ok := s.loadProjectForEdit(c, c.Param("key"))
	if !ok {
		return
	}
	var req projectInput
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "请求体不是合法 JSON: "+err.Error())
		return
	}
	if msg := validateProjectInput(&req, false); msg != "" {
		BadRequest(c, msg)
		return
	}

	updates := map[string]any{
		"name":        defaultStr(req.Name, p.Name),
		"description": req.Description,
		"workdir":     defaultStr(strings.TrimSpace(req.Workdir), p.Workdir),
		"shell":       defaultStr(req.Shell, p.Shell),
		"timeout_sec": req.TimeoutSec,
	}
	if req.Concurrency != "" {
		updates["concurrency"] = req.Concurrency
	}
	if req.HookEnabled != nil {
		updates["hook_enabled"] = *req.HookEnabled
	}
	if req.Archived != nil {
		updates["archived"] = *req.Archived
	}
	if req.AllowEnvOverride != nil {
		updates["allow_env_over"] = *req.AllowEnvOverride
	}
	if req.UseGit != nil {
		updates["use_git"] = *req.UseGit
	}
	updates["git_url"] = req.GitURL
	updates["git_branch"] = defaultStr(req.GitBranch, p.GitBranch)
	updates["git_credential_id"] = req.GitCredentialID
	updates["git_subdir"] = req.GitSubdir
	updates["git_depth"] = req.GitDepth
	if req.GitClean != nil {
		updates["git_clean"] = *req.GitClean
	}
	if req.GitSubmodule != nil {
		updates["git_submodule"] = *req.GitSubmodule
	}

	if err := s.st.DB().Model(&store.Project{}).Where("id = ?", p.ID).Updates(updates).Error; err != nil {
		Internal(c, "更新项目失败: "+err.Error())
		return
	}
	s.st.DB().First(p, p.ID)

	if req.Commands != nil {
		s.syncCommands(p, *req.Commands)
	}
	if req.Envs != nil {
		if msg := s.syncEnvs(p, *req.Envs); msg != "" {
			BadRequest(c, msg)
			return
		}
	}
	s.audit(c, "update-project", p.Key, "")
	s.respondProject(c, p)
}

func (s *Server) respondProject(c *gin.Context, p *store.Project) {
	u := CurrentUser(c)
	d := toProjectDTO(p, s.canEdit(u, p), s.projectStats([]uint{p.ID})[p.ID])
	d.Commands = s.commandsOf(p.ID)
	d.Envs = s.envsOf(p.ID)
	OK(c, d)
}

func validateProjectInput(req *projectInput, isCreate bool) string {
	if isCreate {
		if !keyRe.MatchString(req.Key) {
			return "项目 key 只能包含字母、数字、点、下划线、短横线，且以字母或数字开头（1-64 位）"
		}
		if strings.TrimSpace(req.Name) == "" {
			return "请填写项目名称"
		}
	}
	if req.TimeoutSec < 0 {
		return "超时时间不能为负数"
	}
	if req.Concurrency != "" {
		switch req.Concurrency {
		case store.ConcurrencyQueue, store.ConcurrencyAllow, store.ConcurrencyCancelPrev:
		default:
			return "并发策略只能是 queue / allow / cancelPrev"
		}
	}
	// Git 开关打开时，仓库地址是硬性要求：否则 Prepare 阶段一定失败。
	if boolOr(req.UseGit, false) && strings.TrimSpace(req.GitURL) == "" {
		return "启用 Git 准备阶段时必须填写仓库地址"
	}
	if req.Commands != nil {
		for i, c := range *req.Commands {
			if strings.TrimSpace(c.Script) == "" {
				return fmt.Sprintf("第 %d 条命令的脚本内容不能为空", i+1)
			}
			if c.FailStrategy != "" && c.FailStrategy != store.FailAbort && c.FailStrategy != store.FailContinue {
				return fmt.Sprintf("第 %d 条命令的失败策略只能是 abort / continue", i+1)
			}
		}
	}
	return ""
}

func defaultStr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// syncCommands 把前端提交的命令列表与库里对齐：有 id 的更新、没 id 的新建、缺失的删除。
func (s *Server) syncCommands(p *store.Project, inputs []commandInput) {
	var existing []store.ProjectCommand
	s.st.DB().Where("project_id = ?", p.ID).Find(&existing)
	byID := map[uint]store.ProjectCommand{}
	for _, c := range existing {
		byID[c.ID] = c
	}

	keep := map[uint]bool{}
	for i, in := range inputs {
		order := in.SortOrder
		if order == 0 {
			order = i + 1
		}
		fields := map[string]any{
			"name":             defaultStr(in.Name, fmt.Sprintf("命令 %d", i+1)),
			"script":           in.Script,
			"sort_order":       order,
			"enabled":          boolOr(in.Enabled, true),
			"fail_strategy":    defaultStr(in.FailStrategy, store.FailAbort),
			"workdir_override": in.WorkdirOverride,
			"timeout_sec":      in.TimeoutSec,
			"shell_override":   in.ShellOverride,
		}
		if in.ID != nil {
			if _, ok := byID[*in.ID]; ok {
				s.st.DB().Model(&store.ProjectCommand{}).Where("id = ?", *in.ID).Updates(fields)
				keep[*in.ID] = true
				continue
			}
		}
		cmd := store.ProjectCommand{
			ProjectID: p.ID, Name: fields["name"].(string), Script: in.Script,
			SortOrder: order, Enabled: boolOr(in.Enabled, true),
			FailStrategy: defaultStr(in.FailStrategy, store.FailAbort),
			WorkdirOver:  in.WorkdirOverride, TimeoutSec: in.TimeoutSec,
			ShellOverride: in.ShellOverride,
		}
		s.st.DB().Create(&cmd)
		keep[cmd.ID] = true
	}
	for id := range byID {
		if !keep[id] {
			s.st.DB().Delete(&store.ProjectCommand{}, id)
		}
	}
}

// placeholderSecret 是前端展示 Secret 时用的占位符。
// 提交时若仍是它（或空串），表示「沿用原来的值」。
const placeholderSecret = "******"

// syncEnvs 整体替换项目环境变量。
//
// Secret 的处理规则（契约 §4.3）：
//   - 新值非空且不是占位符 → 加密保存
//   - 值是占位符或空串，且库里已有同名 Secret → 保留原密文
//   - 其余情况 → 按新值处理
func (s *Server) syncEnvs(p *store.Project, inputs []envInput) string {
	var existing []store.ProjectEnv
	s.st.DB().Where("project_id = ?", p.ID).Find(&existing)
	oldByKey := map[string]store.ProjectEnv{}
	for _, e := range existing {
		oldByKey[e.Key] = e
	}

	keep := map[uint]bool{}
	for _, in := range inputs {
		key := strings.TrimSpace(in.Key)
		if key == "" {
			continue // 空行直接忽略，前端加行时常留空
		}
		if strings.ContainsAny(key, "= \t\n") {
			return "环境变量名不能包含空格或等号: " + key
		}

		if in.IsSecret {
			enc := ""
			switch {
			case in.Value != "" && in.Value != placeholderSecret:
				e, err := s.box.Encrypt(in.Value)
				if err != nil {
					return "加密环境变量失败: " + key
				}
				enc = e
			default:
				if old, ok := oldByKey[key]; ok && old.IsSecret {
					enc = old.SecretEnc
				}
			}
			rec := store.ProjectEnv{ProjectID: p.ID, Key: key, IsSecret: true, SecretEnc: enc, Value: ""}
			if old, ok := oldByKey[key]; ok {
				s.st.DB().Model(&store.ProjectEnv{}).Where("id = ?", old.ID).
					Updates(map[string]any{"key": key, "is_secret": true, "secret_enc": enc, "value": ""})
				keep[old.ID] = true
				continue
			}
			s.st.DB().Create(&rec)
			keep[rec.ID] = true
			continue
		}

		if old, ok := oldByKey[key]; ok {
			s.st.DB().Model(&store.ProjectEnv{}).Where("id = ?", old.ID).
				Updates(map[string]any{"key": key, "is_secret": false, "value": in.Value, "secret_enc": ""})
			keep[old.ID] = true
			continue
		}
		rec := store.ProjectEnv{ProjectID: p.ID, Key: key, Value: in.Value}
		s.st.DB().Create(&rec)
		keep[rec.ID] = true
	}

	for _, e := range existing {
		if !keep[e.ID] {
			s.st.DB().Delete(&store.ProjectEnv{}, e.ID)
		}
	}
	return ""
}

// ---------- 归档 / 删除 ----------

func (s *Server) archiveProject(c *gin.Context) {
	p, _, ok := s.loadProjectForEdit(c, c.Param("key"))
	if !ok {
		return
	}
	var body struct {
		Archived bool `json:"archived"`
	}
	_ = c.ShouldBindJSON(&body)
	s.st.DB().Model(&store.Project{}).Where("id = ?", p.ID).Update("archived", body.Archived)
	s.audit(c, "archive-project", p.Key, fmt.Sprint(body.Archived))
	OK(c, gin.H{"key": p.Key, "archived": body.Archived})
}

func (s *Server) deleteProject(c *gin.Context) {
	p, _, ok := s.loadProjectForEdit(c, c.Param("key"))
	if !ok {
		return
	}
	// 正在跑的 Run 先取消，否则会留下一个往已删除项目写日志的进程。
	s.exec.CancelProject(p.ID)

	s.st.DB().Where("run_id IN (?)",
		s.st.DB().Model(&store.Run{}).Select("id").Where("project_id = ?", p.ID)).
		Delete(&store.RunStep{})
	s.st.DB().Where("project_id = ?", p.ID).Delete(&store.Run{})
	s.st.DB().Where("project_id = ?", p.ID).Delete(&store.ProjectCommand{})
	s.st.DB().Where("project_id = ?", p.ID).Delete(&store.ProjectEnv{})
	s.st.DB().Where("project_id = ?", p.ID).Delete(&store.HookToken{})
	s.st.DB().Delete(&store.Project{}, p.ID)

	// 日志文件也一起清掉（失败不影响删除结果，只记日志）。
	logDir := filepath.Join(s.cfg.LogDir(), p.Key)
	if err := os.RemoveAll(logDir); err != nil {
		fmt.Fprintf(os.Stderr, "清理日志目录失败 %s: %v\n", logDir, err)
	}
	s.audit(c, "delete-project", p.Key, "")
	OK(c, nil)
}

// ---------- 命令 ----------

func (s *Server) listCommands(c *gin.Context) {
	p, _, ok := s.loadProjectFor(c, c.Param("key"))
	if !ok {
		return
	}
	OK(c, s.commandsOf(p.ID))
}

func (s *Server) createCommand(c *gin.Context) {
	p, _, ok := s.loadProjectForEdit(c, c.Param("key"))
	if !ok {
		return
	}
	var in commandInput
	if err := c.ShouldBindJSON(&in); err != nil {
		BadRequest(c, "请求体不是合法 JSON")
		return
	}
	if strings.TrimSpace(in.Script) == "" {
		BadRequest(c, "脚本内容不能为空")
		return
	}
	var maxOrder int
	s.st.DB().Model(&store.ProjectCommand{}).Where("project_id = ?", p.ID).
		Select("COALESCE(MAX(sort_order),0)").Row().Scan(&maxOrder)

	cmd := store.ProjectCommand{
		ProjectID: p.ID, Name: in.Name, Script: in.Script,
		SortOrder: maxOrder + 1, Enabled: boolOr(in.Enabled, true),
		FailStrategy: defaultStr(in.FailStrategy, store.FailAbort),
		WorkdirOver:  in.WorkdirOverride, TimeoutSec: in.TimeoutSec,
		ShellOverride: in.ShellOverride,
	}
	if err := s.st.DB().Create(&cmd).Error; err != nil {
		Internal(c, "创建命令失败")
		return
	}
	s.audit(c, "create-command", p.Key, cmd.Name)
	OK(c, toCommandDTO(cmd))
}

func (s *Server) updateCommand(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var cmd store.ProjectCommand
	if err := s.st.DB().First(&cmd, id).Error; err != nil {
		NotFound(c, "命令不存在")
		return
	}
	p, _, ok := s.loadProjectForEdit(c, keyOf(cmd.ProjectID, s))
	if !ok {
		return
	}
	var in commandInput
	if err := c.ShouldBindJSON(&in); err != nil {
		BadRequest(c, "请求体不是合法 JSON")
		return
	}
	updates := map[string]any{
		"name":             defaultStr(in.Name, cmd.Name),
		"script":           defaultStr(in.Script, cmd.Script),
		"enabled":          boolOr(in.Enabled, cmd.Enabled),
		"fail_strategy":    defaultStr(in.FailStrategy, cmd.FailStrategy),
		"workdir_override": in.WorkdirOverride,
		"timeout_sec":      in.TimeoutSec,
		"shell_override":   in.ShellOverride,
	}
	if in.SortOrder != 0 {
		updates["sort_order"] = in.SortOrder
	}
	s.st.DB().Model(&store.ProjectCommand{}).Where("id = ?", id).Updates(updates)
	s.st.DB().First(&cmd, id)
	s.audit(c, "update-command", p.Key, cmd.Name)
	OK(c, toCommandDTO(cmd))
}

func (s *Server) deleteCommand(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var cmd store.ProjectCommand
	if err := s.st.DB().First(&cmd, id).Error; err != nil {
		NotFound(c, "命令不存在")
		return
	}
	p, _, ok := s.loadProjectForEdit(c, keyOf(cmd.ProjectID, s))
	if !ok {
		return
	}
	s.st.DB().Delete(&store.ProjectCommand{}, id)
	s.audit(c, "delete-command", p.Key, cmd.Name)
	OK(c, nil)
}

func (s *Server) reorderCommands(c *gin.Context) {
	p, _, ok := s.loadProjectForEdit(c, c.Param("key"))
	if !ok {
		return
	}
	var body struct {
		IDs []uint `json:"ids"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		BadRequest(c, "请求体需要 ids 数组")
		return
	}
	err := s.st.DB().Transaction(func(tx *gorm.DB) error {
		for i, id := range body.IDs {
			if err := tx.Model(&store.ProjectCommand{}).
				Where("id = ? AND project_id = ?", id, p.ID).
				Update("sort_order", i+1).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		Internal(c, "重排失败: "+err.Error())
		return
	}
	s.audit(c, "reorder-commands", p.Key, "")
	OK(c, s.commandsOf(p.ID))
}

// keyOf 是个小工具，从 projectID 反查 key（用于审计信息与权限校验）。
func keyOf(projectID uint, s *Server) string {
	var p store.Project
	if err := s.st.DB().Select("key").First(&p, projectID).Error; err != nil {
		return ""
	}
	return p.Key
}

// ---------- 环境变量 ----------

func (s *Server) listEnvs(c *gin.Context) {
	p, _, ok := s.loadProjectFor(c, c.Param("key"))
	if !ok {
		return
	}
	OK(c, s.envsOf(p.ID))
}

func (s *Server) replaceEnvs(c *gin.Context) {
	p, _, ok := s.loadProjectForEdit(c, c.Param("key"))
	if !ok {
		return
	}
	var body struct {
		Envs []envInput `json:"envs"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		BadRequest(c, "请求体需要 envs 数组")
		return
	}
	if msg := s.syncEnvs(p, body.Envs); msg != "" {
		BadRequest(c, msg)
		return
	}
	s.audit(c, "update-envs", p.Key, "")
	OK(c, s.envsOf(p.ID))
}

// ---------- 预演（dry-run） ----------

func (s *Server) previewProject(c *gin.Context) {
	p, _, ok := s.loadProjectFor(c, c.Param("key"))
	if !ok {
		return
	}
	var body struct {
		Branch string            `json:"branch"`
		Env    map[string]string `json:"env"`
	}
	_ = c.ShouldBindJSON(&body)

	workdir := runner.ExpandWorkdir(p.Workdir, s.cfg.WebRoot, p)
	ref := defaultStr(body.Branch, p.GitBranch)

	envs := s.envsOf(p.ID)
	builtins := []gin.H{
		{"key": "PROJECT_KEY", "value": p.Key},
		{"key": "PROJECT_NAME", "value": p.Name},
		{"key": "WORKDIR", "value": workdir},
		{"key": "DEPLOY_SERVER_URL", "value": s.cfg.ServerURL},
		{"key": "BUILD_NUMBER", "value": "(执行时分配)"},
	}

	var sb strings.Builder
	n := 0
	if p.UseGit {
		n++
		fmt.Fprintf(&sb, "# prepare. Git 准备\n")
		fmt.Fprintf(&sb, "cd %s\n", workdir)
		if p.GitSubdir != "" {
			fmt.Fprintf(&sb, "# 之后在子目录 %s 中执行\n", p.GitSubdir)
		}
		fmt.Fprintf(&sb, "# clone 或 fetch + checkout %s\n\n", ref)
	}
	cmds := s.commandsOf(p.ID)
	for _, cmd := range cmds {
		if !cmd.Enabled {
			continue
		}
		n++
		fmt.Fprintf(&sb, "# %d. %s\n", n, cmd.Name)
		cb := workdir
		if cmd.WorkdirOver != "" {
			cb = cmd.WorkdirOver
		}
		fmt.Fprintf(&sb, "cd %s\n", cb)
		fmt.Fprintf(&sb, "%s\n\n", strings.TrimRight(cmd.Script, "\n"))
	}
	if n == 0 {
		sb.WriteString("# 该项目没有任何启用的命令\n")
	}

	OK(c, gin.H{
		"workdir": workdir,
		"git": gin.H{
			"enabled": p.UseGit, "url": p.GitURL,
			"branch": ref, "subdir": p.GitSubdir,
		},
		"envs":        envs,
		"builtinEnvs": builtins,
		"script":      sb.String(),
	})
}

// 保证 ids 包被引用（Hook 相关在另一文件里用）。
var _ = ids.NewAPIToken
