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
		Short: "show project details (including commands and env vars)",
		Args:  exactArgs(1, "usage: deploy project get <key>"),
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
				gitInfo := "disabled"
				if proj.UseGit {
					gitInfo = proj.GitURL + " @" + orDash(proj.GitBranch)
					if proj.GitSubdir != "" {
						gitInfo += "(subdir " + proj.GitSubdir + ")"
					}
				}
				p.String(output.KeyValue([][2]string{
					{"Key", p.Bold(proj.Key)},
					{"name", orDash(proj.Name)},
					{"description", orDash(proj.Description)},
					{"workdir", orDash(proj.Workdir)},
					{"Shell", orDash(proj.Shell)},
					{"concurrency", orDash(proj.Concurrency)},
					{"timeout", strconv.Itoa(proj.TimeoutSec) + " s (0 = unlimited)"},
					{"Git", gitInfo},
					{"Hook trigger", boolText(proj.HookEnabled)},
					{"allow env override", boolText(proj.AllowEnvOverride)},
					{"owner", orDash(proj.OwnerName)},
					{"archived", boolText(proj.Archived)},
					{"last run", formatTime(proj.LastRunAt) + "  " + orDash(proj.LastRunStatus)},
					{"run count", strconv.Itoa(proj.RunCount)},
					{"created at", formatTime(proj.CreatedAt)},
				}))

				p.Println()
				p.Println(p.Bold("commands"))
				if len(proj.Commands) == 0 {
					p.Println("(no commands)")
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
					p.String(output.Table([]string{"#", "ID", "name", "enabled", "failure strategy", "script"}, rows))
				}

				if len(proj.Envs) > 0 {
					p.Println()
					p.Println(p.Bold("env vars"))
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
		return "yes"
	}
	return "no"
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
