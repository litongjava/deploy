package cmd

import (
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"deploy-cli/internal/output"
)

func newProjectGetCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "查看项目详情（含命令列表与环境变量）",
		Args:  exactArgs(1, "用法：deploy project get <key>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := app.AuthClient()
			if err != nil {
				return err
			}
			ctx, cancel := app.ctx()
			defer cancel()

			proj, err := client.GetProject(ctx, args[0])
			if err != nil {
				return err
			}

			p := app.Printer
			return app.jsonOrHuman(proj, func() {
				gitInfo := "未启用"
				if proj.UseGit {
					gitInfo = proj.GitURL + " @" + orDash(proj.GitBranch)
					if proj.GitSubdir != "" {
						gitInfo += "（子目录 " + proj.GitSubdir + "）"
					}
				}
				p.String(output.KeyValue([][2]string{
					{"Key", p.Bold(proj.Key)},
					{"名称", orDash(proj.Name)},
					{"描述", orDash(proj.Description)},
					{"工作目录", orDash(proj.Workdir)},
					{"Shell", orDash(proj.Shell)},
					{"并发策略", orDash(proj.Concurrency)},
					{"超时", strconv.Itoa(proj.TimeoutSec) + " 秒（0=不限）"},
					{"Git", gitInfo},
					{"Hook 触发", boolText(proj.HookEnabled)},
					{"允许覆盖环境变量", boolText(proj.AllowEnvOverride)},
					{"负责人", orDash(proj.OwnerName)},
					{"已归档", boolText(proj.Archived)},
					{"最近运行", formatTime(proj.LastRunAt) + "  " + orDash(proj.LastRunStatus)},
					{"运行次数", strconv.Itoa(proj.RunCount)},
					{"创建时间", formatTime(proj.CreatedAt)},
				}))

				p.Println()
				p.Println(p.Bold("命令列表"))
				if len(proj.Commands) == 0 {
					p.Println("（没有命令）")
				} else {
					rows := make([][]string, 0, len(proj.Commands))
					for _, c := range proj.Commands {
						rows = append(rows, []string{
							strconv.Itoa(c.SortOrder),
							strconv.FormatInt(c.ID, 10),
							c.Name,
							boolText(c.Enabled),
							orDash(c.FailStrategy),
							oneLine(c.Script, 60),
						})
					}
					p.String(output.Table([]string{"#", "ID", "名称", "启用", "失败策略", "脚本"}, rows))
				}

				if len(proj.Envs) > 0 {
					p.Println()
					p.Println(p.Bold("环境变量"))
					rows := make([][]string, 0, len(proj.Envs))
					for _, e := range proj.Envs {
						value := e.Value
						if e.IsSecret {
							value = "******"
						}
						rows = append(rows, []string{e.Key, value})
					}
					p.String(output.Table([]string{"KEY", "VALUE"}, rows))
				}
			})
		},
	}
}

func boolText(b bool) string {
	if b {
		return "是"
	}
	return "否"
}

// oneLine 把多行脚本压成一行并截断，便于表格展示。
func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		// 按 rune 截断，避免切坏中文
		runes := []rune(s)
		if len(runes) > max {
			return string(runes[:max]) + "…"
		}
		return s + "…"
	}
	return s
}
