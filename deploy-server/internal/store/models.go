package store

import "time"

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

// ProjectEnv 是项目环境变量。IsSecret 的值在出参时永远为空串。
type ProjectEnv struct {
	ID        uint   `gorm:"primarykey" json:"id"`
	ProjectID uint   `gorm:"index;not null" json:"projectId"`
	Key       string `gorm:"size:128;not null" json:"key"`
	Value     string `gorm:"type:text" json:"value"`
	SecretEnc string `gorm:"type:text" json:"-"`
	IsSecret  bool   `json:"isSecret"`
}

// HookToken 是免登录触发部署的令牌。
type HookToken struct {
	ID         uint       `gorm:"primarykey" json:"id"`
	ProjectID  uint       `gorm:"index;not null" json:"projectId"`
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
type Run struct {
	ID           uint   `gorm:"primarykey" json:"id"`
	ProjectID    uint   `gorm:"index;not null" json:"projectId"`
	ProjectKey   string `gorm:"size:64;index" json:"projectKey"`
	ProjectName  string `gorm:"size:128" json:"projectName"`
	BuildNumber  int64  `gorm:"index" json:"buildNumber"`
	Status       string `gorm:"size:16;index;not null" json:"status"`
	TriggerType  string `gorm:"size:16" json:"triggerType"`
	TriggerUser  string `gorm:"size:64" json:"triggerUserName"`
	TriggerUID   uint   `json:"triggerUserId"`
	TriggerIP    string `gorm:"size:64" json:"triggerIp"`
	TriggerUA    string `gorm:"size:256" json:"triggerUa"`
	RequestedRef string `gorm:"size:128" json:"requestedRef"`
	ParamsJSON   string `gorm:"type:text" json:"-"`

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
