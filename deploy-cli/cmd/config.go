package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"deploy-cli/internal/exitcode"
	"deploy-cli/internal/output"
)

func newConfigCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "read and write local config (address / token / user / current)",
		Long: `Read and write ~/.deploy/config.yaml.

  deploy config get                 show all current config (token masked)
  deploy config get address         read a single key
  deploy config set address http://127.0.0.1:10055
  deploy config set current prod    switch the default server (created if missing)
  deploy config list                list all servers
  deploy config path                show the config file path`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return exitcode.Usagef("unknown subcommand %q; available: get, set, list, path", args[0])
			}
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		newConfigGetCmd(app),
		newConfigSetCmd(app),
		newConfigListCmd(app),
		newConfigPathCmd(app),
	)
	return cmd
}

func newConfigGetCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "get [key]",
		Short: "read a config key (omit key to show everything)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p := app.Printer

			if len(args) == 1 {
				key := strings.ToLower(strings.TrimSpace(args[0]))
				value, err := app.Cfg.Get(key)
				if err != nil {
					return exitcode.Usagef("%v", err)
				}
				if p.JSON {
					return p.JSONOut(map[string]string{
						"key":    key,
						"value":  value,
						"server": app.Cfg.Current,
					})
				}
				p.Println(value)
				return nil
			}

			current, _ := app.Cfg.Get("current")
			address, _ := app.Cfg.Get("address")
			token, _ := app.Cfg.Get("token")
			user, _ := app.Cfg.Get("user")

			if p.JSON {
				return p.JSONOut(map[string]string{
					"current": current,
					"address": address,
					"token":   maskToken(token),
					"user":    user,
					"path":    app.Cfg.Path(),
				})
			}
			p.String(output.KeyValue([][2]string{
				{"current", current},
				{"address", orDash(address)},
				{"token", maskToken(token)},
				{"user", orDash(user)},
				{"config", app.Cfg.Path()},
			}))
			return nil
		},
	}
}

func newConfigSetCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "write a config key",
		Args:  exactArgs(2, "usage: deploy config set <key> <value> (key: address/token/user/current)"),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := strings.ToLower(strings.TrimSpace(args[0]))
			if err := app.Cfg.Set(key, args[1]); err != nil {
				return exitcode.Usagef("%v", err)
			}
			if err := app.saveConfig(); err != nil {
				return err
			}
			p := app.Printer
			return app.jsonOrHuman(map[string]any{
				"key":    key,
				"server": app.Cfg.Current,
				"path":   app.Cfg.Path(),
			}, func() {
				if key == "token" {
					p.Printf("token updated (server: %s, value: %s)\n", app.Cfg.Current, maskToken(args[1]))
				} else {
					p.Printf("set %s = %s (server: %s)\n", key, args[1], app.Cfg.Current)
				}
			})
		},
	}
}

func newConfigListCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "list all servers",
		Args:  exactArgs(0, "list takes no positional arguments"),
		RunE: func(cmd *cobra.Command, args []string) error {
			p := app.Printer
			// 复用 Servers()：按键排序，并跳过配置不存在时造出来的空 default 壳。
			servers := app.Cfg.Servers(maskToken)

			type ctxView struct {
				Key     string `json:"key"`  // 与 Name 同值：键就是名字
				Name    string `json:"name"` // --server 用它
				Address string `json:"address"`
				User    string `json:"user"`
				Token   string `json:"token"`
				Active  bool   `json:"active"`
			}
			views := make([]ctxView, 0, len(servers))
			for _, s := range servers {
				views = append(views, ctxView{
					Key: s.Key, Name: s.Name, Address: s.Address,
					User: s.User, Token: s.Token, Active: s.Active,
				})
			}

			return app.jsonOrHuman(map[string]any{
				"current": app.Cfg.Current,
				"servers": views,
			}, func() {
				rows := make([][]string, 0, len(views))
				for _, v := range views {
					mark := ""
					if v.Active {
						mark = "*"
					}
					rows = append(rows, []string{mark, v.Name, orDash(v.Address), orDash(v.User), v.Token})
				}
				p.String(output.Table([]string{"", "NAME", "ADDRESS", "USER", "TOKEN"}, rows))
			})
		},
	}
}

func newConfigPathCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "show the config file path",
		Args:  exactArgs(0, "path takes no positional arguments"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if app.Printer.JSON {
				return app.Printer.JSONOut(map[string]string{"path": app.Cfg.Path()})
			}
			app.Printer.Println(app.Cfg.Path())
			return nil
		},
	}
}
