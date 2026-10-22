package api

import "time"

// ---------- 通用 ----------

// Page 对应契约 0.4 节的分页响应 data。
type Page[T any] struct {
	List     []T `json:"list"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
}

// ---------- 认证 ----------

// User 对象（契约 1 节）。
type User struct {
	ID                 int64  `json:"id"`
	Username           string `json:"username"`
	Nickname           string `json:"nickname"`
	Email              string `json:"email"`
	Role               string `json:"role"`
	Status             string `json:"status"`
	MustChangePassword bool   `json:"mustChangePassword"`
	LastLoginAt        string `json:"lastLoginAt"`
	CreatedAt          string `json:"createdAt"`
}

// LoginRequest 是 POST /auth/login 的请求体。
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResult 是 POST /auth/login 的 data。
type LoginResult struct {
	Token              string `json:"token"`
	ExpiresAt          int64  `json:"expiresAt"`
	MustChangePassword bool   `json:"mustChangePassword"`
	User               User   `json:"user"`
}

// ---------- 项目 ----------

// Command 对象（契约 4.2）。
type Command struct {
	ID              int64  `json:"id"`
	ProjectID       int64  `json:"projectId"`
	Name            string `json:"name"`
	Script          string `json:"script"`
	SortOrder       int    `json:"sortOrder"`
	Enabled         bool   `json:"enabled"`
	FailStrategy    string `json:"failStrategy"`
	WorkdirOverride string `json:"workdirOverride"`
	TimeoutSec      int    `json:"timeoutSec"`
	ShellOverride   string `json:"shellOverride"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

// EnvVar 对象（契约 4.3）。
type EnvVar struct {
	ID       int64  `json:"id"`
	Key      string `json:"key"`
	Value    string `json:"value"`
	IsSecret bool   `json:"isSecret"`
}

// Project 对象（契约 4.1）。列表接口不返回 commands / envs。
type Project struct {
	Kind             string    `json:"kind"`
	Dependencies     []string  `json:"dependencies"`
	ID               int64     `json:"id"`
	Key              string    `json:"key"`
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	Workdir          string    `json:"workdir"`
	Shell            string    `json:"shell"`
	TimeoutSec       int       `json:"timeoutSec"`
	Concurrency      string    `json:"concurrency"`
	HookEnabled      bool      `json:"hookEnabled"`
	OwnerID          int64     `json:"ownerId"`
	OwnerName        string    `json:"ownerName"`
	Archived         bool      `json:"archived"`
	UseGit           bool      `json:"useGit"`
	GitURL           string    `json:"gitUrl"`
	GitBranch        string    `json:"gitBranch"`
	GitCredentialID  *int64    `json:"gitCredentialId"`
	GitSubdir        string    `json:"gitSubdir"`
	GitDepth         int       `json:"gitDepth"`
	GitClean         bool      `json:"gitClean"`
	GitSubmodule     bool      `json:"gitSubmodule"`
	AllowEnvOverride bool      `json:"allowEnvOverride"`
	LastRunAt        string    `json:"lastRunAt"`
	LastRunStatus    string    `json:"lastRunStatus"`
	RunCount         int       `json:"runCount"`
	CanEdit          bool      `json:"canEdit"`
	Commands         []Command `json:"commands"`
	Envs             []EnvVar  `json:"envs"`
	CreatedAt        string    `json:"createdAt"`
	UpdatedAt        string    `json:"updatedAt"`
}

// CommandInput 是内联提交的 Command（无 id 即新建）。
type CommandInput struct {
	ID              int64  `json:"id,omitempty"`
	Name            string `json:"name"`
	Script          string `json:"script"`
	SortOrder       int    `json:"sortOrder"`
	Enabled         bool   `json:"enabled"`
	FailStrategy    string `json:"failStrategy,omitempty"`
	WorkdirOverride string `json:"workdirOverride,omitempty"`
	TimeoutSec      int    `json:"timeoutSec,omitempty"`
	ShellOverride   string `json:"shellOverride,omitempty"`
}

// EnvVarInput 是内联提交的 EnvVar。
type EnvVarInput struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	IsSecret bool   `json:"isSecret,omitempty"`
}

// ProjectInput 对应契约 4.4 的 ProjectInput（POST/PUT 内联 commands + envs）。
type ProjectInput struct {
	Kind             string         `json:"kind"`
	Dependencies     []string       `json:"dependencies"`
	Key              string         `json:"key"`
	Name             string         `json:"name"`
	Description      string         `json:"description"`
	Workdir          string         `json:"workdir"`
	Shell            string         `json:"shell"`
	TimeoutSec       int            `json:"timeoutSec"`
	Concurrency      string         `json:"concurrency"`
	HookEnabled      bool           `json:"hookEnabled"`
	Archived         bool           `json:"archived"`
	UseGit           bool           `json:"useGit"`
	GitURL           string         `json:"gitUrl"`
	GitBranch        string         `json:"gitBranch"`
	GitCredentialID  *int64         `json:"gitCredentialId"`
	GitSubdir        string         `json:"gitSubdir"`
	GitDepth         int            `json:"gitDepth"`
	GitClean         bool           `json:"gitClean"`
	GitSubmodule     bool           `json:"gitSubmodule"`
	AllowEnvOverride bool           `json:"allowEnvOverride"`
	Commands         []CommandInput `json:"commands"`
	Envs             []EnvVarInput  `json:"envs"`
}

