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
		Short: "run history (runs list)",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return exitcode.Usagef("unknown subcommand %q; available: list", args[0])
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
		Short:   "list run history (filterable by project/status)",
		Long: `List run history.

  deploy runs list
  deploy runs list --project my-app --status failed --page 2

status values: queued/running/success/failed/canceled/timeout/error`,
		Args: exactArgs(0, "list takes no positional arguments"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if status != "" && !validRunStatus(status) {
				return exitcode.Usagef("--status must be queued/running/success/failed/canceled/timeout/error, got %q", status)
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
					p.Println("(no matching runs)")
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
					[]string{"RUN", "project", "build", "status", "trigger", "triggered by", "started at", "duration", "exit code"},
					rows))
				p.Printf("total %d, page %d (page size %d)\n", result.Total, result.Page, result.PageSize)
			})
		},
	}

	cmd.Flags().StringVar(&project, "project", "", "filter by project key")
	cmd.Flags().StringVar(&status, "status", "", "filter by status: queued/running/success/failed/canceled/timeout/error")
	cmd.Flags().IntVar(&page, "page", 1, "page number (starts at 1)")
	cmd.Flags().IntVar(&pageSize, "page-size", 20, "page size (max 200)")
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
