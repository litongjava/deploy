// Package runner 是平台的执行引擎：把「一条 Run」变成一串真实进程，
// 并负责超时、取消、进程树清理、日志采集。
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"deploy-server/internal/config"
	"deploy-server/internal/runlog"
	"deploy-server/internal/secret"
	"deploy-server/internal/store"
)

// Executor 持有并发闸门、运行中句柄与项目锁。
type Executor struct {
	cfg    *config.Config
	st     *store.Store
	box    *secret.Box
	broker *runlog.Broker

	sem *gate

	mu      sync.Mutex
	projMu  map[uint]*sync.Mutex
	taskMu  map[uint]*sync.Mutex
	running map[uint]*handle
	// cmdFilter 记录「本次触发只想跑哪几条命令」。
	// 放在内存里而不是库里：它只对本次调度有效，服务重启后 Run 会被标记为中断。
	cmdFilter map[uint][]uint

	// settingsInt 让执行器读到运行时可变的设置（日志上限等），
	// 由 main 注入，避免 runner 反向依赖 api 层。
	settingsInt func(key string, def int) int

	wg sync.WaitGroup
}

// SetSettingsIntReader 注入运行时设置读取函数。
func (e *Executor) SetSettingsIntReader(fn func(key string, def int) int) { e.settingsInt = fn }

// logMaxBytes 取生效的单次运行日志上限。
func (e *Executor) logMaxBytes() int64 {
	if e.settingsInt != nil {
		if v := e.settingsInt("logMaxBytes", int(e.cfg.LogMaxBytes)); v > 0 {
			return int64(v)
		}
	}
	return e.cfg.LogMaxBytes
}

// SubmitRequest 描述一次触发。
//
// ProjectID 与 TaskID 二选一：填 TaskID 表示这是一次「任务执行」，
// 走的是同一套执行引擎，但没有 Git 准备阶段、没有依赖流水线。
type SubmitRequest struct {
	ProjectID   uint
	TaskID      uint
	Ref         string
	Params      map[string]string
	CommandIDs  []uint
	TriggerType string
	TriggerUser string
	TriggerUID  uint
	TriggerIP   string
	TriggerUA   string
}

// jobSpec 是执行引擎眼里的「一次可执行的作业」。
//
// 项目（构建 + 部署）与任务（跑一件事）在存储上是两张表，但执行语义完全一样：
// 准备 → 按顺序跑脚本 → 收退出码。这里把它们归一，执行引擎只认 jobSpec。
type jobSpec struct {
	Kind   string // store.TargetProject | store.TargetTask
	ID     uint
	Key    string
	Name   string
	Owner  uint
	Detail string // 项目=项目类型；任务=留空

	Workdir      string
	Shell        string
	TimeoutSec   int
	Concurrency  string
	AllowEnvOver bool
	Archived     bool

	UseGit          bool
	GitURL          string
	GitBranch       string
	GitCredentialID *uint
	GitSubdir       string
	GitDepth        int
	GitClean        bool
	GitSubmodule    bool

	Steps []store.TaskStep // 任务专用；项目走 project_commands 表
}

// jobStep 是 jobSpec 归一后的一步脚本。
//
// ID 对项目是 project_commands.id，对任务是 1 基的序号 —— 它只用于
// RunStep.CommandID 与「只跑某一步」的筛选，不需要全局唯一。
type jobStep struct {
	ID              uint
	Name            string
	Script          string
	SortOrder       int
	Enabled         bool
	FailStrategy    string
	WorkdirOverride string
	TimeoutSec      int
	ShellOverride   string
}

// isTask 判断这次运行的目标是不是任务。
func (j *jobSpec) isTask() bool { return j.Kind == store.TargetTask }

// logKey 是日志目录里这一级子目录的名字。
//
// 任务单独放在 tasks/ 下面：项目的 key 与任务的 key 是两个独立的命名空间，
// 不加前缀的话同名项目与任务会往同一个目录里写日志。
func (j *jobSpec) logKey() string {
	if j.isTask() {
		return filepath.Join("tasks", j.Key)
	}
	return j.Key
}

type handle struct {
	mu       sync.Mutex
	cancel   context.CancelFunc
	cmd      *exec.Cmd
	byUser   bool
	finished bool
}

func (h *handle) setCmd(c *exec.Cmd) {
	h.mu.Lock()
	h.cmd = c
	h.mu.Unlock()
}

func (h *handle) clearCmd() {
	h.mu.Lock()
	h.cmd = nil
	h.mu.Unlock()
}

