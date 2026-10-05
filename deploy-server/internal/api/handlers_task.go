package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"deploy-server/internal/runner"
	"deploy-server/internal/store"
)

// 任务（Task）是「跑一件事」的一等公民：压测、巡检、清缓存、导数据……
//
// 它与项目共用执行引擎、Run 表、日志与 Hook 机制，区别只在语义：
//   - 项目负责构建 + 部署，可以有 Git 准备阶段与依赖构建；
//   - 任务没有 Git、没有依赖，就是按顺序跑几步 shell。
//
// 因此任务的接口刻意做小：只有 CRUD + 执行 + Hook，
// 步骤与变量内联在 payload 里整体保存，没有单条命令的 CRUD 与排序接口。

// ---------- 请求载荷 ----------

type taskStepInput struct {
	Name            string `json:"name"`
	Script          string `json:"script"`
	SortOrder       int    `json:"sortOrder"`
	Enabled         *bool  `json:"enabled"`
	FailStrategy    string `json:"failStrategy"`
	WorkdirOverride string `json:"workdirOverride"`
	TimeoutSec      int    `json:"timeoutSec"`
	ShellOverride   string `json:"shellOverride"`
}

type taskEnvInput struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	IsSecret bool   `json:"isSecret"`
}

// taskInput 与 projectInput 遵守同一套约定：
// 指针字段区分「没传」与「传了零值」；非指针字段一律视为传了，用 defaultStr 兜底。
// Steps / Envs 为 nil 表示不动，为 &[]T{} 表示清空。
type taskInput struct {
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

	Steps *[]taskStepInput `json:"steps"`
	Envs  *[]taskEnvInput  `json:"envs"`
}

// ---------- 列表 ----------

