package cmd

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"deploy-cli/internal/api"
	"deploy-cli/internal/exitcode"
	"deploy-cli/internal/output"
	"deploy-cli/internal/ws"
)

func newRunCmd(app *App) *cobra.Command {
	var (
		follow            bool
		wait              bool
		branch            string
		envPairs          []string
		commandIDs        []int64
		cancelOnInterrupt bool
	)

	cmd := &cobra.Command{
		Use:   "run <projectKey>",
		Short: "trigger a project run (-f follows logs, --wait waits for the result)",
		Long: `Trigger a project run.

  deploy run my-app                    return immediately (prints the runId)
  deploy run my-app -f                 replay history, then follow live until the end
  deploy run my-app --wait             print no logs, just wait for the result
  deploy run my-app -e TAG=v1 --branch main

Ctrl+C only detaches log following by default and does not cancel the run; add --cancel-on-interrupt to call the cancel endpoint.`,
		Args: exactArgs(1, "usage: deploy run <projectKey> [-f] [-e K=V] [--branch REF] [--wait] [--cancel-on-interrupt]"),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := strings.TrimSpace(args[0])
			if key == "" {
				return exitcode.Usagef("project key must not be empty")
			}
			// 先做纯本地校验，再触碰网络与鉴权（保证用法错误优先返回退出码 2）。
			envMap, err := parseEnvPairs(envPairs)
			if err != nil {
				return err
			}
			for _, id := range commandIDs {
				if id <= 0 {
					return exitcode.Usagef("--command-id must be a positive integer, got %d", id)
				}
			}
			client, err := app.AuthClient()
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			res, err := client.TriggerRun(ctx, key, &api.RunRequest{
				Branch:     branch,
				Env:        envMap,
				CommandIDs: commandIDs,
			})
			cancel()
			if err != nil {
				return err
			}

			p := app.Printer

			// 不跟随也不等待：打印触发结果后立即返回。
			if !follow && !wait {
				return app.jsonOrHuman(map[string]any{
					"runId":       res.RunID,
					"projectKey":  res.ProjectKey,
					"buildNumber": res.BuildNumber,
					"status":      res.Status,
					"logUrl":      res.LogURL,
					"statusUrl":   res.StatusURL,
				}, func() {
					p.Printf("triggered run ID #%d (project %s, build #%d, status %s)\n",
						res.RunID, key, res.BuildNumber, res.Status)
					p.Infof("follow logs: deploy logs %d -f", res.RunID)
				})
			}

			sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			if follow {
				return app.followRun(sigCtx, client, res.RunID, res.BuildNumber, cancelOnInterrupt)
			}

			p.Infof("%s triggered, waiting for it to finish...", runHead(res.RunID, res.BuildNumber))
			run, err := waitForRun(sigCtx, client, res.RunID, 0, p)
			if errors.Is(err, context.Canceled) {
				p.Infof("stopped waiting (%s is still running)", runHead(res.RunID, res.BuildNumber))
				return nil
			}
			if err != nil {
				return err
			}
			if run == nil {
				return exitcode.Fail(exitcode.DeployFailed)
			}
			return app.finishRun(run.ID, run.BuildNumber, run.Status, run.ExitCode, run.DurationMs)
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&follow, "follow", "f", false, "replay history first, then follow live over WebSocket until the end")
	f.BoolVar(&wait, "wait", false, "wait for the run to finish and return (no logs)")
	f.StringVar(&branch, "branch", "", "branch / ref to use (written to requestedRef)")
	f.StringArrayVarP(&envPairs, "env", "e", nil, "override env vars K=V at trigger time; repeatable")
	f.Int64SliceVar(&commandIDs, "command-id", nil, "only run the given command IDs; repeatable")
	f.BoolVar(&cancelOnInterrupt, "cancel-on-interrupt", false, "call the cancel endpoint on Ctrl+C and exit")

	return cmd
}