// kill 取消上下文并杀掉当前进程树。先 cancel 再 killTree：
// cancel 让 runCommand 的 select 走 ctx.Done() 分支，killTree 保证子进程不残留。
func (h *handle) kill() {
	h.mu.Lock()
	h.byUser = true
	c := h.cmd
	h.mu.Unlock()
	if h.cancel != nil {
		h.cancel()
	}
	if c != nil {
		killTree(c)
	}
}

func (h *handle) canceledByUser() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.byUser
}

// New 构造执行引擎。
func New(cfg *config.Config, st *store.Store, box *secret.Box) *Executor {
	return &Executor{
		cfg:       cfg,
		st:        st,
		box:       box,
		broker:    runlog.NewBroker(),
		sem:       newGate(cfg.MaxConcurrentRuns),
		projMu:    make(map[uint]*sync.Mutex),
		taskMu:    make(map[uint]*sync.Mutex),
		running:   make(map[uint]*handle),
		cmdFilter: make(map[uint][]uint),
	}
}

// SetMaxConcurrent 动态调整全局并发上限（系统设置里改，立即生效）。
func (e *Executor) SetMaxConcurrent(n int) { e.sem.setLimit(n) }

// MaxConcurrent 当前的并发上限。
func (e *Executor) MaxConcurrent() int { return e.sem.limitNow() }

// Broker 暴露日志发布订阅中心，给 WebSocket 层用。
func (e *Executor) Broker() *runlog.Broker { return e.broker }

// RunningCount 当前正在执行的 Run 数。
func (e *Executor) RunningCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.running)
}

// RunningIDs 返回正在执行的 Run ID 列表。
func (e *Executor) RunningIDs() []uint {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]uint, 0, len(e.running))
	for id := range e.running {
		out = append(out, id)
	}
	return out
}

// jobLock 返回「同一个执行对象串行」用的锁。
//
// 项目与任务的 ID 各自从 1 开始，会重号，所以必须用两张表分开存锁，
// 否则 ID 相同的项目与任务会被误判成同一个执行对象而互相阻塞。
func (e *Executor) jobLock(kind string, id uint) *sync.Mutex {
	e.mu.Lock()
	defer e.mu.Unlock()
	m := e.projMu
	if kind == store.TargetTask {
		m = e.taskMu
	}
	l, ok := m[id]
	if !ok {
		l = &sync.Mutex{}
		m[id] = l
	}
	return l
}

func (e *Executor) setHandle(id uint, h *handle) {
	e.mu.Lock()
	e.running[id] = h
	e.mu.Unlock()
}

func (e *Executor) clearHandle(id uint) {
	e.mu.Lock()
	delete(e.running, id)
	e.mu.Unlock()
}

// Submit 创建 Run 记录并异步排队执行。
//
// build number 的分配放在事务里，配合 SQLite 单连接，天然不会有竞态。
func (e *Executor) Submit(req SubmitRequest) (*store.Run, error) {
	job, err := e.loadJob(req)
	if err != nil {
		return nil, err
	}
	if job.Archived {
		return nil, ErrProjectArchived
	}
	run, err := e.createRun(job, req, "", "", 0)
	if err != nil {
		return nil, err
	}

	// cancelPrev：先取消同一个目标正在跑的，再排自己。
	if job.Concurrency == store.ConcurrencyCancelPrev {
		e.CancelTarget(job.Kind, job.ID)
	}
	if len(req.CommandIDs) > 0 {
		e.mu.Lock()
		e.cmdFilter[run.ID] = req.CommandIDs
		e.mu.Unlock()
	}

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.execute(run.ID, req)
	}()
	return run, nil
}

// loadJob 按 SubmitRequest 把执行对象读成 jobSpec。
func (e *Executor) loadJob(req SubmitRequest) (*jobSpec, error) {
	if req.TaskID != 0 {
		return e.loadTaskJob(req.TaskID)
	}
	p, err := e.loadProject(req.ProjectID)
	if err != nil {
		return nil, err
	}
	return projectJob(p), nil
}

// loadJobByRun 按 Run 记录把执行对象读成 jobSpec。
func (e *Executor) loadJobByRun(run *store.Run) (*jobSpec, error) {
	if run.TargetKind == store.TargetTask {
		return e.loadTaskJob(run.TaskID)
	}
	p, err := e.loadProject(run.ProjectID)
	if err != nil {
		return nil, err
	}
	return projectJob(p), nil
}