func (s *Server) listTasks(c *gin.Context) {
	u := CurrentUser(c)
	page, size, offset := pageParams(c)

	q := s.st.DB().Model(&store.Task{})
	switch c.Query("archived") {
	case "true", "1":
		q = q.Where("archived = ?", true)
	case "all":
		// 不加条件
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
	if u.Role != "admin" && s.visibilityMode() == "ownerOnly" {
		q = q.Where("owner_id = ?", u.ID)
	}

	var total int64
	q.Count(&total)
	var tasks []store.Task
	q.Order("updated_at desc, id desc").Limit(size).Offset(offset).Find(&tasks)

	ids := make([]uint, 0, len(tasks))
	for i := range tasks {
		ids = append(ids, tasks[i].ID)
	}
	stats := s.taskStats(ids)

	list := make([]taskDTO, 0, len(tasks))
	for i := range tasks {
		list = append(list, toTaskDTO(&tasks[i], s.canEditTask(u, &tasks[i]), stats[tasks[i].ID]))
	}
	OK(c, NewPage(list, total, page, size))
}

// taskStats 一次查出分页内所有任务的运行统计，避免 N+1。
func (s *Server) taskStats(ids []uint) map[uint]*projectStats {
	out := map[uint]*projectStats{}
	if len(ids) == 0 {
		return out
	}
	type row struct {
		TaskID uint
		Cnt    int64
		LastAt *time.Time
	}
	var rows []row
	s.st.DB().Model(&store.Run{}).
		Select("task_id, COUNT(*) as cnt, MAX(created_at) as last_at").
		Where("task_id IN ?", ids).Group("task_id").Scan(&rows)
	for _, r := range rows {
		out[r.TaskID] = &projectStats{LastRunAt: r.LastAt, RunCount: r.Cnt}
	}

	sub := s.st.DB().Model(&store.Run{}).Select("MAX(id)").Where("task_id IN ?", ids).Group("task_id")
	var lastRuns []store.Run
	s.st.DB().Where("id IN (?)", sub).Find(&lastRuns)
	for i := range lastRuns {
		r := lastRuns[i]
		st, ok := out[r.TaskID]
		if !ok {
			st = &projectStats{}
			out[r.TaskID] = st
		}
		st.LastRunStatus = r.Status
		if r.CreatedAt.After(derefTime(st.LastRunAt)) {
			t := r.CreatedAt
			st.LastRunAt = &t
		}
	}
	return out
}

// ---------- 详情 ----------

func (s *Server) getTask(c *gin.Context) {
	t, u, ok := s.loadTaskFor(c, c.Param("key"))
	if !ok {
		return
	}
	s.respondTask(c, t, s.canEditTask(u, t))
}

// respondTask 组装任务详情：基本信息 + 步骤 + 变量 + 运行统计。
func (s *Server) respondTask(c *gin.Context, t *store.Task, canEdit bool) {
	d := toTaskDTO(t, canEdit, s.taskStats([]uint{t.ID})[t.ID])
	d.Steps = toTaskStepDTOs(store.DecodeTaskSteps(t.StepsJSON))
	d.Envs = toTaskEnvDTOs(store.DecodeTaskEnvs(t.EnvsJSON))
	OK(c, d)
}

// ---------- 新建 / 修改 / 删除 ----------

func (s *Server) createTask(c *gin.Context) {
	u := CurrentUser(c)
	var req taskInput
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "request body is not valid JSON: "+err.Error())
		return
	}
	if msg := validateTaskInput(&req, true); msg != "" {
		BadRequest(c, msg)
		return
	}

	var dup int64
	s.st.DB().Model(&store.Task{}).Where("key = ?", req.Key).Count(&dup)
	if dup > 0 {
		Conflict(c, "task key already exists: "+req.Key)
		return
	}

	// GORM 的 default:true 与 RETURNING 会让「想存 false」变成 true，
	// 所以先记住意图，落库后再补偿一次（与 createProject 的处理一致）。
	wantHook := boolOr(req.HookEnabled, true)
	wantAllowEnv := boolOr(req.AllowEnvOverride, true)

	t := store.Task{
		Key: req.Key, Name: strings.TrimSpace(req.Name), Description: req.Description,
		OwnerID: u.ID, OwnerName: u.Username,
		Workdir:     defaultStr(req.Workdir, "${WORKSPACE_ROOT}/${TASK_KEY}"),
		Shell:       defaultStr(req.Shell, "auto"),
		TimeoutSec:  req.TimeoutSec,
		Concurrency: defaultStr(req.Concurrency, store.ConcurrencyQueue),
	}
	if err := s.st.DB().Create(&t).Error; err != nil {
		Internal(c, "failed to create the task: "+err.Error())
		return
	}
	if !wantHook || !wantAllowEnv {
		if err := s.st.DB().Model(&store.Task{}).Where("id = ?", t.ID).
			Updates(map[string]any{"hook_enabled": wantHook, "allow_env_over": wantAllowEnv}).Error; err != nil {
			Internal(c, "failed to save the task flags: "+err.Error())
			return
		}
		s.st.DB().First(&t, t.ID)
	}

	if req.Steps != nil {
		steps, msg := s.buildTaskSteps(*req.Steps)
		if msg != "" {
			BadRequest(c, msg)
			return
		}
		s.st.DB().Model(&store.Task{}).Where("id = ?", t.ID).
			Update("steps_json", store.EncodeTaskSteps(steps))
	}
	if req.Envs != nil {
		envs, msg, err := s.buildTaskEnvs(*req.Envs, nil)
		if msg != "" {
			BadRequest(c, msg)
			return
		}
		if err != nil {
			Internal(c, err.Error())
			return
		}
		s.st.DB().Model(&store.Task{}).Where("id = ?", t.ID).
			Update("envs_json", store.EncodeTaskEnvs(envs))
	}

	s.st.DB().First(&t, t.ID)
	s.audit(c, "create-task", t.Key, "")
	s.respondTask(c, &t, true)
}

