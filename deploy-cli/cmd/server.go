package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"deploy-cli/internal/config"
	"deploy-cli/internal/exitcode"
	"deploy-cli/internal/output"
)

// server 命令组管理本地「服务器」：名字 + 地址 + token，都落在 ~/.deploy/config.yaml。
//
// 术语约定（键就是 name）：
//
//	name      —— 配置文件中 contexts 下的那个键，同时也是给人看、给 --server 用的名字。
//	             没有独立的显示标签：`server update <旧名> --name <新名>` 改的就是键，
//	             current 会跟着改名走。
//	address   —— 服务端地址（-a/--address 覆盖；DEPLOY_ADDRESS 环境变量次之）。
//
// 需求把这一步交给了使用者手工完成 —— 先在服务器上把 deploy-server 部署并初始化好，
// 再把「名字、地址、token」交给 CLI。所以这里必须能有**非交互**的一次性写法：
//
//	deploy server add 97 -a http://192.168.31.97:10055 -t dk_xxx -u admin
//
// 之后所有命令都可以用 --server 97 指定操作哪一台（不指定就用 current）。
// 名字为什么是 newServerCmd：函数名跟命令名走（cobra 的 Use 是 "server"），
// 这样 `newXxxCmd` ↔ `deploy xxx` 一一对应，README 与 --help 互相印证。
func newServerCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "manage servers (name + address + token)",
		Long: `Manage the local server list; everything is stored in ~/.deploy/config.yaml.

A server's name IS the key in the config file: it is what --server uses, and what
server update --name renames.

  deploy server list                            list all servers (tokens masked)
  deploy server get 97                          show one server
  deploy server add 97 -a http://1.2.3.4:10055 -t dk_xxx -u admin
  deploy server use 97                          make 97 the default server
  deploy server update 97 --name prod           rename 97 to prod (current follows)
  deploy server update 97 -t dk_new             update a server (only the fields you pass)
  deploy server remove 97                       delete a server
  deploy server current                         show the current default server

Two ways to pick a server (the latter wins):
  deploy server use 97 && deploy run my-app
  deploy --server 97 run my-app

Renaming is the only operation that changes the key: address/token/username move
with it, and current follows.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return exitcode.Usagef("unknown subcommand %q; available: list, get, add, use, update, remove, current", args[0])
			}
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		newServerListCmd(app),
		newServerGetCmd(app),
		newServerAddCmd(app),
		newServerUseCmd(app),
		newServerUpdateCmd(app),
		newServerRemoveCmd(app),
		newServerCurrentCmd(app),
	)
	return cmd
}

// serverView 是给 JSON 输出用的稳定结构（字段名不会随内部实现变化）。
type serverView struct {
	Key     string `json:"key"`  // 稳定 ID：--server 用它
	Name    string `json:"name"` // 与 Key 同值：键就是名字，保留给已有 JSON 消费方
	Address string `json:"address"`
	User    string `json:"user"`
	Comment string `json:"comment"`
	Token   string `json:"token"`
	Active  bool   `json:"active"`
}

func toServerView(s config.Server) serverView {
	return serverView{
		Key: s.Key, Name: s.Name, Address: s.Address, User: s.User,
		Comment: s.Comment, Token: s.Token, Active: s.Active,
	}
}

func newServerListCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "list all servers",
		Args:  exactArgs(0, "list takes no positional arguments"),
		RunE: func(cmd *cobra.Command, args []string) error {
			p := app.Printer
			servers := app.Cfg.Servers(maskToken)
			views := make([]serverView, 0, len(servers))
			for _, s := range servers {
				views = append(views, toServerView(s))
			}
			return app.jsonOrHuman(map[string]any{
				"current": app.Cfg.Current,
				"path":    app.Cfg.Path(),
				"servers": views,
			}, func() {
				if len(views) == 0 {
					p.Println("no servers configured yet; add one with:")
					p.Println("  deploy server add <name> -a <address> -t <token>")
					return
				}
				rows := make([][]string, 0, len(views))
				for _, v := range views {
					mark := ""
					if v.Active {
						mark = "*"
					}
					rows = append(rows, []string{mark, v.Name, orDash(v.Address), orDash(v.User), v.Token, orDash(v.Comment)})
				}
				p.String(output.Table([]string{"", "NAME", "ADDRESS", "USER", "TOKEN", "COMMENT"}, rows))
				p.Printf("\ncurrent server: %s (config file %s)\n", p.Bold(app.Cfg.Current), app.Cfg.Path())
			})
		},
	}
}

func newServerGetCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "show one server",
		Args:  exactArgs(1, "usage: deploy server get <key>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := strings.TrimSpace(args[0])
			ctx, ok := app.Cfg.GetServer(key)
			if !ok {
				return exitcode.Usagef("server %q does not exist; run `deploy server list` to see configured servers", key)
			}
			view := serverView{
				Key: key, Name: key, Address: ctx.Address, User: ctx.User,
				Comment: ctx.Comment, Token: maskToken(ctx.Token),
				Active: key == app.Cfg.Current,
			}
			p := app.Printer
			return app.jsonOrHuman(view, func() {
				p.String(output.KeyValue([][2]string{
					{"name", orDash(view.Name)},
					{"address", orDash(view.Address)},
					{"user", orDash(view.User)},
					{"token", view.Token},
					{"comment", orDash(view.Comment)},
					{"current", boolText(view.Active)},
					{"config file", app.Cfg.Path()},
				}))
			})
		},
	}
}

func newServerAddCmd(app *App) *cobra.Command {
	var (
		address string
		token   string
		user    string
		comment string
		noSet   bool
	)
	cmd := &cobra.Command{
		Use:   "add <key>",
		Short: "create (or overwrite) a server and make it the default",
		Long: `Create a server and save it to the local config file; it becomes the current server by default.

  deploy server add 97 -a http://192.168.31.97:10055 -t dk_xxx -u admin