func projectJob(p *store.Project) *jobSpec {
	return &jobSpec{
		Kind: store.TargetProject, ID: p.ID, Key: p.Key, Name: p.Name,
		Owner: p.OwnerID, Detail: p.Kind,
		Workdir: p.Workdir, Shell: p.Shell, TimeoutSec: p.TimeoutSec,
		Concurrency: p.Concurrency, AllowEnvOver: p.AllowEnvOver, Archived: p.Archived,
		UseGit: p.UseGit, GitURL: p.GitURL, GitBranch: p.GitBranch,
		GitCredentialID: p.GitCredentialID, GitSubdir: p.GitSubdir,
		GitDepth: p.GitDepth, GitClean: p.GitClean, GitSubmodule: p.GitSubmodule,
	}
}

func (e *Executor) loadTaskJob(id uint) (*jobSpec, error) {
	var t store.Task
	if err := e.st.DB().First(&t, id).Error; err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}
	return &jobSpec{
		Kind: store.TargetTask, ID: t.ID, Key: t.Key, Name: t.Name,
		Owner: t.OwnerID,
		Workdir: t.Workdir, Shell: t.Shell, TimeoutSec: t.TimeoutSec,
		Concurrency: t.Concurrency, AllowEnvOver: t.AllowEnvOver, Archived: t.Archived,
		Steps: store.DecodeTaskSteps(t.StepsJSON),
	}, nil
}

// stepsOf 取出这次要跑的步骤。
//
// filter 是「只跑这几步」的白名单（命令 ID / 任务步骤序号），空表示全跑。
func (e *Executor) stepsOf(job *jobSpec, filter []uint) []jobStep {
	if job.isTask() {
		out := make([]jobStep, 0, len(job.Steps))
		for i, s := range job.Steps {
			if !s.Enabled {
				continue
			}
			out = append(out, jobStep{
				ID: uint(i + 1), Name: s.Name, Script: s.Script, SortOrder: i + 1,
				Enabled: true, FailStrategy: s.FailStrategy,
				WorkdirOverride: s.WorkdirOverride, TimeoutSec: s.TimeoutSec,
				ShellOverride: s.ShellOverride,
			})
		}
		return filterSteps(out, filter)
	}

	var cmds []store.ProjectCommand
	q := e.st.DB().Where("project_id = ? AND enabled = ?", job.ID, true)
	if len(filter) > 0 {
		q = q.Where("id IN ?", filter)
	}
	q.Order("sort_order asc, id asc").Find(&cmds)

	out := make([]jobStep, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, jobStep{
			ID: c.ID, Name: c.Name, Script: c.Script, SortOrder: c.SortOrder,
			Enabled: c.Enabled, FailStrategy: c.FailStrategy,
			WorkdirOverride: c.WorkdirOver, TimeoutSec: c.TimeoutSec,
			ShellOverride: c.ShellOverride,
		})
	}
	return out
}

// filterSteps 按白名单过滤，保持原顺序。
func filterSteps(steps []jobStep, filter []uint) []jobStep {
	if len(filter) == 0 {
		return steps
	}
	want := make(map[uint]bool, len(filter))
	for _, id := range filter {
		want[id] = true
	}
	out := make([]jobStep, 0, len(steps))
	for _, s := range steps {
		if want[s.ID] {
			out = append(out, s)
		}
	}
	return out
}

// loadJobEnvs 读出注入用的环境变量（项目查表 / 任务解 JSON）。
func (e *Executor) loadJobEnvs(job *jobSpec) []EnvVar {
	if job.isTask() {
		var t store.Task
		if err := e.st.DB().Select("envs_json").First(&t, job.ID).Error; err != nil {
			return nil
		}
		return EnvVarsFromTask(store.DecodeTaskEnvs(t.EnvsJSON))
	}
	var envs []store.ProjectEnv
	e.st.DB().Where("project_id = ?", job.ID).Order("id asc").Find(&envs)
	return EnvVarsFromProject(envs)
}

