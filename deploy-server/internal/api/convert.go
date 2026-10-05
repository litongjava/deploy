package api

import (
	"encoding/json"
	"time"

	"deploy-server/internal/store"
)

// defaultKind 兜底：老数据（迁移前建的项目）没有 kind 字段，按应用项目算。
func defaultKind(k string) string {
	if k == "" {
		return store.KindApp
	}
	return k
}

// nonNilKeys 让 JSON 里出现 [] 而不是 null，前端少写一层判空。
func nonNilKeys(keys []string) []string {
	if keys == nil {
		return []string{}
	}
	return keys
}

// ---------- User ----------

type userDTO struct {
	ID                 uint       `json:"id"`
	Username           string     `json:"username"`
	Nickname           string     `json:"nickname"`
	Email              string     `json:"email"`
	Role               string     `json:"role"`
	Status             string     `json:"status"`
	MustChangePassword bool       `json:"mustChangePassword"`
	LastLoginAt        *time.Time `json:"lastLoginAt"`
	CreatedAt          time.Time  `json:"createdAt"`
}

func toUserDTO(u *store.User) userDTO {
	return userDTO{
		ID: u.ID, Username: u.Username, Nickname: u.Nickname, Email: u.Email,
		Role: u.Role, Status: u.Status, MustChangePassword: u.MustChangePassword,
		LastLoginAt: u.LastLoginAt, CreatedAt: u.CreatedAt,
	}
}

// ---------- Project ----------

