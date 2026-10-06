package cmd

import (
	"github.com/spf13/cobra"

	"deploy-cli/internal/output"
)

func newWhoamiCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "show the current user and server",
		Args:  exactArgs(0, "whoami takes no positional arguments"),
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
				"address": app.Settings.Address,
				"name":    app.Cfg.Current,
				"user":    user,
			}, func() {
				p.String(output.KeyValue([][2]string{
					{"user", p.Bold(user.Username)},
					{"nickname", orDash(user.Nickname)},
					{"role", user.Role},
					{"status", user.Status},
					{"server", app.Settings.Server},
					{"name", app.Cfg.Current},
					{"address", app.Settings.Address},
					{"last login", formatTime(user.LastLoginAt)},
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
