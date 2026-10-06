package cmd

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"

	"deploy-cli/internal/api"
	"deploy-cli/internal/exitcode"
	"deploy-cli/internal/ws"
)

func newLogsCmd(app *App) *cobra.Command {
	var (
		follow bool
		offset int64
	)

	cmd := &cobra.Command{
		Use:   "logs <runId>",
		Short: "show run logs (-f follows live)",
		Long: `Show the logs of a run.

  deploy logs 88                fetch all historical logs at once
  deploy logs 88 -f             replay history, then follow live until the run ends
  deploy logs 88 --offset 4096  start from the given byte offset

In follow mode a dropped connection reconnects automatically and resumes from nextOffset: no lost or duplicated logs.`,
		Args: exactArgs(1, "usage: deploy logs <runId> [-f] [--offset N]"),
		RunE: func(cmd *cobra.Command, args []string) error {
			runID, err := parseRunID(args[0])
			if err != nil {
				return err
			}
			if offset < 0 {
				return exitcode.Usagef("--offset must not be negative")
			}
			client, err := app.AuthClient()
			if err != nil {
				return err
			}
			p := app.Printer
			w := p.LogWriter()

			if !follow {
				// 历史日志可能很大：给一个宽松的超时，而不是普通请求的 60s。
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				_, err := ws.FetchHistory(ctx, client, ws.Options{
					RunID:   runID,
					Offset:  offset,
					Writer:  w,
					Verbose: verboseFn(p),
				})
				p.FlushLog(w)
				return err
			}

			sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
			defer stop()

			follower := ws.NewFollower(client, ws.NewDialer(client))
			result, err := follower.Follow(sigCtx, ws.Options{
				RunID:   runID,
				Offset:  offset,
				Writer:  w,
				Verbose: verboseFn(p),
			})
			p.FlushLog(w)

			if errors.Is(err, context.Canceled) || sigCtx.Err() != nil {
				p.Infof("log following detached (%s is unaffected)", runHead(runID, buildNumberOf(result)))
				return nil
			}
			if err != nil {
				return err
			}
			// exit 帧不带构建号，这里补一次查询，让摘要与日志头部对齐。
			return app.finishRun(runID, app.resolveBuildNumber(client, runID, result), result.Status, result.ExitCode, result.DurationMs)
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow logs live until the run ends")
	cmd.Flags().Int64Var(&offset, "offset", 0, "starting byte offset (default 0, from the beginning)")
	return cmd
}

func buildNumberOf(result *ws.Result) int {
	if result == nil {
		return 0
	}
	return result.BuildNumber
}

// resolveBuildNumber 尽量拿到项目内构建号：优先用跟随结果里的，
// 否则用一次轻量的 GET /runs/{id} 查询（失败不影响主流程）。
func (a *App) resolveBuildNumber(client *api.Client, runID int64, result *ws.Result) int {
	if n := buildNumberOf(result); n > 0 {
		return n
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	run, err := client.GetRun(ctx, runID)
	if err != nil || run == nil {
		a.Printer.Verbosef("failed to query the build number of run %d: %v", runID, err)
		return 0
	}
	return run.BuildNumber
}
