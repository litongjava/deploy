package cmd

import (
	"github.com/spf13/cobra"

	"deploy-cli/internal/output"
)

func newProjectListCmd(app *App) *cobra.Command {
	var (
		page     int
		pageSize int
		keyword  string
		archived bool
	)
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "list projects (table output)",
		Args:    exactArgs(0, "list takes no positional arguments"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := app.AuthClient()
			if err != nil {
				return err
			}
			ctx, cancel := app.ctx()
			defer cancel()

			// 契约：archived 不传 = 未归档。
			var archivedFilter *bool
			if archived {
				v := true
				archivedFilter = &v
			}
			result, err := client.ListProjects(ctx, page, pageSize, keyword, archivedFilter)
			if err != nil {
				return err
			}

			p := app.Printer
			return app.jsonOrHuman(result, func() {
				if len(result.List) == 0 {
					p.Println("(no matching projects)")
					return
				}
				rows := make([][]string, 0, len(result.List))
				for _, proj := range result.List {
					lastRun := "-"
					if proj.LastRunAt != "" {
						lastRun = formatTime(proj.LastRunAt)
					}
					status := proj.LastRunStatus
					if status == "" {
						status = "-"
					}
					name := proj.Name
					if proj.Archived {
						name += "(archived)"
					}
					rows = append(rows, []string{
						proj.Key,
						name,
						orDash(proj.Workdir),
						lastRun,
						p.Status(status),
					})
				}
				p.String(output.Table([]string{"KEY", "name", "workdir", "last run", "status"}, rows))
				p.Printf("total %d, page %d (page size %d)\n", result.Total, result.Page, result.PageSize)
			})
		},
	}
	cmd.Flags().IntVar(&page, "page", 1, "page number (starts at 1)")
	cmd.Flags().IntVar(&pageSize, "page-size", 50, "page size (max 200)")
	cmd.Flags().StringVar(&keyword, "keyword", "", "filter by keyword")
	cmd.Flags().BoolVar(&archived, "archived", false, "only list archived projects")
	return cmd
}
