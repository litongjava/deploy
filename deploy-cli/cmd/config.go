package cmd

import (
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"deploy-cli/internal/exitcode"
	"deploy-cli/internal/output"
)

func newConfigCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "读写本地配置（server / token / user / current）",
		Long: `读写 ~/.deploy/config.yaml。

  deploy config get                 显示当前全部配置（token 打码）
  deploy config get server          读取单项
  deploy config set server http://127.0.0.1:10055
  deploy config set current prod    切换 context（不存在则新建）
  deploy config list                列出所有 context
  deploy config path                显示配置文件路径`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return exitcode.Usagef("未知子命令 %q，可用：get、set、list、path", args[0])
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
		Short: "读取配置项（省略 key 时显示全部）",
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
						"key":     key,
						"value":   value,
						"context": app.Cfg.Current,
					})
				}
				p.Println(value)
				return nil
			}

			current, _ := app.Cfg.Get("current")
			server, _ := app.Cfg.Get("server")
			token, _ := app.Cfg.Get("token")
			user, _ := app.Cfg.Get("user")

			if p.JSON {
				return p.JSONOut(map[string]string{
					"current": current,
					"server":  server,
					"token":   maskToken(token),
					"user":    user,
					"path":    app.Cfg.Path(),
				})
			}
			p.String(output.KeyValue([][2]string{
				{"current", current},
				{"server", orDash(server)},
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
		Short: "写入配置项",
		Args:  exactArgs(2, "用法：deploy config set <key> <value>（key: server/token/user/current）"),
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
				"key":     key,
				"context": app.Cfg.Current,
				"path":    app.Cfg.Path(),
			}, func() {
				if key == "token" {
					p.Printf("已更新 token（context: %s，值: %s）\n", app.Cfg.Current, maskToken(args[1]))
				} else {
					p.Printf("已设置 %s = %s（context: %s）\n", key, args[1], app.Cfg.Current)
				}
			})
		},
	}
}

func newConfigListCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "列出所有 context",
		Args:  exactArgs(0, "list 不接受位置参数"),
		RunE: func(cmd *cobra.Command, args []string) error {
			p := app.Printer
			names := app.Cfg.ContextNames()
			sort.Strings(names)

			type ctxView struct {
				Name   string `json:"name"`
				Server string `json:"server"`
				User   string `json:"user"`
				Token  string `json:"token"`
				Active bool   `json:"active"`
			}
			views := make([]ctxView, 0, len(names))
			for _, n := range names {
				c := app.Cfg.Contexts[n]
				v := ctxView{Name: n, Active: n == app.Cfg.Current}
				if c != nil {
					v.Server = c.Server
					v.User = c.User
					v.Token = maskToken(c.Token)
				}
				views = append(views, v)
			}

			return app.jsonOrHuman(map[string]any{
				"current":  app.Cfg.Current,
				"contexts": views,
			}, func() {
				rows := make([][]string, 0, len(views))
				for _, v := range views {
					mark := ""
					if v.Active {
						mark = "*"
					}
					rows = append(rows, []string{mark, v.Name, orDash(v.Server), orDash(v.User), v.Token})
				}
				p.String(output.Table([]string{"", "CONTEXT", "SERVER", "USER", "TOKEN"}, rows))
			})
		},
	}
}

func newConfigPathCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "显示配置文件路径",
		Args:  exactArgs(0, "path 不接受位置参数"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if app.Printer.JSON {
				return app.Printer.JSONOut(map[string]string{"path": app.Cfg.Path()})
			}
			app.Printer.Println(app.Cfg.Path())
			return nil
		},
	}
}
