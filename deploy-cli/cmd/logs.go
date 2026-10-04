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
		Short: "查看运行日志（-f 实时跟随）",
		Long: `查看指定运行的日志。

  deploy logs 88                一次性拉取全部历史日志
  deploy logs 88 -f             先补历史，再实时跟随到运行结束
  deploy logs 88 --offset 4096  从指定字节偏移开始拉取

跟随模式下断线会自动重连，并按 nextOffset 续拉，不丢日志也不重复。`,
		Args: exactArgs(1, "用法：deploy logs <runId> [-f] [--offset N]"),
		RunE: func(cmd *cobra.Command, args []string) error {
			runID, err := parseRunID(args[0])
			if err != nil {
				return err
			}
			if offset < 0 {
				return exitcode.Usagef("--offset 不能为负数")
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
				p.Infof("已断开日志跟随（%s 未受影响）", runHead(runID, buildNumberOf(result)))
				return nil
			}
			if err != nil {
				return err
			}
			// exit 帧不带构建号，这里补一次查询，让摘要与日志头部对齐。
			return app.finishRun(runID, app.resolveBuildNumber(client, runID, result), result.Status, result.ExitCode, result.DurationMs)
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "实时跟随日志直到运行结束")
	cmd.Flags().Int64Var(&offset, "offset", 0, "起始字节偏移（默认 0，从头开始）")
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
		a.Printer.Verbosef("查询运行 %d 的构建号失败：%v", runID, err)
		return 0
	}
	return run.BuildNumber
}