type commandDTO struct {
	ID            uint      `json:"id"`
	ProjectID     uint      `json:"projectId"`
	Name          string    `json:"name"`
	Script        string    `json:"script"`
	SortOrder     int       `json:"sortOrder"`
	Enabled       bool      `json:"enabled"`
	FailStrategy  string    `json:"failStrategy"`
	WorkdirOver   string    `json:"workdirOverride"`
	TimeoutSec    int       `json:"timeoutSec"`
	ShellOverride string    `json:"shellOverride"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

func toCommandDTO(c store.ProjectCommand) commandDTO {
	return commandDTO{
		ID: c.ID, ProjectID: c.ProjectID, Name: c.Name, Script: c.Script,
		SortOrder: c.SortOrder, Enabled: c.Enabled, FailStrategy: c.FailStrategy,
		WorkdirOver: c.WorkdirOver, TimeoutSec: c.TimeoutSec,
		ShellOverride: c.ShellOverride, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

type envDTO struct {
	ID       uint   `json:"id"`
	Key      string `json:"key"`
	Value    string `json:"value"`
	IsSecret bool   `json:"isSecret"`
}

// toEnvDTO 把 Secret 的值抹掉。契约里写明：isSecret 的值一律返回空串，
// 前端显示占位符；提交时若仍是占位符则保持原值。
func toEnvDTO(e store.ProjectEnv) envDTO {
	v := e.Value
	if e.IsSecret {
		v = ""
	}
	return envDTO{ID: e.ID, Key: e.Key, Value: v, IsSecret: e.IsSecret}
}

// ---------- Task ----------

// taskStepDTO 是任务里的一步 shell。任务步骤内联存在 tasks.steps_json，
// 没有独立的行 ID，sortOrder 就是执行顺序（1 基）。
type taskStepDTO struct {
	Name            string `json:"name"`
	Script          string `json:"script"`
	SortOrder       int    `json:"sortOrder"`
	Enabled         bool   `json:"enabled"`
	FailStrategy    string `json:"failStrategy"`
	WorkdirOverride string `json:"workdirOverride"`
	TimeoutSec      int    `json:"timeoutSec"`
	ShellOverride   string `json:"shellOverride"`
}

func toTaskStepDTOs(steps []store.TaskStep) []taskStepDTO {
	out := make([]taskStepDTO, 0, len(steps))
	for _, s := range steps {
		out = append(out, taskStepDTO{
			Name: s.Name, Script: s.Script, SortOrder: s.SortOrder, Enabled: s.Enabled,
			FailStrategy: s.FailStrategy, WorkdirOverride: s.WorkdirOverride,
			TimeoutSec: s.TimeoutSec, ShellOverride: s.ShellOverride,
		})
	}
	return out
}

// taskEnvDTO 任务的变量。与项目一致：isSecret 的值永远返回空串。
type taskEnvDTO struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	IsSecret bool   `json:"isSecret"`
}

func toTaskEnvDTOs(envs []store.TaskEnvVar) []taskEnvDTO {
	out := make([]taskEnvDTO, 0, len(envs))
	for _, e := range envs {
		v := e.Value
		if e.IsSecret {
			v = ""
		}
		out = append(out, taskEnvDTO{Key: e.Key, Value: v, IsSecret: e.IsSecret})
	}
	return out
}

type taskDTO struct {
	ID          uint   `json:"id"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	OwnerID     uint   `json:"ownerId"`
	OwnerName   string `json:"ownerName"`

	Workdir      string `json:"workdir"`
	Shell        string `json:"shell"`
	TimeoutSec   int    `json:"timeoutSec"`
	Concurrency  string `json:"concurrency"`
	HookEnabled  bool   `json:"hookEnabled"`
	Archived     bool   `json:"archived"`
	AllowEnvOver bool   `json:"allowEnvOverride"`

	StepCount   int  `json:"stepCount"`
	Steps       []taskStepDTO `json:"steps,omitempty"`
	Envs        []taskEnvDTO  `json:"envs,omitempty"`
	LastRunAt   *time.Time    `json:"lastRunAt"`
	LastRunStatus string      `json:"lastRunStatus"`
	RunCount    int64         `json:"runCount"`
	CanEdit     bool          `json:"canEdit"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func toTaskDTO(t *store.Task, canEdit bool, st *projectStats) taskDTO {
	steps := store.DecodeTaskSteps(t.StepsJSON)
	d := taskDTO{
		ID: t.ID, Key: t.Key, Name: t.Name, Description: t.Description,
		OwnerID: t.OwnerID, OwnerName: t.OwnerName,
		Workdir: t.Workdir, Shell: t.Shell, TimeoutSec: t.TimeoutSec,
		Concurrency: t.Concurrency, HookEnabled: t.HookEnabled, Archived: t.Archived,
		AllowEnvOver: t.AllowEnvOver, StepCount: len(steps),
		CanEdit: canEdit, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
	if st != nil {
		d.LastRunAt = st.LastRunAt
		d.LastRunStatus = st.LastRunStatus
		d.RunCount = st.RunCount
	}
	return d
}

type projectDTO struct {
	ID          uint   `json:"id"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	OwnerID     uint   `json:"ownerId"`
	OwnerName   string `json:"ownerName"`

	Workdir     string `json:"workdir"`
	Shell       string `json:"shell"`
	TimeoutSec  int    `json:"timeoutSec"`
	Concurrency string `json:"concurrency"`
	HookEnabled bool   `json:"hookEnabled"`
	Archived    bool   `json:"archived"`

	// 项目类型与依赖构建（见 docs/API设计.md §4.5）
	Kind         string   `json:"kind"`
	Dependencies []string `json:"dependencies"`

	UseGit          bool   `json:"useGit"`
	GitURL          string `json:"gitUrl"`
	GitBranch       string `json:"gitBranch"`
	GitCredentialID *uint  `json:"gitCredentialId"`
	GitSubdir       string `json:"gitSubdir"`
	GitDepth        int    `json:"gitDepth"`
	GitClean        bool   `json:"gitClean"`
	GitSubmodule    bool   `json:"gitSubmodule"`
	AllowEnvOver    bool   `json:"allowEnvOverride"`

	LastRunAt     *time.Time `json:"lastRunAt"`
	LastRunStatus string     `json:"lastRunStatus"`
	RunCount      int64      `json:"runCount"`

	CanEdit  bool         `json:"canEdit"`
	Commands []commandDTO `json:"commands,omitempty"`
	Envs     []envDTO     `json:"envs,omitempty"`

	// 只有详情接口会带上这三项：构建计划、依赖我的项目、以及计划是否可用。
	DependencyPlan *depPlanDTO  `json:"dependencyPlan,omitempty"`
	Dependents     []depNodeDTO `json:"dependents,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// depNodeDTO 是依赖图里的一个节点。
type depNodeDTO struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Archived bool   `json:"archived"`
	Order    int    `json:"order,omitempty"`
}

// depPlanDTO 是「含依赖构建时按什么顺序构建」的计算结果。
// ok=false 时 error 说明原因（依赖被删、改成应用项目、归档或成环）。
type depPlanDTO struct {
	OK    bool         `json:"ok"`
	Error string       `json:"error,omitempty"`
	Order []depNodeDTO `json:"order"`
}

// projectStats 是列表页需要的「最近一次运行」聚合结果。
type projectStats struct {
	LastRunAt     *time.Time
	LastRunStatus string
	RunCount      int64
}

func toProjectDTO(p *store.Project, canEdit bool, st *projectStats) projectDTO {
	d := projectDTO{
		ID: p.ID, Key: p.Key, Name: p.Name, Description: p.Description,
		OwnerID: p.OwnerID, OwnerName: p.OwnerName,
		Workdir: p.Workdir, Shell: p.Shell, TimeoutSec: p.TimeoutSec,
		Concurrency: p.Concurrency, HookEnabled: p.HookEnabled, Archived: p.Archived,
		Kind:         defaultKind(p.Kind),
		Dependencies: nonNilKeys(store.ParseDependencies(p.Dependencies)),
		UseGit:       p.UseGit, GitURL: p.GitURL, GitBranch: p.GitBranch,
		GitCredentialID: p.GitCredentialID, GitSubdir: p.GitSubdir, GitDepth: p.GitDepth,
		GitClean: p.GitClean, GitSubmodule: p.GitSubmodule, AllowEnvOver: p.AllowEnvOver,
		CanEdit: canEdit, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
	if st != nil {
		d.LastRunAt = st.LastRunAt
		d.LastRunStatus = st.LastRunStatus
		d.RunCount = st.RunCount
	}
	return d
}

// ---------- Run ----------

type stepDTO struct {
	ID             uint       `json:"id"`
	RunID          uint       `json:"runId"`
	Phase          string     `json:"phase"`
	CommandID      uint       `json:"commandId"`
	Name           string     `json:"name"`
	SortOrder      int        `json:"sortOrder"`
	Status         string     `json:"status"`
	StartedAt      *time.Time `json:"startedAt"`
	FinishedAt     *time.Time `json:"finishedAt"`
	DurationMs     int64      `json:"durationMs"`
	ExitCode       int        `json:"exitCode"`
	LogOffsetStart int64      `json:"logOffsetStart"`
	LogOffsetEnd   int64      `json:"logOffsetEnd"`
	Script         string     `json:"script"`
}

func toStepDTO(s store.RunStep) stepDTO {
	return stepDTO{
		ID: s.ID, RunID: s.RunID, Phase: s.Phase, CommandID: s.CommandID,
		Name: s.Name, SortOrder: s.SortOrder, Status: s.Status,
		StartedAt: s.StartedAt, FinishedAt: s.FinishedAt, DurationMs: s.DurationMs,
		ExitCode: s.ExitCode, LogOffsetStart: s.LogOffsetStart,
		LogOffsetEnd: s.LogOffsetEnd, Script: s.Script,
	}
}

type runDTO struct {
	ID          uint   `json:"id"`
	ProjectID   uint   `json:"projectId"`
	ProjectKey  string `json:"projectKey"`
	ProjectName string `json:"projectName"`
	BuildNumber int64  `json:"buildNumber"`
	Status      string `json:"status"`

	// 执行对象。targetKind 取 project / task；targetKey / targetName 是两种情况下
	// 都能用的统一叫法，界面上按 targetKind 决定链接到 /projects 还是 /tasks。
	// projectKey / projectName 为兼容既有调用方保留，任务运行时与 target* 相同。
	TaskID      uint   `json:"taskId"`
	TargetKind  string `json:"targetKind"`
	TargetKey   string `json:"targetKey"`
	TargetName  string `json:"targetName"`

	TriggerType  string            `json:"triggerType"`
	TriggerUID   uint              `json:"triggerUserId"`
	TriggerUser  string            `json:"triggerUserName"`
	TriggerIP    string            `json:"triggerIp"`
	TriggerUA    string            `json:"triggerUa"`
	RequestedRef string            `json:"requestedRef"`
	Params       map[string]string `json:"params,omitempty"`

	// 依赖构建流水线：pipelineId 相同的一组 Run 属于同一次触发。
	PipelineID    string        `json:"pipelineId,omitempty"`
	PipelineRole  string        `json:"pipelineRole,omitempty"`
	PipelineOrder int           `json:"pipelineOrder,omitempty"`
	Pipeline      []pipelineRun `json:"pipeline,omitempty"`

	PrepareStatus     string `json:"prepareStatus"`
	PrepareDurationMs int64  `json:"prepareDurationMs"`
	GitCommit         string `json:"gitCommit"`
	GitCommitShort    string `json:"gitCommitShort"`
	GitBranchResolved string `json:"gitBranchResolved"`

	StartedAt  *time.Time `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt"`
	DurationMs int64      `json:"durationMs"`
	ExitCode   int        `json:"exitCode"`
	ErrorMsg   string     `json:"errorMsg"`

	LogSize   int64     `json:"logSize"`
	Steps     []stepDTO `json:"steps,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// pipelineRun 是流水线里的一条 Run 的摘要，供运行详情页展示「前置构建」。
type pipelineRun struct {
	RunID         uint   `json:"runId"`
	ProjectKey    string `json:"projectKey"`
	ProjectName   string `json:"projectName"`
	BuildNumber   int64  `json:"buildNumber"`
	Status        string `json:"status"`
	PipelineRole  string `json:"pipelineRole"`
	PipelineOrder int    `json:"pipelineOrder"`
	DurationMs    int64  `json:"durationMs"`
}

func toPipelineRun(r store.Run) pipelineRun {
	return pipelineRun{
		RunID: r.ID, ProjectKey: r.ProjectKey, ProjectName: r.ProjectName,
		BuildNumber: r.BuildNumber, Status: r.Status,
		PipelineRole: r.PipelineRole, PipelineOrder: r.PipelineOrder,
		DurationMs: r.DurationMs,
	}
}

func toRunDTO(r *store.Run) runDTO {
	kind := r.TargetKind
	if kind == "" {
		kind = store.TargetProject // 兼容本字段上线之前的历史 Run
	}
	d := runDTO{
		ID: r.ID, ProjectID: r.ProjectID, ProjectKey: r.ProjectKey,
		ProjectName: r.ProjectName, BuildNumber: r.BuildNumber, Status: r.Status,
		TaskID: r.TaskID, TargetKind: kind, TargetKey: r.ProjectKey, TargetName: r.ProjectName,
		TriggerType: r.TriggerType, TriggerUID: r.TriggerUID, TriggerUser: r.TriggerUser,
		TriggerIP: r.TriggerIP, TriggerUA: r.TriggerUA, RequestedRef: r.RequestedRef,
		PipelineID: r.PipelineID, PipelineRole: r.PipelineRole, PipelineOrder: r.PipelineOrder,
		PrepareStatus: r.PrepareStatus, PrepareDurationMs: r.PrepareDurationMs,
		GitCommit: r.GitCommit, GitCommitShort: r.GitCommitShort,
		GitBranchResolved: r.GitBranchResolved,
		StartedAt:         r.StartedAt, FinishedAt: r.FinishedAt, DurationMs: r.DurationMs,
		ExitCode: r.ExitCode, ErrorMsg: r.ErrorMsg, LogSize: r.LogSize,
		CreatedAt: r.CreatedAt,
	}
	if r.ParamsJSON != "" {
		m := map[string]string{}
		if err := json.Unmarshal([]byte(r.ParamsJSON), &m); err == nil {
			d.Params = m
		}
	}
	return d
}

// ---------- Hook / Credential / Token ----------

// hookDTO 里 TaskID 非 0 表示这是任务的令牌（此时 ProjectID 为 0）。
// 界面上用 targetKind 判断该显示成项目的令牌还是任务的令牌。
type hookDTO struct {
	ID         uint       `json:"id"`
	ProjectID  uint       `json:"projectId"`
	TaskID     uint       `json:"taskId"`
	TargetKind string     `json:"targetKind"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Enabled    bool       `json:"enabled"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	LastUsedIP string     `json:"lastUsedIp"`
	CallCount  int64      `json:"callCount"`
	CreatedAt  time.Time  `json:"createdAt"`
}

func toHookDTO(h store.HookToken) hookDTO {
	kind := store.TargetProject
	if h.TaskID != 0 {
		kind = store.TargetTask
	}
	return hookDTO{
		ID: h.ID, ProjectID: h.ProjectID, TaskID: h.TaskID, TargetKind: kind,
		Name: h.Name, Prefix: h.Prefix,
		Enabled: h.Enabled, ExpiresAt: h.ExpiresAt, LastUsedAt: h.LastUsedAt,
		LastUsedIP: h.LastUsedIP, CallCount: h.CallCount, CreatedAt: h.CreatedAt,
	}
}

type credDTO struct {
	ID        uint      `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"createdAt"`
}

func toCredDTO(c store.Credential) credDTO {
	return credDTO{ID: c.ID, Name: c.Name, Type: c.Type, Username: c.Username, CreatedAt: c.CreatedAt}
}

type apiTokenDTO struct {
	ID         uint       `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	CreatedAt  time.Time  `json:"createdAt"`
}

func toAPITokenDTO(t store.APIToken) apiTokenDTO {
	return apiTokenDTO{
		ID: t.ID, Name: t.Name, Prefix: t.Prefix, ExpiresAt: t.ExpiresAt,
		LastUsedAt: t.LastUsedAt, CreatedAt: t.CreatedAt,
	}
}