func (s *Server) updateTask(c *gin.Context) {
	t, u, ok := s.loadTaskForEdit(c, c.Param("key"))
	if !ok {
		return
	}
	var req taskInput
	if err := c.ShouldBindJSON(&req); err != nil {
		BadRequest(c, "request body is not valid JSON: "+err.Error())
		return
	}
	if msg := validateTaskInput(&req, false); msg != "" {
		BadRequest(c, msg)
		return
	}

	// 与项目一致：Key 不可改；name/workdir/shell/concurrency 不能清空（用原值兜底），
	// description 与 timeoutSec 是「传了就写」——省略即清空。
	updates := map[string]any{
		"name":        defaultStr(req.Name, t.Name),
		"description": req.Description,
		"workdir":     defaultStr(req.Workdir, t.Workdir),
		"shell":       defaultStr(req.Shell, t.Shell),
		"timeout_sec": req.TimeoutSec,
		"concurrency": defaultStr(req.Concurrency, t.Concurrency),
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
	if err := s.st.DB().Model(&store.Task{}).Where("id = ?", t.ID).Updates(updates).Error; err != nil {
		Internal(c, "failed to update the task: "+err.Error())
		return
	}

	if req.Steps != nil {
		steps, msg := s.buildTaskSteps(*req.Steps)
		if msg != "" {
			BadRequest(c, msg)
			return
		}
		s.st.DB().Model(&store.Task{}).Where("id = ?", t.ID).
			Update("steps_json", store.EncodeTaskSteps(steps))
	}
	if req.Envs != nil {
		old := store.DecodeTaskEnvs(t.EnvsJSON)
		envs, msg, err := s.buildTaskEnvs(*req.Envs, old)
		if msg != "" {
			BadRequest(c, msg)
			return
		}
		if err != nil {
			Internal(c, err.Error())
			return
		}
		s.st.DB().Model(&store.Task{}).Where("id = ?", t.ID).
			Update("envs_json", store.EncodeTaskEnvs(envs))
	}

	s.st.DB().First(t, t.ID)
	s.audit(c, "update-task", t.Key, "")
	s.respondTask(c, t, s.canEditTask(u, t))
}

func (s *Server) archiveTask(c *gin.Context) {
	t, _, ok := s.loadTaskForEdit(c, c.Param("key"))
	if !ok {
		return
	}
	var body struct {
		Archived bool `json:"archived"`
	}
	_ = c.ShouldBindJSON(&body)
	s.st.DB().Model(&store.Task{}).Where("id = ?", t.ID).Update("archived", body.Archived)
	s.audit(c, "archive-task", t.Key, fmt.Sprint(body.Archived))
	OK(c, gin.H{"key": t.Key, "archived": body.Archived})
}

func (s *Server) deleteTask(c *gin.Context) {
	t, _, ok := s.loadTaskForEdit(c, c.Param("key"))
	if !ok {
		return
	}
	s.exec.CancelTask(t.ID)

	// 手动级联：SQLite 侧关掉了外键约束，删除顺序必须由调用方保证。
	// 任务的日志放在 tasks/<key> 下（见 runner.jobSpec.logKey），与项目不会重名。
	s.st.DB().Where("run_id IN (?)",
		s.st.DB().Model(&store.Run{}).Select("id").Where("task_id = ?", t.ID)).Delete(&store.RunStep{})
	s.st.DB().Where("task_id = ?", t.ID).Delete(&store.Run{})
	s.st.DB().Where("task_id = ?", t.ID).Delete(&store.HookToken{})
	s.st.DB().Delete(&store.Task{}, t.ID)

	if err := os.RemoveAll(filepath.Join(s.cfg.LogDir(), "tasks", t.Key)); err != nil {
		fmt.Fprintf(os.Stderr, "failed to remove the task log dir: %v\n", err)
	}
	s.audit(c, "delete-task", t.Key, "")
	OK(c, nil)
}

// ---------- 预览 / 执行 ----------

func (s *Server) previewTask(c *gin.Context) {
	t, _, ok := s.loadTaskFor(c, c.Param("key"))
	if !ok {
		return
	}
	var body struct {
		Env map[string]string `json:"env"`
	}
	_ = c.ShouldBindJSON(&body)

	workdir := runner.ExpandWorkdir(t.Workdir, s.cfg.WebRoot, t.Key, t.Name)
	var b strings.Builder
	fmt.Fprintf(&b, "# 任务 %s (%s)\n", t.Name, t.Key)
	fmt.Fprintf(&b, "# 没有 Git 准备阶段，命令直接在下面的工作目录里执行\n")
	fmt.Fprintf(&b, "cd %s\n", workdir)
	steps := store.DecodeTaskSteps(t.StepsJSON)
	wrote := false
	for _, st := range steps {
		if !st.Enabled {
			continue
		}
		wrote = true
		target := workdir
		if st.WorkdirOverride != "" {
			target = runner.ExpandWorkdir(st.WorkdirOverride, s.cfg.WebRoot, t.Key, t.Name)
		}
		fmt.Fprintf(&b, "\n# --- %s ---\n", defaultStr(st.Name, "步骤"))
		if target != workdir {
			fmt.Fprintf(&b, "cd %s\n", target)
		}
		fmt.Fprintf(&b, "%s\n", strings.TrimRight(st.Script, "\n"))
	}
	if !wrote {
		b.WriteString("\n# 该任务没有启用的步骤\n")
	}

	OK(c, gin.H{
		"workdir": workdir,
		"envs":    toTaskEnvDTOs(store.DecodeTaskEnvs(t.EnvsJSON)),
		"builtinEnvs": []gin.H{
			{"key": "TASK_KEY", "value": t.Key},
			{"key": "TASK_NAME", "value": t.Name},
			{"key": "WORKDIR", "value": workdir},
			{"key": "BUILD_NUMBER", "value": "(assigned at run time)"},
			{"key": "DEPLOY_SERVER_URL", "value": s.cfg.ServerURL},
		},
		"script": b.String(),
	})
}

func (s *Server) triggerTaskRun(c *gin.Context) {
	t, u, ok := s.loadTaskForEdit(c, c.Param("key"))
	if !ok {
		return
	}
	if t.Archived {
		BadRequest(c, "the task is archived and cannot run; unarchive it first")
		return
	}
	var req triggerReq
	_ = c.ShouldBindJSON(&req)
	if req.WithDependencies {
		BadRequest(c, "tasks have no dependencies; withDependencies is not supported")
		return
	}
	ids := req.CommandIDs
	if req.CommandID != 0 {
		ids = append(ids, req.CommandID)
	}

	// 预先拦掉「指定了不存在的步骤」：比让 Run 静默跑 0 步要好查得多。
	if len(ids) > 0 {
		valid := map[uint]bool{}
		for i := range store.DecodeTaskSteps(t.StepsJSON) {
			valid[uint(i+1)] = true
		}
		for _, id := range ids {
			if !valid[id] {
				BadRequest(c, "step not found in this task: "+strconv.Itoa(int(id)))
				return
			}
		}
	}

	run, err := s.exec.Submit(runner.SubmitRequest{
		TaskID:      t.ID,
		Params:      req.Env,
		CommandIDs:  ids,
		TriggerType: triggerTypeFrom(c),
		TriggerUser: u.Username,
		TriggerUID:  u.ID,
		TriggerIP:   ClientIP(c),
		TriggerUA:   c.GetHeader("User-Agent"),
	})
	if err != nil {
		Internal(c, "trigger failed: "+err.Error())
		return
	}
	s.audit(c, "trigger-task", t.Key, "run="+strconv.Itoa(int(run.ID)))
	OK(c, gin.H{
		"runId": run.ID, "buildNumber": run.BuildNumber, "status": run.Status,
		"taskKey": t.Key, "targetKind": store.TargetTask,
		"logUrl": "/api/v1/runs/" + strconv.Itoa(int(run.ID)) + "/log",
	})
}

// ---------- 校验与 JSON 组装 ----------

func validateTaskInput(req *taskInput, isCreate bool) string {
	if isCreate {
		if !keyRe.MatchString(req.Key) {
			return "task key may only contain letters, digits, dots, underscores and hyphens, and must start with a letter or digit (1-64 chars)"
		}
		if strings.TrimSpace(req.Name) == "" {
			return "task name is required"
		}
	}
	if req.TimeoutSec < 0 {
		return "timeout cannot be negative"
	}
	switch req.Concurrency {
	case "", store.ConcurrencyQueue, store.ConcurrencyAllow, store.ConcurrencyCancelPrev:
	default:
		return "concurrency policy must be queue / allow / cancelPrev"
	}
	if req.Steps != nil {
		for i, st := range *req.Steps {
			if strings.TrimSpace(st.Script) == "" {
				// 空脚本行是前端「点了新增但没填」的残留，允许并会被丢掉；
				// 但只填了名字没填脚本，多半是漏填，明确报错更好排查。
				if strings.TrimSpace(st.Name) != "" {
					return fmt.Sprintf("script of step #%d must not be empty", i+1)
				}
				continue
			}
			switch st.FailStrategy {
			case "", store.FailAbort, store.FailContinue:
			default:
				return fmt.Sprintf("failure policy of step #%d must be abort / continue", i+1)
			}
			if st.TimeoutSec < 0 {
				return fmt.Sprintf("timeout of step #%d cannot be negative", i+1)
			}
		}
	}
	return ""
}

// buildTaskSteps 把载荷里的步骤转成落库形态。
// 返回的文案非空表示用户可修正的错误（回 400）。
func (s *Server) buildTaskSteps(inputs []taskStepInput) ([]store.TaskStep, string) {
	out := make([]store.TaskStep, 0, len(inputs))
	for _, in := range inputs {
		out = append(out, store.TaskStep{
			Name: in.Name, Script: in.Script, SortOrder: in.SortOrder,
			Enabled:      boolOr(in.Enabled, true),
			FailStrategy: defaultStr(in.FailStrategy, store.FailAbort),
			WorkdirOverride: in.WorkdirOverride, TimeoutSec: in.TimeoutSec,
			ShellOverride: in.ShellOverride,
		})
	}
	return store.NormalizeSteps(out), ""
}

// buildTaskEnvs 把载荷里的变量转成落库形态（Secret 加密）。
//
// old 是上一次的变量：当某个 Secret 传回占位符或空值时，
// 说明前端没有改动它，沿用原密文 —— 与项目环境的语义保持一致。
func (s *Server) buildTaskEnvs(inputs []taskEnvInput, old []store.TaskEnvVar) ([]store.TaskEnvVar, string, error) {
	oldByKey := map[string]store.TaskEnvVar{}
	for _, e := range old {
		oldByKey[e.Key] = e
	}

	out := make([]store.TaskEnvVar, 0, len(inputs))
	seen := map[string]bool{}
	for _, in := range inputs {
		key := strings.TrimSpace(in.Key)
		if key == "" {
			continue
		}
		if strings.ContainsAny(key, "= \t\n") {
			return nil, "env var names must not contain spaces or equals signs: " + key, nil
		}
		if seen[key] {
			return nil, "duplicate env var name: " + key, nil
		}
		seen[key] = true

		if in.IsSecret {
			enc := ""
			if in.Value != "" && in.Value != placeholderSecret {
				e, err := s.box.Encrypt(in.Value)
				if err != nil {
					return nil, "failed to encrypt the env var: " + key, nil
				}
				enc = e
			} else if prev, ok := oldByKey[key]; ok && prev.IsSecret {
				enc = prev.SecretEnc
			}
			out = append(out, store.TaskEnvVar{Key: key, IsSecret: true, SecretEnc: enc})
			continue
		}
		out = append(out, store.TaskEnvVar{Key: key, Value: in.Value})
	}
	return out, "", nil
}
