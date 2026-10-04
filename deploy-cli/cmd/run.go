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
		Short: "触发项目运行（-f 跟随日志，--wait 等待结果）",
		Long: `触发一次项目运行。

  deploy run my-app                    触发后立刻返回（打印 runId）
  deploy run my-app -f                 先补历史日志，再实时跟随到结束
  deploy run my-app --wait             不打印日志，只等结果
  deploy run my-app -e TAG=v1 --branch main

Ctrl+C 默认只断开日志跟随，不会取消运行；加上 --cancel-on-interrupt 才会调用取消接口。`,
		Args: exactArgs(1, "用法：deploy run <projectKey> [-f] [-e K=V] [--branch REF] [--wait] [--cancel-on-interrupt]"),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := strings.TrimSpace(args[0])
			if key == "" {
				return exitcode.Usagef("项目 key 不能为空")
			}
			// 先做纯本地校验，再触碰网络与鉴权（保证用法错误优先返回退出码 2）。
			envMap, err := parseEnvPairs(envPairs)
			if err != nil {
				return err
			}
			for _, id := range commandIDs {
				if id <= 0 {
					return exitcode.Usagef("--command-id 必须是正整数，收到 %d", id)
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
					p.Printf("已触发运行 ID #%d（项目 %s，构建号 #%d，状态 %s）\n",
						res.RunID, key, res.BuildNumber, res.Status)
					p.Infof("跟随日志：deploy logs %d -f", res.RunID)
				})
			}

			sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			if follow {
				return app.followRun(sigCtx, client, res.RunID, res.BuildNumber, cancelOnInterrupt)
			}

			p.Infof("%s 已触发，等待结束…", runHead(res.RunID, res.BuildNumber))
			run, err := waitForRun(sigCtx, client, res.RunID, 0, p)
			if errors.Is(err, context.Canceled) {
				p.Infof("已停止等待（%s 仍在继续）", runHead(res.RunID, res.BuildNumber))
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
	f.BoolVarP(&follow, "follow", "f", false, "先补历史日志，再通过 WebSocket 实时跟随到结束")
	f.BoolVar(&wait, "wait", false, "等待运行结束后返回（不输出日志）")
	f.StringVar(&branch, "branch", "", "指定分支 / ref（写入 requestedRef）")
	f.StringArrayVarP(&envPairs, "env", "e", nil, "触发时覆盖环境变量 K=V，可重复")
	f.Int64SliceVar(&commandIDs, "command-id", nil, "只执行指定命令 ID，可重复")
	f.BoolVar(&cancelOnInterrupt, "cancel-on-interrupt", false, "Ctrl+C 时调用取消接口并退出")

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
				p.Verbosef("步骤 [%d] %s", step.SortOrder, step.Name)
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
		p.Infof("已断开日志跟随（%s 未取消；重新跟随：deploy logs %d -f）", runHead(runID, buildNumber), runID)
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
		p.Warnf("取消运行失败：%v", err)
	} else {
		p.Infof("已请求取消 %s，等待其结束…", runHead(runID, buildNumber))
	}

	run, werr := waitForRun(context.Background(), client, runID, 30*time.Second, p)
	if werr != nil {
		return werr
	}
	if run == nil {
		p.Warnf("等待 %s 结束超时", runHead(runID, buildNumber))
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
		p.Errorf("未能获取 %s 的最终状态", runHead(runID, buildNumber))
		return exitcode.Fail(exitcode.DeployFailed)
	}
	p.Errorf("%s 未成功：status=%s", runHead(runID, buildNumber), status)
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
			p.Verbosef("运行 #%d 状态 %s，等待中…", runID, run.Status)
		case isTransient(err):
			p.Verbosef("查询运行状态失败（%v），继续等待", err)
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
