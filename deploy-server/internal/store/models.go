package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// 运行状态取值。
const (
	RunQueued   = "queued"
	RunRunning  = "running"
	RunSuccess  = "success"
	RunFailed   = "failed"
	RunCanceled = "canceled"
	RunTimeout  = "timeout"
	RunError    = "error"
)

// 运行阶段 / 步骤状态。
const (
	PhasePrepare = "prepare"
	PhaseCommand = "command"

	PrepareSkipped = "skipped"
	StepPending    = "pending"
	StepRunning    = "running"
	StepSkipped    = "skipped"
)

// 触发方式。
const (
	TriggerWeb   = "web"
	TriggerAPI   = "api"
	TriggerCLI   = "cli"
	TriggerHook  = "hook"
	TriggerRerun = "rerun"
)

// 项目类型。
const (
	// KindApp 应用项目：常规的构建 / 部署单元。
	KindApp = "app"
	// KindLib 构建项目：作为依赖被别的项目先构建（例如底层库、父 pom 模块）。
	// 只有这一类型可以被选为依赖，避免把带部署命令的项目当依赖跑。
	KindLib = "lib"
)

// Run 在依赖流水线里的角色。
const (
	RoleMain       = "main"       // 流水线的目标项目
	RoleDependency = "dependency" // 为满足依赖而先构建的项目
)

// Run 的执行对象类型。
//
// 平台上有两种可执行的东西：
//   - 项目（Project）：构建 + 部署，可以有 Git 准备阶段与依赖构建；
//   - 任务（Task）：跑一件事（压测、巡检、清缓存……），没有 Git、没有依赖。
//
// 两者共用同一套执行引擎与同一张 Run 表，靠这个字段区分执行对象。
// 空串按 project 处理，兼容本字段上线之前的历史数据。
const (
	TargetProject = "project"
	TargetTask    = "task"
)

// 并发策略。
const (
	ConcurrencyQueue      = "queue"
	ConcurrencyAllow      = "allow"
	ConcurrencyCancelPrev = "cancelPrev"
)

// 失败策略。
const (
	FailAbort    = "abort"
	FailContinue = "continue"
)