<name> is the server's name AND its config key: --server <name> uses it. There is
no separate display name, so there is no --name flag here — rename later with
` + "`deploy server update <old> --name <new>`" + `.

An existing server with the same name is updated; pass --no-set-current to keep the
default server unchanged.`,
		Args: exactArgs(1, "usage: deploy server add <name> -a <address> -t <token> [-u <user>] [--comment <comment>]"),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := strings.TrimSpace(args[0])
			if strings.TrimSpace(address) == "" {
				return exitcode.Usagef("address is required: deploy server add %s -a http://<address>:10055 -t <token>", key)
			}
			target := config.NormalizeAddress(address)
			// 只填了地址和 token 就一定要落库，否则 add 完还是「未登录」状态很费解。
			// 这里不强制交互式登录：需求里 token 是使用者手工提供的信息。
			if strings.TrimSpace(token) == "" && isInteractive() {
				var err error
				token, err = promptLine("API token (may be empty; sign in later with deploy login)", "")
				if err != nil {
					return err
				}
			}
			srvKey, srvAddress, srvToken, srvUser, srvComment := key, target, token, user, comment
			ctx, err := app.Cfg.UpsertServer(srvKey,
				&srvAddress, &srvToken, &srvUser, &srvComment, !noSet)
			if err != nil {
				return exitcode.Usagef("%v", err)
			}
			if err := app.saveConfig(); err != nil {
				return err
			}
			view := serverView{
				Key: key, Name: key, Address: ctx.Address, User: ctx.User,
				Comment: ctx.Comment, Token: maskToken(ctx.Token), Active: key == app.Cfg.Current,
			}
			p := app.Printer
			return app.jsonOrHuman(view, func() {
				p.Printf("saved server %s -> %s\n", p.Bold(key), view.Address)
				if ctx.Token == "" {
					p.Warnf("no token yet: run `deploy server update %s -t <token>`, or `deploy login -a <address> --name %s`", key, key)
				}
				p.Printf("current server: %s\n", app.Cfg.Current)
				p.Printf("config file: %s\n", app.Cfg.Path())
			})
		},
	}
	addAddressFlag(cmd.Flags(), &address, "server address, e.g. http://192.168.31.97:10055")
	cmd.Flags().StringVarP(&token, "token", "t", "", "API token (create one under Settings -> API Token on the server)")
	cmd.Flags().StringVarP(&user, "user", "u", "", "username (recorded only, shown by whoami)")
	cmd.Flags().StringVar(&comment, "comment", "", "free-form comment, e.g. test-box")
	cmd.Flags().BoolVar(&noSet, "no-set-current", false, "do not make this the default server")
	return cmd
}

func newServerUseCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "use <key>",
		Short: "make a server the default",
		Args:  exactArgs(1, "usage: deploy server use <key>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := strings.TrimSpace(args[0])
			if _, ok := app.Cfg.GetServer(key); !ok {
				return exitcode.Usagef("server %q does not exist; run `deploy server list` to see configured servers", key)
			}
			if err := app.Cfg.UseContext(key); err != nil {
				return exitcode.Usagef("%v", err)
			}
			if err := app.saveConfig(); err != nil {
				return err
			}
			ctx, _ := app.Cfg.GetServer(key)
			p := app.Printer
			return app.jsonOrHuman(map[string]any{
				"current": key,
				"name":    key,
				"address": ctx.Address,
			}, func() {
				p.Printf("current server switched to %s (%s)\n", p.Bold(key), ctx.Address)
			})
		},
	}
}

func newServerUpdateCmd(app *App) *cobra.Command {
	var (
		address string
		token   string
		user    string
		comment string
		name    string
	)
	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "update a server (only the fields you pass)",
		Long: `Update an existing server. Only fields you pass explicitly are changed:

  deploy server update 97 -t dk_new_token
  deploy server update 97 -a http://10.0.0.5:10055
  deploy server update 97 --name prod            rename 97 to prod

