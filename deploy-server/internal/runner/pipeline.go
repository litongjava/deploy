package runner

import (
	"errors"
	"fmt"
	"time"

	"deploy-server/internal/ids"
	"deploy-server/internal/runlog"
	"deploy-server/internal/store"
)

// 依赖构建流水线。
//
// 一次「含依赖构建」的触发会产生多条 Run：为每个依赖项目各一条（按拓扑序执行），
// 最后是目标项目自己那条。它们用同一个 pipeline_id 串起来，规则是：
//
//   - 依赖在前、目标在后；同一条流水线里每个项目只构建一次（菱形依赖不重复构建）
//   - 任何一条依赖不是 success，后面的依赖与目标项目都不再执行（用户选定的语义）
//   - 依赖跑的时候目标 Run 保持 queued：界面上立刻能看到整条计划，但不会占着并发闸门
//
// 协调器不持有并发闸门，每条 Run 自己进出闸门，因此不会自锁。

// PipelineResult 是一次触发的成果：目标 Run，以及为满足依赖提前创建的 Run。
type PipelineResult struct {
	Main       *store.Run
	Deps       []*store.Run
	PipelineID string
}

// ErrDependency 表示依赖配置本身有问题（缺失 / 类型不对 / 归档 / 成环），
// 属于用户可修正的错误，调用方应回 400 而不是 500。
var ErrDependency = errors.New("dependency configuration is invalid")

// SubmitWithDependencies 触发一次构建；withDeps 为真时先按序构建依赖项目。
func (e *Executor) SubmitWithDependencies(req SubmitRequest, withDeps bool) (*PipelineResult, error) {
	p, err := e.loadProject(req.ProjectID)
	if err != nil {
		return nil, err
	}
	if p.Archived {
		return nil, ErrProjectArchived
	}
	if !withDeps {
		run, err := e.Submit(req)
		if err != nil {
			return nil, err
		}
		return &PipelineResult{Main: run}, nil
	}

	deps, err := e.st.ResolveDependencies(p)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDependency, err)
	}
	if len(deps) == 0 {
		// 没有依赖（或依赖已全部构建过）时保持单 Run 语义，行为与以前完全一致。
		run, err := e.Submit(req)
		if err != nil {
			return nil, err
		}
		return &PipelineResult{Main: run}, nil
	}

	pipelineID := ids.NewPipelineID()
	depReq := dependencyRequest(req)
	depRuns := make([]*store.Run, 0, len(deps))
	for i := range deps {
		dr, err := e.createRun(&deps[i], depReq, pipelineID, store.RoleDependency, i+1)
		if err != nil {
			return nil, err
		}
		depRuns = append(depRuns, dr)
	}
	main, err := e.createRun(p, req, pipelineID, store.RoleMain, 0)
	if err != nil {
		return nil, err
	}

	if p.Concurrency == store.ConcurrencyCancelPrev {
		e.CancelProject(p.ID)
	}
	// 命令筛选只作用于目标项目：依赖项目要完整构建。
	if len(req.CommandIDs) > 0 {
		e.mu.Lock()
		e.cmdFilter[main.ID] = req.CommandIDs
		e.mu.Unlock()
	}

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.runPipeline(main, req, depReq, depRuns)
	}()
	return &PipelineResult{Main: main, Deps: depRuns, PipelineID: pipelineID}, nil
}

// dependencyRequest 把触发参数改写成「给依赖项目用的」。
//
// 分支 / Tag 会传下去（依赖与目标应当来自同一条线），但触发时传入的 env 覆盖不传：
// 那是给目标项目的参数（镜像 tag、发布目录之类），套到依赖上往往语义不对。
func dependencyRequest(req SubmitRequest) SubmitRequest {
	return SubmitRequest{
		Ref:         req.Ref,
		TriggerType: req.TriggerType,
		TriggerUser: req.TriggerUser,
		TriggerUID:  req.TriggerUID,
		TriggerIP:   req.TriggerIP,
		TriggerUA:   req.TriggerUA,
	}
}

// runPipeline 顺序执行依赖，全部成功后再执行目标项目。
func (e *Executor) runPipeline(main *store.Run, mainReq, depReq SubmitRequest, depRuns []*store.Run) {
	for _, dr := range depRuns {
		req := depReq
		req.ProjectID = dr.ProjectID
		e.execute(dr.ID, req)

		var cur store.Run
		if err := e.st.DB().First(&cur, dr.ID).Error; err != nil {
			e.blockMain(main, nil, "failed to read the dependency run: "+err.Error())
			return
		}
		if cur.Status != store.RunSuccess {
			e.blockMain(main, &cur, "")
			return
		}
	}
	e.execute(main.ID, mainReq)
}

// blockMain 让目标项目「不执行」并给出为什么。
//
// 目标 Run 停在 queued 上会让人以为还在排队，所以这里落成明确的终态，
// 并写一条日志文件把原因写清楚（运行详情页的日志终端直接就能看到）。
func (e *Executor) blockMain(main *store.Run, dep *store.Run, override string) {
	status := store.RunFailed
	msg := override
	if msg == "" && dep != nil {
		if dep.Status == store.RunCanceled {
			status = store.RunCanceled
			msg = fmt.Sprintf("dependency build %s was canceled (run #%d); the main project was not executed",
				dep.ProjectKey, dep.ID)
		} else {
			msg = fmt.Sprintf("dependency build %s failed (run #%d, status %s); the main project was not executed",
				dep.ProjectKey, dep.ID, dep.Status)
		}
	}
	if status == store.RunFailed && msg == "" {
		msg = "the pipeline was aborted"
	}

	var logSize int64
	if w, err := runlog.NewWriter(e.cfg.LogDir(), main.ProjectKey, main.ID, e.broker, nil, e.logMaxBytes()); err == nil {
		fmt.Fprintf(w, "=== Deploy Run #%d (not started) ===\n", main.BuildNumber)
		fmt.Fprintf(w, "Project : %s (%s)\n", main.ProjectName, main.ProjectKey)
		fmt.Fprintf(w, "----------------------------------------\n")
		fmt.Fprintf(w, "Dependency build did not succeed, so this project was not executed.\n")
		fmt.Fprintf(w, "%s\n", msg)
		logSize = w.Close()
	}

	e.st.DB().Model(&store.Run{}).Where("id = ?", main.ID).Updates(map[string]any{
		"status":      status,
		"finished_at": time.Now(),
		"duration_ms": 0,
		"exit_code":   -1,
		"error_msg":   truncate(msg, 1000),
		"log_size":    logSize,
	})
	e.broker.CloseRun(main.ID)
}
