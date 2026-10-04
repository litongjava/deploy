package cmd

import (
	"context"
	"time"

	"github.com/spf13/cobra"
)

func newLogoutCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "清除当前 context 的 token",
		Args:  exactArgs(0, "logout 不接受位置参数"),
		RunE: func(cmd *cobra.Command, args []string) error {
			p := app.Printer

			// 尽力通知服务端；失败不影响本地清理（JWT 无状态）。
			if client, err := app.Client(); err == nil && client.Token != "" {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				if lerr := client.Logout(ctx); lerr != nil {
					p.Verbosef("调用 /auth/logout 失败（忽略）：%v", lerr)
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
				"context": app.Cfg.Current,
				"cleared": had,
			}, func() {
				if had {
					p.Printf("已清除 context %q 的 token\n", app.Cfg.Current)
				} else {
					p.Printf("context %q 本来就没有保存 token\n", app.Cfg.Current)
				}
			})
		},
	}
}
