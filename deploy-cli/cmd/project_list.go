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
		Short:   "列出项目（表格输出）",
		Args:    exactArgs(0, "list 不接受位置参数"),
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
					p.Println("（没有匹配的项目）")
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
						name += "（已归档）"
					}
					rows = append(rows, []string{
						proj.Key,
						name,
						orDash(proj.Workdir),
						lastRun,
						p.Status(status),
					})
				}
				p.String(output.Table([]string{"KEY", "名称", "工作目录", "最近运行", "状态"}, rows))
				p.Printf("共 %d 个，第 %d 页（每页 %d）\n", result.Total, result.Page, result.PageSize)
			})
		},
	}
	cmd.Flags().IntVar(&page, "page", 1, "页码（从 1 开始）")
	cmd.Flags().IntVar(&pageSize, "page-size", 50, "每页条数（最大 200）")
	cmd.Flags().StringVar(&keyword, "keyword", "", "按关键字过滤")
	cmd.Flags().BoolVar(&archived, "archived", false, "只列出已归档的项目")
	return cmd
}