--name IS a rename: the server's name is its config key, so the key, --server and
current all move to the new name. Refuses an empty or already-taken name.

Note: pass an empty string to clear a field (e.g. --user "");
omitted fields keep their value -- changing only the token never wipes the username.`,
		Args: exactArgs(1, "usage: deploy server update <name> [-a address] [-t token] [-u user] [--name new-name] [--comment comment]"),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := strings.TrimSpace(args[0])
			if _, ok := app.Cfg.GetServer(key); !ok {
				return exitcode.Usagef("server %q does not exist; run `deploy server list` to see configured servers", key)
			}
			var pAddress, pToken, pUser, pComment *string
			if cmd.Flags().Changed("address") || cmd.Flags().Changed("server") {
				pAddress = &address
			}
			if cmd.Flags().Changed("token") {
				pToken = &token
			}
			if cmd.Flags().Changed("user") {
				pUser = &user
			}
			if cmd.Flags().Changed("comment") {
				pComment = &comment
			}
			rename := cmd.Flags().Changed("name")
			if pAddress == nil && pToken == nil && pUser == nil && pComment == nil && !rename {
				return exitcode.Usagef("pass at least one field to update (-a / -t / -u / --name / --comment)")
			}
			if _, err := app.Cfg.UpsertServer(key, pAddress, pToken, pUser, pComment, false); err != nil {
				return exitcode.Usagef("%v", err)
			}
			newKey := key
			if rename {
				newKey = strings.TrimSpace(name)
				if newKey == "" {
					return exitcode.Usagef("--name must not be empty; a server's name is its key")
				}
				if err := app.Cfg.RenameServer(key, newKey); err != nil {
					return exitcode.Usagef("%v", err)
				}
			}
			if err := app.saveConfig(); err != nil {
				return err
			}
			ctx, _ := app.Cfg.GetServer(newKey)
			view := serverView{
				Key: newKey, Name: newKey, Address: ctx.Address, User: ctx.User,
				Comment: ctx.Comment, Token: maskToken(ctx.Token), Active: newKey == app.Cfg.Current,
			}
			p := app.Printer
			return app.jsonOrHuman(view, func() {
				if newKey != key {
					p.Printf("renamed server %s -> %s\n", p.Bold(key), p.Bold(newKey))
				}
				p.Printf("updated server %s -> %s (token %s)\n", p.Bold(newKey), orDash(view.Address), view.Token)
			})
		},
	}
	addAddressFlag(cmd.Flags(), &address, "new server address")
	cmd.Flags().StringVarP(&token, "token", "t", "", "new API token")
	cmd.Flags().StringVarP(&user, "user", "u", "", "new username")
	cmd.Flags().StringVar(&name, "name", "", "new name; renaming moves the config key and current to it")
	cmd.Flags().StringVar(&comment, "comment", "", "new comment")
	return cmd
}

func newServerRemoveCmd(app *App) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "remove <key>",
		Short: "delete a server",
		Args:  exactArgs(1, "usage: deploy server remove <key>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := strings.TrimSpace(args[0])
			if _, ok := app.Cfg.GetServer(key); !ok {
				return exitcode.Usagef("server %q does not exist; run `deploy server list` to see configured servers", key)
			}
			if !yes {
				if !isInteractive() {
					return exitcode.Usagef("non-interactive shell: pass --yes to confirm: deploy server remove %s --yes", key)
				}
				ans, err := promptLine("delete server "+key+"? type yes to continue", "")
				if err != nil {
					return err
				}
				if !strings.EqualFold(ans, "yes") && !strings.EqualFold(ans, "y") {
					app.Printer.Println("cancelled")
					return nil
				}
			}
			if err := app.Cfg.RemoveServer(key); err != nil {
				return exitcode.Usagef("%v", err)
			}
			if err := app.saveConfig(); err != nil {
				return err
			}
			p := app.Printer
			return app.jsonOrHuman(map[string]any{
				"removed": key,
				"current": app.Cfg.Current,
			}, func() {
				p.Printf("deleted server %s; current server: %s\n", key, app.Cfg.Current)
			})
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip interactive confirmation")
	return cmd
}

func newServerCurrentCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "show the current default server",
		Args:  exactArgs(0, "current takes no positional arguments"),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := app.Cfg.Current
			ctx, _ := app.Cfg.GetServer(key)
			view := map[string]any{
				"current": key,
				"path":    app.Cfg.Path(),
			}
			if ctx != nil {
				view["name"] = key
				view["address"] = ctx.Address
				view["user"] = ctx.User
				view["token"] = maskToken(ctx.Token)
				view["comment"] = ctx.Comment
			}
			return app.jsonOrHuman(view, func() {
				app.Printer.Println(key)
			})
		},
	}
}

// boolText 由 project_get.go 提供（"yes"/"no"），这里直接复用。
