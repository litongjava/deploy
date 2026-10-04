package cmd

import (
	"github.com/spf13/cobra"

	"deploy-cli/internal/output"
)

func newWhoamiCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "显示当前登录用户与服务器",
		Args:  exactArgs(0, "whoami 不接受位置参数"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := app.AuthClient()
			if err != nil {
				return err
			}
			ctx, cancel := app.ctx()
			defer cancel()

			user, err := client.Me(ctx)
			if err != nil {
				return err
			}
			p := app.Printer
			return app.jsonOrHuman(map[string]any{
				"server":  app.Settings.Server,
				"context": app.Cfg.Current,
				"user":    user,
			}, func() {
				p.String(output.KeyValue([][2]string{
					{"用户", p.Bold(user.Username)},
					{"昵称", orDash(user.Nickname)},
					{"角色", user.Role},
					{"状态", user.Status},
					{"服务器", app.Settings.Server},
					{"context", app.Cfg.Current},
					{"最近登录", formatTime(user.LastLoginAt)},
				}))
			})
		},
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
