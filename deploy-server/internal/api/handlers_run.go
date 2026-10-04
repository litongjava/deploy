package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"deploy-server/internal/runlog"
	"deploy-server/internal/runner"
	"deploy-server/internal/store"
)

type triggerReq struct {
	Branch     string            `json:"branch"`
	Env        map[string]string `json:"env"`
	CommandIDs []uint            `json:"commandIds"`
	CommandID  uint              `json:"commandId"`
	// WithDependencies 为真时先按依赖顺序构建依赖的构建项目（见 §4.5）。
	WithDependencies bool `json:"withDependencies"`
}

// triggerRun 是 Web / CLI 触发的入口。
func (s *Server) triggerRun(c *gin.Context) {
	p, u, ok := s.loadProjectForEdit(c, c.Param("key"))
	if !ok {
		return
	}
	if p.Archived {
		BadRequest(c, "the project is archived and cannot run; unarchive it first")
		return
	}
	var req triggerReq
	_ = c.ShouldBindJSON(&req)

	ids := req.CommandIDs
	if req.CommandID != 0 {
		ids = append(ids, req.CommandID)
	}

	// 命令筛选与依赖构建不能同时用：只跑部分命令时，依赖也没法判断该不该重建。
	if req.WithDependencies && len(ids) > 0 {
		BadRequest(c, "single-command runs cannot be combined with withDependencies")
		return
	}

	res, err := s.exec.SubmitWithDependencies(runner.SubmitRequest{
		ProjectID:   p.ID,
		Ref:         req.Branch,
		Params:      req.Env,
		CommandIDs:  ids,
		TriggerType: triggerTypeFrom(c),
		TriggerUser: u.Username,
		TriggerUID:  u.ID,
		TriggerIP:   ClientIP(c),
		TriggerUA:   c.GetHeader("User-Agent"),
	}, req.WithDependencies)
	if err != nil {
		switch {
		case errors.Is(err, runner.ErrProjectArchived):
			BadRequest(c, err.Error())
		case errors.Is(err, runner.ErrDependency):
			BadRequest(c, err.Error())
		default:
			Internal(c, "trigger failed: "+err.Error())
		}
		return
	}
	run := res.Main
	s.audit(c, "trigger-run", p.Key, "run="+strconv.Itoa(int(run.ID)))

	deps := make([]gin.H, 0, len(res.Deps))
	for _, d := range res.Deps {
		deps = append(deps, gin.H{
			"key": d.ProjectKey, "runId": d.ID, "buildNumber": d.BuildNumber,
		})
	}
	OK(c, gin.H{
		"runId": run.ID, "buildNumber": run.BuildNumber, "status": run.Status,
		"projectKey":      p.Key,
		"pipelineId":      res.PipelineID,
		"dependencies":    deps,
		"dependencyCount": len(deps),
		"logUrl":          "/api/v1/runs/" + strconv.Itoa(int(run.ID)) + "/log",
	})
}

// triggerTypeFrom 区分 Web 与 CLI：CLI 会带 X-Deploy-Client 头。
func triggerTypeFrom(c *gin.Context) string {
	if strings.HasPrefix(c.GetHeader("User-Agent"), "deploy-cli") || c.GetHeader("X-Deploy-Client") != "" {
		return store.TriggerCLI
	}
	return store.TriggerWeb
}

func (s *Server) listRuns(c *gin.Context) {
	u := CurrentUser(c)
	page, size, offset := pageParams(c)

	q := s.st.DB().Model(&store.Run{})
	if pk := c.Query("projectKey"); pk != "" {
		q = q.Where("project_key = ?", pk)
	}
	if st := c.Query("status"); st != "" {
		q = q.Where("status = ?", st)
	}
	if c.Query("projectId") != "" {
		if id, err := strconv.Atoi(c.Query("projectId")); err == nil {
			q = q.Where("project_id = ?", id)
		}
	}
	// 可见性收敛：非管理员在 ownerOnly 模式下只能看到自己项目的运行。
	if u.Role != "admin" && s.visibilityMode() == "ownerOnly" {
		sub := s.st.DB().Model(&store.Project{}).Select("id").Where("owner_id = ?", u.ID)
		q = q.Where("project_id IN (?)", sub)
	}

	var total int64
	q.Count(&total)
	var runs []store.Run
	q.Order("id desc").Limit(size).Offset(offset).Find(&runs)

	list := make([]runDTO, 0, len(runs))
	for i := range runs {
		list = append(list, toRunDTO(&runs[i]))
	}
	OK(c, NewPage(list, total, page, size))
}