// ---------- 运行 ----------

// Run 对象（契约 7.1）。
type Run struct {
	ID                int64             `json:"id"`
	ProjectID         int64             `json:"projectId"`
	ProjectKey        string            `json:"projectKey"`
	ProjectName       string            `json:"projectName"`
	BuildNumber       int               `json:"buildNumber"`
	Status            string            `json:"status"`
	TriggerType       string            `json:"triggerType"`
	TriggerUserID     int64             `json:"triggerUserId"`
	TriggerUserName   string            `json:"triggerUserName"`
	TriggerIP         string            `json:"triggerIp"`
	TriggerUA         string            `json:"triggerUa"`
	RequestedRef      string            `json:"requestedRef"`
	Params            map[string]string `json:"params"`
	PrepareStatus     string            `json:"prepareStatus"`
	PrepareDurationMs int64             `json:"prepareDurationMs"`
	GitCommit         string            `json:"gitCommit"`
	GitCommitShort    string            `json:"gitCommitShort"`
	GitBranchResolved string            `json:"gitBranchResolved"`
	GitMessage        string            `json:"gitMessage"`
	GitAuthor         string            `json:"gitAuthor"`
	GitCommitAt       string            `json:"gitCommitAt"`
	StartedAt         string            `json:"startedAt"`
	FinishedAt        string            `json:"finishedAt"`
	DurationMs        int64             `json:"durationMs"`
	ExitCode          *int              `json:"exitCode"`
	ErrorMsg          string            `json:"errorMsg"`
	LogSize           int64             `json:"logSize"`
	Steps             []RunStep         `json:"steps"`
}

// RunStep 对象（契约 7.2）。
type RunStep struct {
	ID             int64  `json:"id"`
	RunID          int64  `json:"runId"`
	Phase          string `json:"phase"`
	CommandID      int64  `json:"commandId"`
	Name           string `json:"name"`
	SortOrder      int    `json:"sortOrder"`
	Status         string `json:"status"`
	StartedAt      string `json:"startedAt"`
	FinishedAt     string `json:"finishedAt"`
	DurationMs     int64  `json:"durationMs"`
	ExitCode       *int   `json:"exitCode"`
	LogOffsetStart int64  `json:"logOffsetStart"`
	LogOffsetEnd   int64  `json:"logOffsetEnd"`
}

// RunRequest 是 POST /projects/{key}/run 的请求体。
type RunRequest struct {
	WithDependencies bool              `json:"withDependencies,omitempty"`
	Branch           string            `json:"branch,omitempty"`
	Env              map[string]string `json:"env,omitempty"`
	CommandIDs       []int64           `json:"commandIds,omitempty"`
	CommandID        int64             `json:"commandId,omitempty"`
}

// RunTriggerResult 是触发运行返回的 data。
type RunTriggerResult struct {
	RunID       int64  `json:"runId"`
	ProjectKey  string `json:"projectKey"`
	BuildNumber int    `json:"buildNumber"`
	Status      string `json:"status"`
	ExitCode    *int   `json:"exitCode"`
	DurationMs  int64  `json:"durationMs"`
	Async       bool   `json:"async"`
	StatusURL   string `json:"statusUrl"`
	LogURL      string `json:"logUrl"`
}

