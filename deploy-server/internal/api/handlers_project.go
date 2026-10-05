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

	// 项目类型与依赖构建（见 docs/API设计.md §4.5）
	Kind         string    `json:"kind"`
	Dependencies *[]string `json:"dependencies"`

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
	// archived 三态：不传 / false = 只看未归档；true = 只看已归档；all = 全部。
	// 以前写成「archived=true 就完全不过滤」，于是前端的「已归档」页签和
	// CLI 的 `project list --archived` 都会把未归档的项目一起列出来。
	switch strings.ToLower(strings.TrimSpace(c.Query("archived"))) {
	case "true", "1":
		q = q.Where("archived = ?", true)
	case "all":
		// 不过滤
	default:
		q = q.Where("archived = ?", false)
	}
	if boolQuery(c, "mine", false) {
		q = q.Where("owner_id = ?", u.ID)
	}
	if kw := strings.TrimSpace(c.Query("keyword")); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("key LIKE ? OR name LIKE ?", like, like)
	}
	// 按类型筛选：新建「依赖的构建项目」下拉只需要 kind=lib 的项目。
	if kind := strings.TrimSpace(c.Query("kind")); kind != "" {
		q = q.Where("kind = ?", kind)
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
	plan := s.dependencyPlanOf(p)
	d.DependencyPlan = &plan
	d.Dependents = s.dependentsOf(p.Key)
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
		BadRequest(c, "request body is not valid JSON: "+err.Error())
		return
	}
	if msg := validateProjectInput(&req, true); msg != "" {
		BadRequest(c, msg)
		return
	}
	var dup int64
	s.st.DB().Model(&store.Project{}).Where("key = ?", req.Key).Count(&dup)
	if dup > 0 {
		Conflict(c, "project key already exists: "+req.Key)
		return
	}
	u := CurrentUser(c)

	// 先把「用户想要的布尔值」存下来：Create 之后 GORM 会用 RETURNING 把
	// 数据库默认值回写进结构体（HookEnabled 会从 false 变回 true），
	// 那时候再读结构体就分不清用户到底选了什么。
	wantHook := boolOr(req.HookEnabled, true)
	wantAllowEnv := boolOr(req.AllowEnvOverride, true)

	p := store.Project{
		Key: req.Key, Name: req.Name, Description: req.Description,
		OwnerID: u.ID, OwnerName: u.Username,
		Workdir:      strings.TrimSpace(req.Workdir),
		Shell:        defaultStr(req.Shell, "auto"),
		TimeoutSec:   req.TimeoutSec,
		Concurrency:  defaultStr(req.Concurrency, store.ConcurrencyQueue),
		HookEnabled:  wantHook,
		Archived:     boolOr(req.Archived, false),
		AllowEnvOver: wantAllowEnv,

		Kind: defaultStr(req.Kind, store.KindApp),

		UseGit:          boolOr(req.UseGit, false),
		GitURL:          strings.TrimSpace(req.GitURL),
		GitBranch:       defaultStr(req.GitBranch, "main"),
		GitCredentialID: req.GitCredentialID,
		GitSubdir:       req.GitSubdir,
		GitDepth:        req.GitDepth,
		GitClean:        boolOr(req.GitClean, false),
		GitSubmodule:    boolOr(req.GitSubmodule, false),
	}
	if req.Dependencies != nil {
		deps := store.NormalizeKeys(*req.Dependencies)
		if msg := s.validateDependencies(p.Key, deps); msg != "" {
			BadRequest(c, msg)
			return
		}
		p.Dependencies = store.EncodeDependencies(deps)
	}
	if err := s.st.DB().Create(&p).Error; err != nil {
		Internal(c, "failed to create project: "+err.Error())
		return
	}
	// GORM 的零值坑：`default:true` 的列在 Create 时会被跳过（库里落默认值 true），
	// 而且 GORM 还会用 RETURNING 把 true 回写进结构体，所以必须拿 Create 之前
	// 记下的 wantHook / wantAllowEnv 来判断要不要补一次显式更新。
	if !wantHook || !wantAllowEnv {
		if err := s.st.DB().Model(&store.Project{}).Where("id = ?", p.ID).
			Updates(map[string]any{"hook_enabled": wantHook, "allow_env_over": wantAllowEnv}).Error; err != nil {
			Internal(c, "failed to save the boolean flags: "+err.Error())
			return
		}
		// 结构体里还是被回写的默认值，重新读一次，保证响应与库一致。
		s.st.DB().First(&p, p.ID)
	}

	if req.Commands != nil {
		if err := s.syncCommands(&p, *req.Commands); err != nil {
			Internal(c, err.Error())
			return
		}
	}
	if req.Envs != nil {
		msg, err := s.syncEnvs(&p, *req.Envs)
		if err != nil {
			Internal(c, err.Error())
			return
		}
		if msg != "" {
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
		BadRequest(c, "request body is not valid JSON: "+err.Error())
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
	if req.Kind != "" {
		updates["kind"] = req.Kind
	}
	if req.Dependencies != nil {
		deps := store.NormalizeKeys(*req.Dependencies)
		if msg := s.validateDependencies(p.Key, deps); msg != "" {
			BadRequest(c, msg)
			return
		}
		updates["dependencies"] = store.EncodeDependencies(deps)
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
		Internal(c, "failed to update project: "+err.Error())
		return
	}
	s.st.DB().First(p, p.ID)

	if req.Commands != nil {
		if err := s.syncCommands(p, *req.Commands); err != nil {
			Internal(c, err.Error())
			return
		}
	}
	if req.Envs != nil {
		msg, err := s.syncEnvs(p, *req.Envs)
		if err != nil {
			Internal(c, err.Error())
			return
		}
		if msg != "" {
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
	plan := s.dependencyPlanOf(p)
	d.DependencyPlan = &plan
	d.Dependents = s.dependentsOf(p.Key)
	OK(c, d)
}

func validateProjectInput(req *projectInput, isCreate bool) string {
	if isCreate {
		if !keyRe.MatchString(req.Key) {
			return "project key may only contain letters, digits, dots, underscores and hyphens, and must start with a letter or digit (1-64 chars)"
		}
		if strings.TrimSpace(req.Name) == "" {
			return "project name is required"
		}
	}
	if req.TimeoutSec < 0 {
		return "timeout cannot be negative"
	}
	if req.Concurrency != "" {
		switch req.Concurrency {
		case store.ConcurrencyQueue, store.ConcurrencyAllow, store.ConcurrencyCancelPrev:
		default:
			return "concurrency policy must be queue / allow / cancelPrev"
		}
	}
	if req.Kind != "" && req.Kind != store.KindApp && req.Kind != store.KindLib {
		return "project kind must be app (application project) or lib (build project)"
	}
	// Git 开关打开时，仓库地址是硬性要求：否则 Prepare 阶段一定失败。
	if boolOr(req.UseGit, false) && strings.TrimSpace(req.GitURL) == "" {
		return "a repository URL is required when the Git prepare phase is enabled"
	}
	if req.Commands != nil {
		for i, c := range *req.Commands {
			if strings.TrimSpace(c.Script) == "" {
				return fmt.Sprintf("script of command #%d must not be empty", i+1)
			}
			if c.FailStrategy != "" && c.FailStrategy != store.FailAbort && c.FailStrategy != store.FailContinue {
				return fmt.Sprintf("failure policy of command #%d must be abort / continue", i+1)
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

// validateDependencies 校验一份待保存的依赖列表。
//
// 四条规则：不是自己、项目存在、类型是构建项目（lib）、加了这条边之后不成环。
// 已归档的依赖允许先存着（可能只是临时归档），真正触发构建时才会被拦下并说明原因。
func (s *Server) validateDependencies(selfKey string, keys []string) string {
	for _, k := range keys {
		if k == selfKey {
			return "a project cannot depend on itself: " + k
		}
		var p store.Project
		if err := s.st.DB().Where("key = ?", k).First(&p).Error; err != nil {
			return "dependency does not exist: " + k
		}
		if p.Kind != store.KindLib {
			return "only build projects (kind=lib) can be used as a dependency: " + k
		}
	}
	graph, err := s.st.DependencyGraph()
	if err != nil {
		return "failed to load the dependency graph: " + err.Error()
	}
	return store.ValidateDependencyTargets(graph, selfKey, keys)
}

// dependencyPlanOf 计算依赖构建计划，供详情页展示「按什么顺序构建」。
func (s *Server) dependencyPlanOf(p *store.Project) depPlanDTO {
	deps, err := s.st.ResolveDependencies(p)
	plan := depPlanDTO{OK: err == nil, Order: make([]depNodeDTO, 0, len(deps)+1)}
	if err != nil {
		plan.Error = err.Error()
	}
	for i, d := range deps {
		plan.Order = append(plan.Order, depNodeDTO{
			Key: d.Key, Name: d.Name, Kind: d.Kind, Archived: d.Archived, Order: i + 1,
		})
	}
	return plan
}

// dependentsOf 反查「谁依赖了我」，删除 / 归档前一眼能看到影响面。
func (s *Server) dependentsOf(key string) []depNodeDTO {
	var all []store.Project
	s.st.DB().Select("key", "name", "kind", "archived", "dependencies").Find(&all)
	out := make([]depNodeDTO, 0, 2)
	for _, p := range all {
		for _, k := range store.ParseDependencies(p.Dependencies) {
			if k == key {
				out = append(out, depNodeDTO{Key: p.Key, Name: p.Name, Kind: p.Kind, Archived: p.Archived})
				break
			}
		}
	}
	return out
}

// syncCommands 把前端提交的命令列表与库里对齐：有 id 的更新、没 id 的新建、缺失的删除。
//
// 这里用 map 而不是结构体来更新，是为了让「清空某项」（workdirOverride=""、
// timeoutSec=0）也能写进库 —— 结构体的零值会被 GORM 跳过。
// 代价是列名要自己写对：写错了 GORM 只会返回一个 error，**必须检查**，
// 否则会出现「保存成功但什么都没改」这种最难查的问题（这行曾经的 workdir_override
// 就是这么错的：实际列名是 workdir_over，更新整条失败且错误被吞掉）。
func (s *Server) syncCommands(p *store.Project, inputs []commandInput) error {
	var existing []store.ProjectCommand
	if err := s.st.DB().Where("project_id = ?", p.ID).Find(&existing).Error; err != nil {
		return fmt.Errorf("failed to load existing commands: %w", err)
	}
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
		wantEnabled := boolOr(in.Enabled, true) // Create 会被 GORM 回写默认值，先记下来
		fields := map[string]any{
			"name":           defaultStr(in.Name, fmt.Sprintf("command %d", i+1)),
			"script":         in.Script,
			"sort_order":     order,
			"enabled":        wantEnabled,
			"fail_strategy":  defaultStr(in.FailStrategy, store.FailAbort),
			"workdir_over":   in.WorkdirOverride,
			"timeout_sec":    in.TimeoutSec,
			"shell_override": in.ShellOverride,
		}
		if in.ID != nil {
			if _, ok := byID[*in.ID]; ok {
				res := s.st.DB().Model(&store.ProjectCommand{}).Where("id = ?", *in.ID).Updates(fields)
				if res.Error != nil {
					return fmt.Errorf("failed to update command #%d (id=%d): %w", i+1, *in.ID, res.Error)
				}
				if res.RowsAffected == 0 {
					return fmt.Errorf("command #%d (id=%d) was not updated: no matching row", i+1, *in.ID)
				}
				keep[*in.ID] = true
				continue
			}
		}
		cmd := store.ProjectCommand{
			ProjectID: p.ID, Name: fields["name"].(string), Script: in.Script,
			SortOrder: order, Enabled: wantEnabled,
			FailStrategy: defaultStr(in.FailStrategy, store.FailAbort),
			WorkdirOver:  in.WorkdirOverride, TimeoutSec: in.TimeoutSec,
			ShellOverride: in.ShellOverride,
		}
		if err := s.st.DB().Create(&cmd).Error; err != nil {
			return fmt.Errorf("failed to create command #%d: %w", i+1, err)
		}
		// Enabled 带 default:true：Create 会跳过 false，且 GORM 会把默认值 true
		// 回写进结构体，所以用 Create 之前记下的 wantEnabled 判断。
		if !wantEnabled {
			if err := s.st.DB().Model(&store.ProjectCommand{}).Where("id = ?", cmd.ID).
				Update("enabled", false).Error; err != nil {
				return fmt.Errorf("failed to disable command #%d: %w", i+1, err)
			}
		}
		keep[cmd.ID] = true
	}
	for id := range byID {
		if !keep[id] {
			if err := s.st.DB().Delete(&store.ProjectCommand{}, id).Error; err != nil {
				return fmt.Errorf("failed to delete command id=%d: %w", id, err)
			}
		}
	}
	return nil
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
//
// 返回值：msg 是给用户看的校验提示（空串表示通过），err 是数据库/加密错误。
// 与 syncCommands 同样的教训：这里的写库错误以前也被丢掉，一旦列名或约束出问题
// 就会表现成「保存成功但没生效」。所以每一处写库都检查 error。
func (s *Server) syncEnvs(p *store.Project, inputs []envInput) (string, error) {
	var existing []store.ProjectEnv
	if err := s.st.DB().Where("project_id = ?", p.ID).Find(&existing).Error; err != nil {
		return "", fmt.Errorf("failed to load existing envs: %w", err)
	}
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
			return "env var names must not contain spaces or equals signs: " + key, nil
		}

		if in.IsSecret {
			enc := ""
			switch {
			case in.Value != "" && in.Value != placeholderSecret:
				e, err := s.box.Encrypt(in.Value)
				if err != nil {
					return "failed to encrypt the env var: " + key, nil
				}
				enc = e
			default:
				if old, ok := oldByKey[key]; ok && old.IsSecret {
					enc = old.SecretEnc
				}
			}
			rec := store.ProjectEnv{ProjectID: p.ID, Key: key, IsSecret: true, SecretEnc: enc, Value: ""}
			if old, ok := oldByKey[key]; ok {
				res := s.st.DB().Model(&store.ProjectEnv{}).Where("id = ?", old.ID).
					Updates(map[string]any{"key": key, "is_secret": true, "secret_enc": enc, "value": ""})
				if res.Error != nil {
					return "", fmt.Errorf("failed to update env var %s: %w", key, res.Error)
				}
				keep[old.ID] = true
				continue
			}
			if err := s.st.DB().Create(&rec).Error; err != nil {
				return "", fmt.Errorf("failed to create env var %s: %w", key, err)
			}
			keep[rec.ID] = true
			continue
		}

		if old, ok := oldByKey[key]; ok {
			res := s.st.DB().Model(&store.ProjectEnv{}).Where("id = ?", old.ID).
				Updates(map[string]any{"key": key, "is_secret": false, "value": in.Value, "secret_enc": ""})
			if res.Error != nil {
				return "", fmt.Errorf("failed to update env var %s: %w", key, res.Error)
			}
			keep[old.ID] = true
			continue
		}
		rec := store.ProjectEnv{ProjectID: p.ID, Key: key, Value: in.Value}
		if err := s.st.DB().Create(&rec).Error; err != nil {
			return "", fmt.Errorf("failed to create env var %s: %w", key, err)
		}
		keep[rec.ID] = true
	}

	for _, e := range existing {
		if !keep[e.ID] {
			if err := s.st.DB().Delete(&store.ProjectEnv{}, e.ID).Error; err != nil {
				return "", fmt.Errorf("failed to delete env var %s: %w", e.Key, err)
			}
		}
	}
	return "", nil
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
		fmt.Fprintf(os.Stderr, "failed to clean the log directory %s: %v\n", logDir, err)
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
		BadRequest(c, "request body is not valid JSON")
		return
	}
	if strings.TrimSpace(in.Script) == "" {
		BadRequest(c, "script must not be empty")
		return
	}
	var maxOrder int
	s.st.DB().Model(&store.ProjectCommand{}).Where("project_id = ?", p.ID).
		Select("COALESCE(MAX(sort_order),0)").Row().Scan(&maxOrder)

	wantEnabled := boolOr(in.Enabled, true)
	cmd := store.ProjectCommand{
		ProjectID: p.ID, Name: in.Name, Script: in.Script,
		SortOrder: maxOrder + 1, Enabled: wantEnabled,
		FailStrategy: defaultStr(in.FailStrategy, store.FailAbort),
		WorkdirOver:  in.WorkdirOverride, TimeoutSec: in.TimeoutSec,
		ShellOverride: in.ShellOverride,
	}
	if err := s.st.DB().Create(&cmd).Error; err != nil {
		Internal(c, "failed to create command")
		return
	}
	// Enabled 带 default:true：Create 会跳过 false，而且 GORM 会用 RETURNING 把
	// 数据库默认值 true 回写进结构体，所以用 Create 之前记下的 wantEnabled 判断。
	if !wantEnabled {
		if err := s.st.DB().Model(&store.ProjectCommand{}).Where("id = ?", cmd.ID).
			Update("enabled", false).Error; err != nil {
			Internal(c, "failed to disable the command: "+err.Error())
			return
		}
		cmd.Enabled = false // 结构体里是被回写的默认值，这里同步回来
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
		NotFound(c, "command not found")
		return
	}
	p, _, ok := s.loadProjectForEdit(c, keyOf(cmd.ProjectID, s))
	if !ok {
		return
	}
	var in commandInput
	if err := c.ShouldBindJSON(&in); err != nil {
		BadRequest(c, "request body is not valid JSON")
		return
	}
	updates := map[string]any{
		"name":           defaultStr(in.Name, cmd.Name),
		"script":         defaultStr(in.Script, cmd.Script),
		"enabled":        boolOr(in.Enabled, cmd.Enabled),
		"fail_strategy":  defaultStr(in.FailStrategy, cmd.FailStrategy),
		"workdir_over":   in.WorkdirOverride,
		"timeout_sec":    in.TimeoutSec,
		"shell_override": in.ShellOverride,
	}
	if in.SortOrder != 0 {
		updates["sort_order"] = in.SortOrder
	}
	if res := s.st.DB().Model(&store.ProjectCommand{}).Where("id = ?", id).Updates(updates); res.Error != nil {
		Internal(c, "failed to update the command: "+res.Error.Error())
		return
	}
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
		NotFound(c, "command not found")
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
		BadRequest(c, "the request body must contain an ids array")
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
		Internal(c, "reorder failed: "+err.Error())
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
		BadRequest(c, "the request body must contain an envs array")
		return
	}
	msg, err := s.syncEnvs(p, body.Envs)
	if err != nil {
		Internal(c, err.Error())
		return
	}
	if msg != "" {
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

	workdir := runner.ExpandWorkdir(p.Workdir, s.cfg.WebRoot, p.Key, p.Name)
	ref := defaultStr(body.Branch, p.GitBranch)

	envs := s.envsOf(p.ID)
	builtins := []gin.H{
		{"key": "PROJECT_KEY", "value": p.Key},
		{"key": "PROJECT_NAME", "value": p.Name},
		{"key": "WORKDIR", "value": workdir},
		{"key": "DEPLOY_SERVER_URL", "value": s.effectiveServerURL()},
		{"key": "BUILD_NUMBER", "value": "(assigned at run time)"},
	}

	var sb strings.Builder
	n := 0
	if p.UseGit {
		n++
		fmt.Fprintf(&sb, "# prepare: git prepare\n")
		fmt.Fprintf(&sb, "cd %s\n", workdir)
		if p.GitSubdir != "" {
			fmt.Fprintf(&sb, "# the commands below run in subdirectory %s\n", p.GitSubdir)
		}
		fmt.Fprintf(&sb, "# clone or fetch + checkout %s\n\n", ref)
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
		sb.WriteString("# this project has no enabled commands\n")
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