// followRun 跟随日志直到结束，并按退出码语义返回。
// buildNumber 来自触发响应（项目内构建号），用于把摘要和日志头部对齐。
func (a *App) followRun(ctx context.Context, client *api.Client, runID int64, buildNumber int, cancelOnInterrupt bool) error {
	p := a.Printer
	follower := ws.NewFollower(client, ws.NewDialer(client))
	w := p.LogWriter()
	opts := ws.Options{
		RunID:   runID,
		Writer:  w,
		Verbose: verboseFn(p),
		OnStatus: func(_ string, step *ws.Step) {
			if step != nil {
				p.Verbosef("step [%d] %s", step.SortOrder, step.Name)
			}
		},
	}

	result, err := follower.Follow(ctx, opts)
	p.FlushLog(w)

	if result != nil && result.BuildNumber > 0 {
		buildNumber = result.BuildNumber
	}
	if errors.Is(err, context.Canceled) || ctx.Err() != nil {
		return a.handleInterrupt(client, runID, buildNumber, cancelOnInterrupt, result)
	}
	if err != nil {
		return err
	}
	return a.finishRun(runID, buildNumber, result.Status, result.ExitCode, result.DurationMs)
}

// handleInterrupt 处理 Ctrl+C：默认只断开跟随，只有 --cancel-on-interrupt 才取消运行。
func (a *App) handleInterrupt(client *api.Client, runID int64, buildNumber int, cancelRun bool, result *ws.Result) error {
	p := a.Printer

	if !cancelRun {
		p.Infof("log following detached (%s was not cancelled; follow again: deploy logs %d -f)", runHead(runID, buildNumber), runID)
		if result != nil && result.Terminated {
			return a.finishRun(runID, buildNumber, result.Status, result.ExitCode, result.DurationMs)
		}
		return nil
	}

	// 原 ctx 已被取消，必须用独立的 context 调取消接口。
	cctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	err := client.CancelRun(cctx, runID)
	cancel()
	if err != nil {
		p.Warnf("cancelling the run failed: %v", err)
	} else {
		p.Infof("requested cancellation of %s, waiting for it to finish...", runHead(runID, buildNumber))
	}

	run, werr := waitForRun(context.Background(), client, runID, 30*time.Second, p)
	if werr != nil {
		return werr
	}
	if run == nil {
		p.Warnf("timed out waiting for %s to finish", runHead(runID, buildNumber))
		return exitcode.Fail(exitcode.DeployFailed)
	}
	return a.finishRun(run.ID, run.BuildNumber, run.Status, run.ExitCode, run.DurationMs)
}

// finishRun 打印结果摘要并按退出码语义返回：
// success → 0；failed/timeout/error/canceled → 1。
// buildNumber > 0 时摘要里会同时标出全局 run ID 与项目内构建号。
func (a *App) finishRun(runID int64, buildNumber int, status string, exitCode *int, durationMs int64) error {
	p := a.Printer

	if p.JSON {
		// 跟随模式下 stdout 是 JSONL 日志流，结果也必须是一行，保证可逐行解析。
		payload := map[string]any{"runId": runID, "status": status, "durationMs": durationMs}
		if buildNumber > 0 {
			payload["buildNumber"] = buildNumber
		}
		if exitCode != nil {
			payload["exitCode"] = *exitCode
		}
		if err := p.JSONLine(payload); err != nil {
			return err
		}
	} else {
		p.Println(a.statusLine(runID, buildNumber, status, exitCode, durationMs))
	}

	if status == api.StatusSuccess {
		return nil
	}
	if status == "" {
		p.Errorf("could not get the final state of %s", runHead(runID, buildNumber))
		return exitcode.Fail(exitcode.DeployFailed)
	}
	p.Errorf("%s did not succeed: status=%s", runHead(runID, buildNumber), status)
	return exitcode.Fail(exitcode.DeployFailed)
}

// waitForRun 轮询运行状态直到终态；timeout<=0 表示一直等。
func waitForRun(ctx context.Context, client *api.Client, runID int64, timeout time.Duration, p *output.Printer) (*api.Run, error) {
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var last *api.Run
	for {
		run, err := client.GetRun(ctx, runID)
		switch {
		case err == nil:
			last = run
			if api.IsTerminalStatus(run.Status) {
				return run, nil
			}
			p.Verbosef("run #%d status %s, waiting...", runID, run.Status)
		case isTransient(err):
			p.Verbosef("failed to query the run status (%v), still waiting", err)
		default:
			return nil, err
		}

		if !deadline.IsZero() && time.Now().After(deadline) {
			return last, nil
		}
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-ticker.C:
		}
	}
}