// createRun 分配构建号并落库，但不启动执行。
//
// 拆出来是给依赖流水线用的：流水线要先把「依赖 + 目标」的 Run 记录一次建好
// （这样界面上立刻能看到整条构建计划），再按顺序逐个执行。
func (e *Executor) createRun(job *jobSpec, req SubmitRequest, pipelineID, role string, order int) (*store.Run, error) {
	run := store.Run{
		ProjectID:    job.ID,
		ProjectKey:   job.Key,
		ProjectName:  job.Name,
		TaskID:       0,
		TargetKind:   store.TargetProject,
		Status:       store.RunQueued,
		TriggerType:  req.TriggerType,
		TriggerUser:  req.TriggerUser,
		TriggerUID:   req.TriggerUID,
		TriggerIP:    req.TriggerIP,
		TriggerUA:    truncate(req.TriggerUA, 250),
		RequestedRef: req.Ref,
		ParamsJSON:   encodeParams(req.Params),

		PipelineID:    pipelineID,
		PipelineRole:  role,
		PipelineOrder: order,
	}
	if job.isTask() {
		// 任务没有「项目」：ProjectID 留 0，展示用的快照字段放任务的 key/名称。
		run.ProjectID = 0
		run.TaskID = job.ID
		run.TargetKind = store.TargetTask
	}

	if err := e.st.DB().Transaction(func(tx *gorm.DB) error {
		var maxNo int64
		q := tx.Model(&store.Run{})
		if job.isTask() {
			q = q.Where("task_id = ?", job.ID)
		} else {
			q = q.Where("project_id = ?", job.ID)
		}
		row := q.Select("COALESCE(MAX(build_number), 0)").Row()
		if err := row.Scan(&maxNo); err != nil {
			return err
		}
		run.BuildNumber = maxNo + 1
		return tx.Create(&run).Error
	}); err != nil {
		return nil, fmt.Errorf("failed to create run record: %w", err)
	}
	return &run, nil
}

// ErrProjectArchived 表示项目已归档，不可执行。
var ErrProjectArchived = errors.New("the project is archived and cannot run")

