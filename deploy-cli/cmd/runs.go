package cmd

import (
	"strconv"

	"github.com/spf13/cobra"

	"deploy-cli/internal/api"
	"deploy-cli/internal/exitcode"
	"deploy-cli/internal/output"
)

func newRunsCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "运行历史（runs list）",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return exitcode.Usagef("未知子命令 %q，可用：list", args[0])
			}
			return cmd.Help()
		},
	}
	cmd.AddCommand(newRunsListCmd(app))
	return cmd
}

func newRunsListCmd(app *App) *cobra.Command {
	var (
		project  string
		status   string
		page     int
		pageSize int
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "列出运行历史（可按项目/状态过滤）",
		Long: `列出运行历史。

  deploy runs list
  deploy runs list --project my-app --status failed --page 2

status 取值：queued/running/success/failed/canceled/timeout/error`,
		Args: exactArgs(0, "list 不接受位置参数"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if status != "" && !validRunStatus(status) {
				return exitcode.Usagef("--status 只能是 queued/running/success/failed/canceled/timeout/error，收到 %q", status)
			}
			client, err := app.AuthClient()
			if err != nil {
				return err
			}
			ctx, cancel := app.ctx()
			defer cancel()

			result, err := client.ListRuns(ctx, page, pageSize, project, status)
			if err != nil {
				return err
			}

			p := app.Printer
			return app.jsonOrHuman(result, func() {
				if len(result.List) == 0 {
					p.Println("（没有匹配的运行）")
					return
				}
				rows := make([][]string, 0, len(result.List))
				for _, r := range result.List {
					exitCode := "-"
					if r.ExitCode != nil {
						exitCode = strconv.Itoa(*r.ExitCode)
					}
					rows = append(rows, []string{
						strconv.FormatInt(r.ID, 10),
						orDash(r.ProjectKey),
						"#" + strconv.Itoa(r.BuildNumber),
						p.Status(r.Status),
						orDash(r.TriggerType),
						orDash(r.TriggerUserName),
						formatTime(r.StartedAt),
						api.FormatDuration(r.DurationMs),
						exitCode,
					})
				}
				p.String(output.Table(
					[]string{"RUN", "项目", "构建号", "状态", "触发方式", "触发人", "开始时间", "耗时", "退出码"},
					rows))
				p.Printf("共 %d 条，第 %d 页（每页 %d）\n", result.Total, result.Page, result.PageSize)
			})
		},
	}

	cmd.Flags().StringVar(&project, "project", "", "按项目 key 过滤")
	cmd.Flags().StringVar(&status, "status", "", "按状态过滤：queued/running/success/failed/canceled/timeout/error")
	cmd.Flags().IntVar(&page, "page", 1, "页码（从 1 开始）")
	cmd.Flags().IntVar(&pageSize, "page-size", 20, "每页条数（最大 200）")
	return cmd
}

func validRunStatus(s string) bool {
	switch s {
	case api.StatusQueued, api.StatusRunning, api.StatusSuccess, api.StatusFailed,
		api.StatusCanceled, api.StatusTimeout, api.StatusError:
		return true
	default:
		return false
	}
}