// User 是平台账号。
type User struct {
	ID                 uint       `gorm:"primarykey" json:"id"`
	Username           string     `gorm:"size:64;uniqueIndex;not null" json:"username"`
	PasswordHash       string     `gorm:"size:128;not null" json:"-"`
	Nickname           string     `gorm:"size:64" json:"nickname"`
	Email              string     `gorm:"size:128" json:"email"`
	Role               string     `gorm:"size:16;not null;default:user" json:"role"`     // admin | user
	Status             string     `gorm:"size:16;not null;default:active" json:"status"` // active | disabled
	MustChangePassword bool       `json:"mustChangePassword"`
	LastLoginAt        *time.Time `json:"lastLoginAt"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

// APIToken 是给 CLI / 第三方用的长效令牌，只在创建时返回一次明文。
type APIToken struct {
	ID         uint       `gorm:"primarykey" json:"id"`
	UserID     uint       `gorm:"index;not null" json:"userId"`
	Name       string     `gorm:"size:64" json:"name"`
	TokenHash  string     `gorm:"size:64;uniqueIndex;not null" json:"-"`
	Prefix     string     `gorm:"size:16" json:"prefix"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// Credential 是 Git 拉取用的凭据。Secret 用 AES-GCM 加密存放。
type Credential struct {
	ID         uint      `gorm:"primarykey" json:"id"`
	UserID     uint      `gorm:"index" json:"userId"` // 0 表示全局（admin 维护）
	Name       string    `gorm:"size:64;not null" json:"name"`
	Type       string    `gorm:"size:16;not null" json:"type"` // https_token | ssh_key
	Username   string    `gorm:"size:128" json:"username"`
	SecretEnc  string    `gorm:"type:text" json:"-"`
	Passphrase string    `gorm:"type:text" json:"-"` // 同样加密
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// Project 是部署单元。
type Project struct {
	ID          uint   `gorm:"primarykey" json:"id"`
	Key         string `gorm:"size:64;uniqueIndex;not null" json:"key"`
	Name        string `gorm:"size:128;not null" json:"name"`
	Description string `gorm:"size:512" json:"description"`
	OwnerID     uint   `gorm:"index;not null" json:"ownerId"`
	OwnerName   string `gorm:"size:64" json:"ownerName"`

	Workdir      string `gorm:"size:512;not null" json:"workdir"`
	Shell        string `gorm:"size:16;default:auto" json:"shell"`
	TimeoutSec   int    `json:"timeoutSec"`
	Concurrency  string `gorm:"size:16;default:queue" json:"concurrency"`
	HookEnabled  bool   `gorm:"default:true" json:"hookEnabled"`
	Archived     bool   `gorm:"default:false" json:"archived"`
	AllowEnvOver bool   `gorm:"default:true" json:"allowEnvOverride"`

	// 项目类型：app（应用项目，默认）| lib（构建项目，可被别的项目依赖）
	Kind string `gorm:"size:16;default:app" json:"kind"`
	// 依赖的构建项目 key，JSON 数组。构建本项目前会按拓扑序先构建它们。
	Dependencies string `gorm:"type:text" json:"-"`

	UseGit          bool   `gorm:"default:false" json:"useGit"`
	GitURL          string `gorm:"size:512" json:"gitUrl"`
	GitBranch       string `gorm:"size:128" json:"gitBranch"`
	GitCredentialID *uint  `json:"gitCredentialId"`
	GitSubdir       string `gorm:"size:256" json:"gitSubdir"`
	GitDepth        int    `json:"gitDepth"`
	GitClean        bool   `gorm:"default:false" json:"gitClean"`
	GitSubmodule    bool   `gorm:"default:false" json:"gitSubmodule"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ProjectCommand 是项目下的一条 shell 步骤。
type ProjectCommand struct {
	ID            uint      `gorm:"primarykey" json:"id"`
	ProjectID     uint      `gorm:"index;not null" json:"projectId"`
	Name          string    `gorm:"size:128" json:"name"`
	Script        string    `gorm:"type:text" json:"script"`
	SortOrder     int       `json:"sortOrder"`
	Enabled       bool      `gorm:"default:true" json:"enabled"`
	FailStrategy  string    `gorm:"size:16;default:abort" json:"failStrategy"`
	WorkdirOver   string    `gorm:"size:512" json:"workdirOverride"`
	TimeoutSec    int       `json:"timeoutSec"`
	ShellOverride string    `gorm:"size:16" json:"shellOverride"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// Task 是一次性作业：跑一件事，不负责构建与部署。
//
// 与 Project 的差别是有意做窄的：
//   - 没有 Git 准备阶段（任务通常操作现成的服务/文件，不需要拉代码）；
//   - 没有依赖构建（没有「先构建谁」这回事）；
//   - 没有 ProjectKind（任务就是任务）；
//   - 步骤与变量内联存 JSON，不单独建表：任务是单一作业，整体保存即可，
//     不需要像项目那样支持单条命令的增删改与排序接口。
//
// 相同的是执行语义：同样的并发策略、失败策略、超时、日志与 Hook 触发。
type Task struct {
	ID          uint   `gorm:"primarykey" json:"id"`
	Key         string `gorm:"size:64;uniqueIndex;not null" json:"key"`
	Name        string `gorm:"size:128;not null" json:"name"`
	Description string `gorm:"size:512" json:"description"`
	OwnerID     uint   `gorm:"index;not null" json:"ownerId"`
	OwnerName   string `gorm:"size:64" json:"ownerName"`

	Workdir      string `gorm:"size:512;not null" json:"workdir"`
	Shell        string `gorm:"size:16;default:auto" json:"shell"`
	TimeoutSec   int    `json:"timeoutSec"`
	Concurrency  string `gorm:"size:16;default:queue" json:"concurrency"`
	HookEnabled  bool   `gorm:"default:true" json:"hookEnabled"`
	Archived     bool   `gorm:"default:false" json:"archived"`
	AllowEnvOver bool   `gorm:"default:true" json:"allowEnvOverride"`

	// StepsJSON 是 []TaskStep 的 JSON；EnvsJSON 是 []TaskEnvVar 的 JSON。
	// 两者都在 store 层做编解码，出参永远走 DTO，不会把密文直接吐出去。
	StepsJSON string `gorm:"type:text" json:"-"`
	EnvsJSON  string `gorm:"type:text" json:"-"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TaskStep 是任务里的一步 shell。存进 Task.StepsJSON，不单独建表。
type TaskStep struct {
	Name            string `json:"name"`
	Script          string `json:"script"`
	SortOrder       int    `json:"sortOrder"`
	Enabled         bool   `json:"enabled"`
	FailStrategy    string `json:"failStrategy"`
	WorkdirOverride string `json:"workdirOverride"`
	TimeoutSec      int    `json:"timeoutSec"`
	ShellOverride   string `json:"shellOverride"`
}

// TaskEnvVar 是任务的一个环境变量。SecretEnc 是密文，明文不落库。
type TaskEnvVar struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	SecretEnc string `json:"secretEnc"`
	IsSecret  bool   `json:"isSecret"`
}

// NormalizeSteps 补齐缺省值并丢掉落空的行，返回可直接执行的步骤列表。
//
// 前端会留下「只点了新增命令但什么都没填」的空行，这里统一清掉，
// 免得执行时白跑一步、日志里多一段莫名其妙的空输出。
func NormalizeSteps(steps []TaskStep) []TaskStep {
	out := make([]TaskStep, 0, len(steps))
	for _, s := range steps {
		if strings.TrimSpace(s.Script) == "" {
			continue
		}
		if s.FailStrategy == "" {
			s.FailStrategy = FailAbort
		}
		if s.Name == "" {
			s.Name = fmt.Sprintf("步骤 %d", len(out)+1)
		}
		s.SortOrder = len(out) + 1
		out = append(out, s)
	}
	return out
}

// ProjectEnv 是项目环境变量。IsSecret 的值在出参时永远为空串。
type ProjectEnv struct {
	ID        uint   `gorm:"primarykey" json:"id"`
	ProjectID uint   `gorm:"index;not null" json:"projectId"`
	Key       string `gorm:"size:128;not null" json:"key"`
	Value     string `gorm:"type:text" json:"value"`
	SecretEnc string `gorm:"type:text" json:"-"`
	IsSecret  bool   `json:"isSecret"`
}

// EncodeTaskSteps / DecodeTaskSteps 负责 []TaskStep 与 Task.StepsJSON 的转换。
// 解码失败返回空列表而不是报错：一条 JSON 坏掉不应该让整个任务列表页打不开。
func EncodeTaskSteps(steps []TaskStep) string {
	if len(steps) == 0 {
		return ""
	}
	raw, err := json.Marshal(steps)
	if err != nil {
		return ""
	}
	return string(raw)
}

func DecodeTaskSteps(raw string) []TaskStep {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []TaskStep
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return NormalizeSteps(out)
}

// EncodeTaskEnvs / DecodeTaskEnvs 同上，负责 []TaskEnvVar 与 Task.EnvsJSON。
func EncodeTaskEnvs(envs []TaskEnvVar) string {
	if len(envs) == 0 {
		return ""
	}
	raw, err := json.Marshal(envs)
	if err != nil {
		return ""
	}
	return string(raw)
}

func DecodeTaskEnvs(raw string) []TaskEnvVar {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []TaskEnvVar
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// HookToken 是免登录触发部署的令牌。
//
// 项目与任务共用这张表：TaskID 非 0 表示这个令牌属于一个任务，
// 此时 ProjectID 为 0。触发时按 TaskID 是否为零决定执行对象。
type HookToken struct {
	ID         uint       `gorm:"primarykey" json:"id"`
	ProjectID  uint       `gorm:"index;not null" json:"projectId"`
	TaskID     uint       `gorm:"index" json:"taskId"`
	Name       string     `gorm:"size:64" json:"name"`
	TokenHash  string     `gorm:"size:64;uniqueIndex;not null" json:"-"`
	Prefix     string     `gorm:"size:16;index" json:"prefix"`
	Enabled    bool       `gorm:"default:true" json:"enabled"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	LastUsedIP string     `gorm:"size:64" json:"lastUsedIp"`
	CallCount  int64      `json:"callCount"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// Run 是一次执行实例。
//
// 它既可能来自项目（构建/部署），也可能来自任务（一次性作业）：
// TargetKind 说明是哪种，ProjectID / TaskID 只有一个非零。
// ProjectKey / ProjectName 是执行对象的「快照」，对任务来说存的是任务的 key 与名称 ——
// 之所以不复用成 TargetKey/TargetName，是为了不动历史数据；出参统一走 DTO 的
// targetKey / targetName，界面上不会看到 projectKey 这个容易误解的名字。
type Run struct {
	ID           uint   `gorm:"primarykey" json:"id"`
	ProjectID    uint   `gorm:"index;not null" json:"projectId"`
	ProjectKey   string `gorm:"size:64;index" json:"projectKey"`
	ProjectName  string `gorm:"size:128" json:"projectName"`
	TaskID       uint   `gorm:"index" json:"taskId"`
	TargetKind   string `gorm:"size:16;index" json:"targetKind"`
	BuildNumber  int64  `gorm:"index" json:"buildNumber"`
	Status       string `gorm:"size:16;index;not null" json:"status"`
	TriggerType  string `gorm:"size:16" json:"triggerType"`
	TriggerUser  string `gorm:"size:64" json:"triggerUserName"`
	TriggerUID   uint   `json:"triggerUserId"`
	TriggerIP    string `gorm:"size:64" json:"triggerIp"`
	TriggerUA    string `gorm:"size:256" json:"triggerUa"`
	RequestedRef string `gorm:"size:128" json:"requestedRef"`
	ParamsJSON   string `gorm:"type:text" json:"-"`

	// 依赖构建流水线：同一个 pipeline_id 下的 Run 属于同一次触发。
	// 目标项目的 Run 是 main，为满足依赖先构建的是 dependency。
	PipelineID    string `gorm:"size:32;index" json:"pipelineId"`
	PipelineRole  string `gorm:"size:16" json:"pipelineRole"`
	PipelineOrder int    `json:"pipelineOrder"`

	PrepareStatus     string `gorm:"size:16" json:"prepareStatus"`
	PrepareDurationMs int64  `json:"prepareDurationMs"`
	GitCommit         string `gorm:"size:64" json:"gitCommit"`
	GitCommitShort    string `gorm:"size:16" json:"gitCommitShort"`
	GitBranchResolved string `gorm:"size:128" json:"gitBranchResolved"`

	StartedAt  *time.Time `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt"`
	DurationMs int64      `json:"durationMs"`
	ExitCode   int        `json:"exitCode"`
	ErrorMsg   string     `gorm:"size:1024" json:"errorMsg"`

	LogPath string `gorm:"size:512" json:"-"`
	LogSize int64  `json:"logSize"`

	CreatedAt time.Time `json:"createdAt"`
}

// RunStep 是 Run 里一个阶段的执行记录。phase=prepare 时 CommandID 为 0。
type RunStep struct {
	ID             uint       `gorm:"primarykey" json:"id"`
	RunID          uint       `gorm:"index;not null" json:"runId"`
	Phase          string     `gorm:"size:16;not null" json:"phase"`
	CommandID      uint       `json:"commandId"`
	Name           string     `gorm:"size:128" json:"name"`
	SortOrder      int        `json:"sortOrder"`
	Status         string     `gorm:"size:16" json:"status"`
	StartedAt      *time.Time `json:"startedAt"`
	FinishedAt     *time.Time `json:"finishedAt"`
	DurationMs     int64      `json:"durationMs"`
	ExitCode       int        `json:"exitCode"`
	LogOffsetStart int64      `json:"logOffsetStart"`
	LogOffsetEnd   int64      `json:"logOffsetEnd"`
	Script         string     `gorm:"type:text" json:"script"`
}

// AuditLog 记录关键操作。
type AuditLog struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	UserID    uint      `gorm:"index" json:"userId"`
	Username  string    `gorm:"size:64" json:"username"`
	Action    string    `gorm:"size:64" json:"action"`
	Target    string    `gorm:"size:128" json:"target"`
	Detail    string    `gorm:"size:1024" json:"detail"`
	IP        string    `gorm:"size:64" json:"ip"`
	CreatedAt time.Time `json:"createdAt"`
}

// Setting 是 key-value 形式的系统设置覆盖项。
type Setting struct {
	Key       string    `gorm:"primarykey;size:64" json:"key"`
	Value     string    `gorm:"type:text" json:"value"`
	UpdatedAt time.Time `json:"updatedAt"`
}