func (s *Server) getRun(c *gin.Context) {
	run, ok := s.loadRunFor(c)
	if !ok {
		return
	}
	d := toRunDTO(run)
	var steps []store.RunStep
	s.st.DB().Where("run_id = ?", run.ID).Order("phase asc, sort_order asc, id asc").Find(&steps)
	d.Steps = make([]stepDTO, 0, len(steps))
	for _, st := range steps {
		d.Steps = append(d.Steps, toStepDTO(st))
	}
	// 依赖构建流水线：把同一次触发里的其它 Run 一并带上，详情页要展示「前置构建」。
	if run.PipelineID != "" {
		var siblings []store.Run
		// 依赖在前、目标项目排最后：这个顺序本身就说明了「先构建谁」。
		s.st.DB().Where("pipeline_id = ?", run.PipelineID).
			Order("CASE WHEN pipeline_role = 'main' THEN 1 ELSE 0 END asc, pipeline_order asc, id asc").
			Find(&siblings)
		d.Pipeline = make([]pipelineRun, 0, len(siblings))
		for _, sr := range siblings {
			d.Pipeline = append(d.Pipeline, toPipelineRun(sr))
		}
	}
	OK(c, d)
}

// loadRunFor 取 Run 并做可见性校验。
func (s *Server) loadRunFor(c *gin.Context) (*store.Run, bool) {
	id, ok := idParam(c, "id")
	if !ok {
		return nil, false
	}
	var run store.Run
	if err := s.st.DB().First(&run, id).Error; err != nil {
		NotFound(c, "run not found")
		return nil, false
	}
	u := CurrentUser(c)
	if u.Role != "admin" && s.visibilityMode() == "ownerOnly" {
		var p store.Project
		if err := s.st.DB().First(&p, run.ProjectID).Error; err == nil && p.OwnerID != u.ID {
			NotFound(c, "run not found")
			return nil, false
		}
	}
	return &run, true
}

func (s *Server) logPathOf(run *store.Run) string {
	return runlog.PathFor(s.cfg.LogDir(), run.ProjectKey, run.ID)
}

// getRunLog 按 offset 增量拉取日志，供前端断线重连后补齐。
func (s *Server) getRunLog(c *gin.Context) {
	run, ok := s.loadRunFor(c)
	if !ok {
		return
	}
	offset, _ := strconv.ParseInt(c.DefaultQuery("offset", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "65536"))
	if limit <= 0 || limit > 1<<20 {
		limit = 65536
	}

	data, next, eof, err := runlog.ReadRange(s.logPathOf(run), offset, limit)
	if err != nil {
		Internal(c, "failed to read the log: "+err.Error())
		return
	}
	OK(c, gin.H{
		"offset": offset, "nextOffset": next, "eof": eof,
		"data": string(data),
	})
}

func (s *Server) downloadRunLog(c *gin.Context) {
	run, ok := s.loadRunFor(c)
	if !ok {
		return
	}
	path := s.logPathOf(run)
	if _, err := os.Stat(path); err != nil {
		NotFound(c, "log file not found (it may have been pruned)")
		return
	}
	name := fmt.Sprintf("%s-%d.log", run.ProjectKey, run.BuildNumber)
	c.Header("Content-Disposition", "attachment; filename="+name)
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.File(path)
}

func (s *Server) cancelRun(c *gin.Context) {
	run, ok := s.loadRunFor(c)
	if !ok {
		return
	}
	// 幂等：已经结束的运行直接返回当前状态，不报错。
	running := s.exec.Cancel(run.ID)
	s.st.DB().First(run, run.ID)
	s.audit(c, "cancel-run", run.ProjectKey, "run="+strconv.Itoa(int(run.ID)))
	OK(c, gin.H{"runId": run.ID, "status": run.Status, "canceled": running})
}

func (s *Server) rerun(c *gin.Context) {
	run, ok := s.loadRunFor(c)
	if !ok {
		return
	}
	var p store.Project
	if err := s.st.DB().First(&p, run.ProjectID).Error; err != nil {
		NotFound(c, "the original project no longer exists")
		return
	}
	u := CurrentUser(c)
	if !s.canEdit(u, &p) {
		Forbidden(c, "only the project owner or an admin can re-run this")
		return
	}
	if p.Archived {
		BadRequest(c, "the project is archived and cannot run")
		return
	}

	params := map[string]string{}
	if run.ParamsJSON != "" {
		_ = json.Unmarshal([]byte(run.ParamsJSON), &params)
	}
	newRun, err := s.exec.Submit(runner.SubmitRequest{
		ProjectID:   p.ID,
		Ref:         run.RequestedRef,
		Params:      params,
		TriggerType: store.TriggerRerun,
		TriggerUser: u.Username,
		TriggerUID:  u.ID,
		TriggerIP:   ClientIP(c),
		TriggerUA:   c.GetHeader("User-Agent"),
	})
	if err != nil {
		Internal(c, "re-run failed: "+err.Error())
		return
	}
	s.audit(c, "rerun", p.Key, "from="+strconv.Itoa(int(run.ID)))
	OK(c, gin.H{
		"runId": newRun.ID, "buildNumber": newRun.BuildNumber,
		"status": newRun.Status, "projectKey": p.Key,
	})
}