// LogChunk 是 GET /runs/{id}/log 的 data（契约 7.3）。
type LogChunk struct {
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"nextOffset"`
	EOF        bool   `json:"eof"`
	Data       string `json:"data"`
}

// 运行状态常量。
const (
	StatusQueued   = "queued"
	StatusRunning  = "running"
	StatusSuccess  = "success"
	StatusFailed   = "failed"
	StatusCanceled = "canceled"
	StatusTimeout  = "timeout"
	StatusError    = "error"
)

// IsTerminalStatus 报告运行状态是否已结束。
func IsTerminalStatus(s string) bool {
	switch s {
	case StatusSuccess, StatusFailed, StatusCanceled, StatusTimeout, StatusError:
		return true
	default:
		return false
	}
}

// IsSuccessStatus 报告运行是否成功。
func IsSuccessStatus(s string) bool { return s == StatusSuccess }

// ---------- 系统 ----------

// DoctorCheck 是 system/doctor 的单条自检项。
type DoctorCheck struct {
	Name    string `json:"name"`
	Found   bool   `json:"found"`
	Version string `json:"version"`
	Path    string `json:"path"`
	Hint    string `json:"hint"`
}

// DoctorShell 描述选择的 shell。
type DoctorShell struct {
	Selected  string   `json:"selected"`
	Path      string   `json:"path"`
	Available []string `json:"available"`
}

// Doctor 是 GET /system/doctor 的 data。
type Doctor struct {
	OS              string        `json:"os"`
	Arch            string        `json:"arch"`
	Hostname        string        `json:"hostname"`
	GoVersion       string        `json:"goVersion"`
	ServerVersion   string        `json:"serverVersion"`
	UptimeSec       int64         `json:"uptimeSec"`
	WebRoot         string        `json:"webRoot"`
	WebRootWritable bool          `json:"webRootWritable"`
	DataDir         string        `json:"dataDir"`
	DataDirWritable bool          `json:"dataDirWritable"`
	Shell           DoctorShell   `json:"shell"`
	Checks          []DoctorCheck `json:"checks"`
}

// Health 是 GET /healthz 的 data。
type Health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Time    string `json:"time"`
}

// ---------- 中间件（GET/POST /system/middleware*） ----------

// MiddlewareField 是中间件的一个可编辑参数。
//
// Secret 为 true 时 Value 恒为空串 —— 服务端绝不回密码明文，
// 只用 Set 表示「已经配过密码了」。
type MiddlewareField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Default     string `json:"default"`
	Placeholder string `json:"placeholder"`
	Help        string `json:"help"`
	Secret      bool   `json:"secret"`
	Value       string `json:"value"`
	Set         bool   `json:"set"`
}

// MiddlewareProbe 是页面上展示的探测项。
type MiddlewareProbe struct {
	Name  string `json:"name"`
	Found bool   `json:"found"`
}

// MiddlewareComponent 是一种中间件的当前状态。
type MiddlewareComponent struct {
	Key             string            `json:"key"`
	Name            string            `json:"name"`
	Note            string            `json:"note"`
	Category        string            `json:"category"`
	Estimate        string            `json:"estimate"`
	Installed       bool              `json:"installed"`
	Running         bool              `json:"running"`
	Version         string            `json:"version"`
	Path            string            `json:"path"`
	Detail          string            `json:"detail"`
	Modes           []string          `json:"modes"`
	DefaultMode     string            `json:"defaultMode"`
	Mode            string            `json:"mode"`
	EffectiveMode   string            `json:"effectiveMode"`
	NativeSupported bool              `json:"nativeSupported"`
	Probes          []MiddlewareProbe `json:"probes"`
	Fields          []MiddlewareField `json:"fields"`
}

// MiddlewareList 是 GET /system/middleware 的 data。
type MiddlewareList struct {
	Supported       bool                  `json:"supported"`
	Platform        string                `json:"platform"`
	DockerAvailable bool                  `json:"dockerAvailable"`
	DockerVersion   string                `json:"dockerVersion"`
	Components      []MiddlewareComponent `json:"components"`
	Missing         []string              `json:"missing"`
	Running         []string              `json:"running"`
	Actions         []string              `json:"actions"`
	ScriptPath      string                `json:"scriptPath"`
	ScriptURL       string                `json:"scriptUrl"`
	ProjectKey      string                `json:"projectKey"`
	Hint            string                `json:"hint"`
	InstallCmd      string                `json:"installCmd"`
}

// MiddlewareInstallRequest 是 POST /system/middleware/install 的请求体。
type MiddlewareInstallRequest struct {
	Components []string `json:"components,omitempty"`
	Mode       string   `json:"mode,omitempty"`
	Action     string   `json:"action,omitempty"`
	DryRun     bool     `json:"dryRun,omitempty"`
	Force      bool     `json:"force,omitempty"`
	Mirror     string   `json:"mirror,omitempty"`
}

// MiddlewareInstallResult 是安装/启停操作的返回 data。
type MiddlewareInstallResult struct {
	RunID       int64    `json:"runId"`
	BuildNumber int      `json:"buildNumber"`
	Status      string   `json:"status"`
	ProjectKey  string   `json:"projectKey"`
	Components  []string `json:"components"`
	Action      string   `json:"action"`
	DryRun      bool     `json:"dryRun"`
	Command     string   `json:"command"`
	LogURL      string   `json:"logUrl"`
	NothingToDo bool     `json:"nothingToDo"`
	Message     string   `json:"message"`
}

// FormatDuration 把毫秒格式化成人类可读的耗时。
func FormatDuration(ms int64) string {
	if ms <= 0 {
		return "-"
	}
	d := time.Duration(ms) * time.Millisecond
	switch {
	case d < time.Second:
		return d.Round(time.Millisecond).String()
	case d < time.Minute:
		return d.Round(100 * time.Millisecond).String()
	default:
		return d.Round(time.Second).String()
	}
}
