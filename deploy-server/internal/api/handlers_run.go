package api

import (
	"encoding/json"
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
}

// triggerRun 是 Web / CLI 触发的入口。
func (s *Server) triggerRun(c *gin.Context) {
	p, u, ok := s.loadProjectForEdit(c, c.Param("key"))
	if !ok {
		return
	}
	if p.Archived {
		BadRequest(c, "项目已归档，不能执行。请先取消归档")
		return
	}
	var req triggerReq
	_ = c.ShouldBindJSON(&req)

	ids := req.CommandIDs
	if req.CommandID != 0 {
		ids = append(ids, req.CommandID)
	}

	run, err := s.exec.Submit(runner.SubmitRequest{
		ProjectID:   p.ID,
		Ref:         req.Branch,
		Params:      req.Env,
		CommandIDs:  ids,
		TriggerType: triggerTypeFrom(c),
		TriggerUser: u.Username,
		TriggerUID:  u.ID,
		TriggerIP:   ClientIP(c),
		TriggerUA:   c.GetHeader("User-Agent"),
	})
	if err != nil {
		Internal(c, "触发失败: "+err.Error())
		return
	}
	s.audit(c, "trigger-run", p.Key, "run="+strconv.Itoa(int(run.ID)))
	OK(c, gin.H{
		"runId": run.ID, "buildNumber": run.BuildNumber, "status": run.Status,
		"projectKey": p.Key,
		"logUrl":     "/api/v1/runs/" + strconv.Itoa(int(run.ID)) + "/log",
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
		NotFound(c, "运行记录不存在")
		return nil, false
	}
	u := CurrentUser(c)
	if u.Role != "admin" && s.visibilityMode() == "ownerOnly" {
		var p store.Project
		if err := s.st.DB().First(&p, run.ProjectID).Error; err == nil && p.OwnerID != u.ID {
			NotFound(c, "运行记录不存在")
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
		Internal(c, "读取日志失败: "+err.Error())
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
		NotFound(c, "日志文件不存在（可能已被清理）")
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
		NotFound(c, "原项目已不存在")
		return
	}
	u := CurrentUser(c)
	if !s.canEdit(u, &p) {
		Forbidden(c, "只有项目负责人或管理员可以重新执行")
		return
	}
	if p.Archived {
		BadRequest(c, "项目已归档，不能执行")
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
		Internal(c, "重新执行失败: "+err.Error())
		return
	}
	s.audit(c, "rerun", p.Key, "from="+strconv.Itoa(int(run.ID)))
	OK(c, gin.H{
		"runId": newRun.ID, "buildNumber": newRun.BuildNumber,
		"status": newRun.Status, "projectKey": p.Key,
	})
}
