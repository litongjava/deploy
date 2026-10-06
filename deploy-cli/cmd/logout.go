package cmd

import (
	"context"
	"time"

	"github.com/spf13/cobra"
)

func newLogoutCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "clear the token of the current server",
		Args:  exactArgs(0, "logout takes no positional arguments"),
		RunE: func(cmd *cobra.Command, args []string) error {
			p := app.Printer

			// 尽力通知服务端；失败不影响本地清理（JWT 无状态）。
			if client, err := app.Client(); err == nil && client.Token != "" {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				if lerr := client.Logout(ctx); lerr != nil {
					p.Verbosef("call to /auth/logout failed (ignored): %v", lerr)
				}
				cancel()
			}

			actx := app.Cfg.Active()
			had := actx.Token != ""
			actx.Token = ""
			if err := app.saveConfig(); err != nil {
				return err
			}
			return app.jsonOrHuman(map[string]any{
				"server":  app.Cfg.Current,
				"cleared": had,
			}, func() {
				if had {
					p.Printf("cleared the token of server %q\n", app.Cfg.Current)
				} else {
					p.Printf("server %q had no token stored\n", app.Cfg.Current)
				}
			})
		},
	}
}