// Wait 等待所有正在执行/排队的 Run 结束（优雅关闭用）。
func (e *Executor) Wait(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() { e.wg.Wait(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// Cancel 取消一个 Run。返回 false 表示它已经结束或不存在。
//
// 属于依赖流水线的 Run 会连带取消同一条流水线里还在跑的其它 Run：
// 否则取消主项目后，为它先构建的依赖还会继续跑下去。
func (e *Executor) Cancel(runID uint) bool {
	var run store.Run
	if err := e.st.DB().Select("id", "pipeline_id").First(&run, runID).Error; err == nil && run.PipelineID != "" {
		var ids []uint
		e.st.DB().Model(&store.Run{}).
			Where("pipeline_id = ? AND id <> ? AND status IN ?",
				run.PipelineID, runID, []string{store.RunRunning, store.RunQueued}).
			Pluck("id", &ids)
		for _, id := range ids {
			e.cancelOne(id)
		}
	}
	return e.cancelOne(runID)
}

// cancelOne 只取消这一个 Run 的进程，不碰流水线里的其它 Run。
func (e *Executor) cancelOne(runID uint) bool {
	e.mu.Lock()
	h := e.running[runID]
	e.mu.Unlock()
	if h == nil {
		return false
	}
	h.kill()
	return true
}

// CancelProject 取消某个项目下所有正在执行的 Run。
func (e *Executor) CancelProject(projectID uint) {
	e.cancelTargetRuns("project_id = ?", projectID)
}

// CancelTask 取消某个任务下所有正在执行的 Run。
func (e *Executor) CancelTask(taskID uint) {
	e.cancelTargetRuns("task_id = ?", taskID)
}

// CancelTarget 按执行对象类型取消。
func (e *Executor) CancelTarget(kind string, id uint) {
	if kind == store.TargetTask {
		e.CancelTask(id)
		return
	}
	e.CancelProject(id)
}

func (e *Executor) cancelTargetRuns(where string, id uint) {
	var ids []uint
	e.st.DB().Model(&store.Run{}).
		Where(where+" AND status IN ?", id, []string{store.RunRunning, store.RunQueued}).
		Pluck("id", &ids)
	for _, id := range ids {
		e.Cancel(id)
	}
}

// RecoverInterrupted 在服务启动时把「上一进程留下的 running/queued」标记为中断。
//
// 这些 Run 的进程随旧进程一起消失了，继续显示 running 会误导用户。
func (e *Executor) RecoverInterrupted() (int64, error) {
	now := time.Now()
	res := e.st.DB().Model(&store.Run{}).
		Where("status IN ?", []string{store.RunRunning, store.RunQueued}).
		Updates(map[string]any{
			"status":      store.RunError,
			"error_msg":   "interrupted because the server restarted",
			"finished_at": now,
		})
	if res.Error != nil {
		return 0, res.Error
	}
	e.st.DB().Model(&store.RunStep{}).
		Where("status = ?", store.StepRunning).
		Updates(map[string]any{"status": store.RunError, "finished_at": now})
	return res.RowsAffected, nil
}

func (e *Executor) loadProject(id uint) (*store.Project, error) {
	var p store.Project
	if err := e.st.DB().First(&p, id).Error; err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}
	return &p, nil
}

// execOutcome 汇总一次 Run 的结果。
type execOutcome struct {
	status   string
	exitCode int
	errMsg   string
	timedOut bool
}

func (e *Executor) execute(runID uint, req SubmitRequest) {
	// 全局并发闸门：超过上限的 Run 在这里排队，
	// 状态保持 queued，符合契约里的状态机。
	e.sem.acquire()
	defer e.sem.release()

	var run store.Run
	if err := e.st.DB().First(&run, runID).Error; err != nil {
		return
	}
	job, err := e.loadJobByRun(&run)
	if err != nil {
		e.finish(runID, execOutcome{status: store.RunError, errMsg: err.Error()}, nil)
		return
	}

	// 同一执行对象串行：把 Prepare 与命令放在同一把锁里，
	// 否则两个 Run 会同时 checkout 同一个工作目录（需求 FR-GIT-9）。
	if job.Concurrency == "" || job.Concurrency == store.ConcurrencyQueue {
		l := e.jobLock(job.Kind, job.ID)
		l.Lock()
		defer l.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := &handle{cancel: cancel}
	e.setHandle(runID, h)
	defer e.clearHandle(runID)

	// 打码清单：先把 Secret 明文解出来，日志从第一行起就是干净的。
	envs := e.loadJobEnvs(job)
	secretValues := SecretValues(envs, e.box)

	w, err := runlog.NewWriter(e.cfg.LogDir(), job.logKey(), runID, e.broker, secretValues, e.logMaxBytes())
	if err != nil {
		e.finish(runID, execOutcome{status: store.RunError, errMsg: err.Error()}, nil)
		return
	}
	// 安全网：任何提前 return 都要把日志文件关掉，但正常路径下
	// 下面会显式关闭（Close 幂等），保证「日志写完」早于「状态落库」。
	defer func() {
		size := w.Close()
		e.st.DB().Model(&store.Run{}).Where("id = ?", runID).Update("log_size", size)
		e.broker.CloseRun(runID)
	}()

	var steps []store.RunStep
	outcome := e.runPhases(ctx, h, run, job, envs, w, &steps)

	e.mu.Lock()
	delete(e.cmdFilter, runID)
	e.mu.Unlock()

	// 顺序很重要：先把日志冲刷并关闭订阅，再写最终状态。
	// 反过来的话，订阅者可能在最后几个字节落盘前就看到「已结束」而提前收尾。
	size := w.Close()
	e.st.DB().Model(&store.Run{}).Where("id = ?", runID).Update("log_size", size)
	e.finish(runID, outcome, steps)
	e.broker.CloseRun(runID)
}

func (e *Executor) runPhases(ctx context.Context, h *handle, run store.Run, job *jobSpec,
	envs []EnvVar, w *runlog.Writer, steps *[]store.RunStep) execOutcome {

	var outcome execOutcome
	outcome.errMsg = ""
	started := time.Now()

	// 标记 running
	e.st.DB().Model(&store.Run{}).Where("id = ?", run.ID).Updates(map[string]any{
		"status":     store.RunRunning,
		"started_at": started,
	})

	workdir := ExpandWorkdir(job.Workdir, e.cfg.WebRoot, job.Key, job.Name)
	actualWorkdir := workdir

	if job.isTask() {
		fmt.Fprintf(w, "=== Task Run #%d ===\n", run.BuildNumber)
		fmt.Fprintf(w, "Task    : %s (%s)\n", job.Name, job.Key)
	} else {
		fmt.Fprintf(w, "=== Deploy Run #%d ===\n", run.BuildNumber)
		fmt.Fprintf(w, "Project : %s (%s)\n", job.Name, job.Key)
	}
	fmt.Fprintf(w, "Workdir : %s\n", workdir)
	fmt.Fprintf(w, "Trigger : %s by %s", run.TriggerType, run.TriggerUser)
	if run.TriggerIP != "" {
		fmt.Fprintf(w, " from %s", run.TriggerIP)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Started : %s\n", started.Format(time.RFC3339))
	if job.UseGit {
		fmt.Fprintf(w, "Git     : %s @ %s\n", job.GitURL, firstNonEmpty(run.RequestedRef, job.GitBranch))
	}
	fmt.Fprintln(w, "----------------------------------------")

	gitVars := map[string]string{}

	// ---------- Phase 1: Prepare ----------
	// 只有项目有 Git 准备阶段；任务直接跳过（prepare_status 记为 skipped）。
	if job.UseGit {
		prepareStart := time.Now()
		step := store.RunStep{
			RunID: run.ID, Phase: store.PhasePrepare, Name: "Git prepare",
			SortOrder: 0, Status: store.StepRunning, StartedAt: &prepareStart,
			Script: fmt.Sprintf("git clone/fetch %s @ %s", job.GitURL, firstNonEmpty(run.RequestedRef, job.GitBranch)),
		}
		e.st.DB().Create(&step)
		*steps = append(*steps, step)

		fmt.Fprintf(w, "\n=== [prepare] Git prepare ===\n")
		pctx, pcancel := context.WithTimeout(ctx, time.Duration(e.cfg.GitPrepareTimeoutS)*time.Second)
		info, geSecrets, gerr := e.prepareGit(pctx, job, run.RequestedRef, workdir, w)
		pcancel()

		// 凭据是在 prepare 里才解出来的，拿到后立刻加入打码清单，
		// 保证 clone 输出里的 token 不会先漏出去（日志文件是增量落盘的）。
		if len(geSecrets) > 0 {
			w.AddSecrets(geSecrets)
		}

		finishStep(e.st, &step, gerr, 0, 0)
		if gerr != nil {
			fmt.Fprintf(w, "\n*** Git prepare failed, aborted without running any user command ***\n%v\n", gerr)
			outcome.status = store.RunFailed
			outcome.errMsg = gerr.Error()
			e.st.DB().Model(&store.Run{}).Where("id = ?", run.ID).Update("prepare_status", store.RunFailed)
			return outcome
		}

		gitVars["GIT_COMMIT"] = info.Commit
		gitVars["GIT_COMMIT_SHORT"] = info.CommitShort
		gitVars["GIT_BRANCH"] = info.Branch
		gitVars["GIT_TAG"] = info.Tag
		gitVars["GIT_AUTHOR"] = info.Author
		gitVars["GIT_MESSAGE"] = info.Message
		gitVars["GIT_DIRTY"] = info.Dirty

		if job.GitSubdir != "" {
			actualWorkdir = filepath.Join(workdir, job.GitSubdir)
		}
		dur := time.Since(prepareStart).Milliseconds()
		fmt.Fprintf(w, "\n--- prepare done: %s (%s), took %dms ---\n",
			info.CommitShort, info.Branch, dur)
		e.st.DB().Model(&store.Run{}).Where("id = ?", run.ID).Updates(map[string]any{
			"prepare_status":      store.RunSuccess,
			"prepare_duration_ms": dur,
			"git_commit":          info.Commit,
			"git_commit_short":    info.CommitShort,
			"git_branch_resolved": info.Branch,
		})
	} else {
		e.st.DB().Model(&store.Run{}).Where("id = ?", run.ID).Update("prepare_status", store.PrepareSkipped)
	}

	// 工作目录不存在则创建：否则用户第一条命令就会莫名其妙地失败。
	if err := os.MkdirAll(actualWorkdir, 0o750); err != nil {
		fmt.Fprintf(w, "\n*** failed to create workdir: %v ***\n", err)
		outcome.status = store.RunError
		outcome.errMsg = "failed to create workdir: " + err.Error()
		return outcome
	}

	// ---------- Phase 2: Commands ----------
	e.mu.Lock()
	filter := e.cmdFilter[run.ID]
	e.mu.Unlock()

	cmds := e.stepsOf(job, filter)
	if len(cmds) == 0 {
		if job.isTask() {
			fmt.Fprintln(w, "\n*** this task has no enabled steps; nothing to do ***")
		} else {
			fmt.Fprintln(w, "\n*** this project has no enabled commands; only the Git prepare phase ran ***")
		}
	}

	builtins := map[string]string{
		"WORKDIR":           actualWorkdir,
		"RUN_ID":            fmt.Sprint(run.ID),
		"BUILD_NUMBER":      fmt.Sprint(run.BuildNumber),
		"TRIGGER_BY":        run.TriggerUser,
		"TRIGGER_TYPE":      run.TriggerType,
		"START_TIME":        started.Format(time.RFC3339),
		"DEPLOY_SERVER_URL": e.cfg.ServerURL,
	}
	if job.isTask() {
		// 任务只注入 TASK_*，不注入 PROJECT_*：名字对不上会让人以为跑错了对象。
		// 工作目录占位符同时接受 ${PROJECT_KEY} 与 ${TASK_KEY}（见 ExpandWorkdir），
		// 那是为了脚本可复用，与这里的变量命名是两回事。
		builtins["TASK_KEY"] = job.Key
		builtins["TASK_NAME"] = job.Name
	} else {
		builtins["PROJECT_KEY"] = job.Key
		builtins["PROJECT_NAME"] = job.Name
	}
	for k, v := range gitVars {
		builtins[k] = v
	}

	env, _, err := BuildEnv(EnvPlan{
		EnvVars:     envs,
		Builtins:    builtins,
		Params:      decodeParams(run.ParamsJSON),
		AllowParams: job.AllowEnvOver,
		Box:         e.box,
	})
	if err != nil {
		fmt.Fprintf(w, "\n*** failed to resolve environment variables: %v ***\n", err)
		outcome.status = store.RunError
		outcome.errMsg = err.Error()
		return outcome
	}

	projectTimeout := time.Duration(job.TimeoutSec) * time.Second
	deadline := time.Time{}
	if projectTimeout > 0 {
		deadline = started.Add(projectTimeout)
	}

	total := len(cmds)
	for i, c := range cmds {
		if ctx.Err() != nil {
			outcome.status = store.RunCanceled
			outcome.errMsg = "execution cancelled"
			return outcome
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			fmt.Fprintf(w, "\n*** timeout (%ds) reached, aborting ***\n", job.TimeoutSec)
			outcome.status = store.RunTimeout
			outcome.errMsg = fmt.Sprintf("%s timed out after %ds", jobLabel(job), job.TimeoutSec)
			return outcome
		}

		fmt.Fprintf(w, "\n=== [%d/%d] %s ===\n", i+1, total, firstNonEmpty(c.Name, fmt.Sprintf("command %d", i+1)))
		fmt.Fprintf(w, "$ %s\n", strings.TrimRight(c.Script, "\n"))

		stepStart := time.Now()
		offStart := w.Size()
		step := store.RunStep{
			RunID: run.ID, Phase: store.PhaseCommand, CommandID: c.ID,
			Name:      firstNonEmpty(c.Name, fmt.Sprintf("command %d", i+1)),
			SortOrder: i + 1, Status: store.StepRunning, StartedAt: &stepStart,
			LogOffsetStart: offStart, Script: c.Script,
		}
		e.st.DB().Create(&step)

		timeout := time.Duration(c.TimeoutSec) * time.Second
		if timeout == 0 {
			timeout = projectTimeout
			if !deadline.IsZero() {
				timeout = time.Until(deadline)
			}
		}
		if timeout < 0 {
			timeout = 0
		}

		// 每步可以有自己的工作目录；任务的步骤常常要 cd 到别处。
		stepWorkdir := actualWorkdir
		if c.WorkdirOverride != "" {
			stepWorkdir = ExpandWorkdir(c.WorkdirOverride, e.cfg.WebRoot, job.Key, job.Name)
			if err := os.MkdirAll(stepWorkdir, 0o750); err != nil {
				fmt.Fprintf(w, "\n*** failed to create workdir override: %v ***\n", err)
				outcome.status = store.RunError
				outcome.errMsg = "failed to create workdir override: " + err.Error()
				return outcome
			}
		}

		code, runErr := e.runScript(ctx, c.Script, stepWorkdir, c.ShellOverride, job.Shell, env, timeout, w, h)

		finishStep(e.st, &step, runErr, code, w.Size())
		*steps = append(*steps, step)

		switch {
		case runErr != nil && errors.Is(runErr, context.Canceled):
			fmt.Fprintf(w, "\n*** execution cancelled ***\n")
			outcome.status = store.RunCanceled
			outcome.errMsg = "execution cancelled"
			return outcome
		case runErr != nil && errors.Is(runErr, context.DeadlineExceeded):
			fmt.Fprintf(w, "\n*** step timed out ***\n")
			outcome.status = store.RunTimeout
			outcome.errMsg = fmt.Sprintf("step '%s' timed out", step.Name)
			outcome.exitCode = code
			return outcome
		case runErr != nil:
			fmt.Fprintf(w, "\n*** failed to start the command: %v ***\n", runErr)
			outcome.status = store.RunError
			outcome.errMsg = runErr.Error()
			return outcome
		case code != 0:
			if c.FailStrategy == store.FailContinue {
				fmt.Fprintf(w, "\n--- step %d failed (exit code %d), continuing per policy ---\n", i+1, code)
				continue
			}
			fmt.Fprintf(w, "\n*** step %d failed (exit code %d), aborting the remaining steps ***\n", i+1, code)
			outcome.status = store.RunFailed
			outcome.exitCode = code
			outcome.errMsg = fmt.Sprintf("step '%s' failed with exit code %d", step.Name, code)
			return outcome
		}
	}

	outcome.status = store.RunSuccess
	return outcome
}

// req0CommandIDs 已移除：命令筛选通过 Executor.cmdFilter 传递。

// jobLabel 给日志与错误信息用的中文对象名。
func jobLabel(job *jobSpec) string {
	if job.isTask() {
		return "task"
	}
	return "project"
}

func finishStep(st *store.Store, step *store.RunStep, runErr error, code int, offsetEnd int64) {
	now := time.Now()
	status := store.RunSuccess
	switch {
	case runErr != nil && errors.Is(runErr, context.Canceled):
		status = store.RunCanceled
	case runErr != nil && errors.Is(runErr, context.DeadlineExceeded):
		status = store.RunTimeout
	case runErr != nil:
		status = store.RunError
	case code != 0:
		status = store.RunFailed
	}
	dur := now.Sub(*step.StartedAt).Milliseconds()
	st.DB().Model(&store.RunStep{}).Where("id = ?", step.ID).Updates(map[string]any{
		"status": status, "finished_at": now, "duration_ms": dur,
		"exit_code": code, "log_offset_end": offsetEnd,
	})
}

func (e *Executor) finish(runID uint, outcome execOutcome, _ []store.RunStep) {
	var run store.Run
	if err := e.st.DB().First(&run, runID).Error; err != nil {
		return
	}
	now := time.Now()
	dur := int64(0)
	if run.StartedAt != nil {
		dur = now.Sub(*run.StartedAt).Milliseconds()
	}

	// 用户主动取消优先于其它判定：否则会被显示成 failed，误导使用者。
	if h := e.lookupHandle(runID); h != nil && h.canceledByUser() {
		if outcome.status != store.RunCanceled {
			outcome.status = store.RunCanceled
			outcome.errMsg = "cancelled by the user"
		}
	}
	if outcome.status == "" || outcome.status == store.RunQueued {
		outcome.status = store.RunError
		if outcome.errMsg == "" {
			outcome.errMsg = "execution ended unexpectedly"
		}
	}

	e.st.DB().Model(&store.Run{}).Where("id = ?", runID).Updates(map[string]any{
		"status":      outcome.status,
		"finished_at": now,
		"duration_ms": dur,
		"exit_code":   outcome.exitCode,
		"error_msg":   truncate(outcome.errMsg, 1000),
	})
}

func (e *Executor) lookupHandle(runID uint) *handle {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running[runID]
}

// runScript 用选定的 shell 执行一段脚本，输出直接落到日志写入器。
//
// timeout>0 时叠加一层 deadline；无论哪种结束方式，进程树都会被清理。
func (e *Executor) runScript(parent context.Context, script, workdir, shellOverride, projectShell string,
	env []string, timeout time.Duration, out io.Writer, h *handle) (int, error) {

	ctx := parent
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, timeout)
		defer cancel()
	}

	exe, args := resolveShell(shellOverride, projectShell, e.cfg.ShellPreference)
	args = append(args, script)

	onStart := func(c *exec.Cmd) {
		if h != nil {
			h.setCmd(c)
		}
	}
	code, err := runCommand(ctx, exe, args, workdir, env, out, onStart)
	if h != nil {
		h.clearCmd()
	}
	return code, err
}

// runCommand 执行一个外部命令并把 stdout/stderr 合并写入 out。
//
// 不用 exec.CommandContext：它只会杀直接子进程，把后台起的孙子进程留成孤儿。
// 这里自己管 ctx，在取消时走 killTree 杀整棵进程树。
func runCommand(ctx context.Context, exe string, args []string, dir string, env []string,
	out io.Writer, onStart func(*exec.Cmd)) (int, error) {

	cmd := exec.Command(exe, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if env != nil {
		cmd.Env = env
	}
	cmd.Stdout = out
	cmd.Stderr = out
	setProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		return -1, err
	}
	if onStart != nil {
		onStart(cmd)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		return exitCodeOf(err), nil
	case <-ctx.Done():
		killTree(cmd)
		<-done
		return -1, ctx.Err()
	}
}

func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
