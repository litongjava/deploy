package cmd

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"deploy-cli/internal/api"
	"deploy-cli/internal/config"
	"deploy-cli/internal/exitcode"
)

func newLoginCmd(app *App) *cobra.Command {
	var (
		address  string
		username string
		password string
		token    string
		name     string
	)
	cmd := &cobra.Command{
		Use:   "login",
		Short: "sign in and save the token (or write one with --token)",
		Long: `Sign in to the server and save the token.

Without -p the password is prompted and not echoed;
--token writes an API token / JWT directly, skipping the username/password sign-in.

The token is stored under the server named by --name; when --name is omitted that
server is "default". A server that does not exist yet is created once the sign-in
succeeds, so login alone is enough:

  deploy login -a http://192.168.31.97:10055 -u admin --name 971
  deploy --server 971 whoami

The global --server flag does NOT pick the write target for login: it only decides
which stored address/token the request uses. Use --name to choose the server that
login writes into.`,
		Args: exactArgs(0, "login takes no positional arguments"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogin(app, address, username, password, token, name)
		},
	}
	addAddressFlag(cmd.Flags(), &address, "server address, e.g. http://127.0.0.1:10055")
	cmd.Flags().StringVarP(&username, "username", "u", "", "username (defaults to admin)")
	cmd.Flags().StringVarP(&password, "password", "p", "", "password; prompted (not echoed) when omitted")
	cmd.Flags().StringVarP(&token, "token", "t", "", "write an API token / JWT directly, skipping sign-in")
	cmd.Flags().StringVar(&name, "name", "", "server to write into, e.g. 971 (created if missing; defaults to \"default\")")
	return cmd
}

// loginTargetKey 决定 login 往哪台服务器里写：--name，缺省是 default。
//
// 这里刻意不看全局 --server：--server 是「用哪台已存好的服务器发请求」，
// login 是「把登录结果写到哪台」。两者混在一起会出现
// 「读 971 的地址、却把 token 写进 default」这种自相矛盾的状态，
// 所以写入目标只认 --name（缺省 default），别的都影响不了它。
func loginTargetKey(name string) string {
	if k := strings.TrimSpace(name); k != "" {
		return k
	}
	return config.DefaultContext
}

// warnIfServerFlagIgnored 在「显式传了全局 --server 但没传 --name」时提醒一句。
//
// 此时地址/token 来自 --server 点名的那台，但登录结果会写进 default。
// 脚本里静默写错地方比直接报错更难查，所以给一条 stderr 警告
// （走 Printer.Warnf，不会污染 --json 的 stdout）。
func warnIfServerFlagIgnored(app *App, name string) {
	if strings.TrimSpace(name) != "" {
		return
	}
	if app.Settings.ServerSource == "flag" && app.Settings.Server != "" && app.Settings.Server != config.DefaultContext {
		app.Printer.Warnf("--server %s only selects which stored address/token to use; login writes into --name (here %q). Pass --name %s to write into it.",
			app.Settings.Server, config.DefaultContext, app.Settings.Server)
	}
}

func runLogin(app *App, address, username, password, token, name string) error {
	p := app.Printer
	key := loginTargetKey(name)
	if err := config.ValidateServerName(key); err != nil {
		return exitcode.Usagef("%v", err)
	}
	warnIfServerFlagIgnored(app, name)

	target := app.Settings.Address
	if strings.TrimSpace(address) != "" {
		target = config.NormalizeAddress(address)
	}
	if strings.TrimSpace(target) == "" {
		return exitcode.Usagef("server address is required: deploy login -a http://127.0.0.1:10055")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if t := strings.TrimSpace(token); t != "" {
		return loginWithToken(app, ctx, target, t, key)
	}

	if strings.TrimSpace(username) == "" {
		username = app.Settings.User
	}
	if strings.TrimSpace(username) == "" {
		if !isInteractive() {
			return exitcode.Usagef("non-interactive shell: pass -u/--username, or --token to write a token directly")
		}
		var err error
		username, err = promptLine("username", "admin")
		if err != nil {
			return err
		}
	}
	if strings.TrimSpace(password) == "" {
		if !isInteractive() {
			return exitcode.Usagef("non-interactive shell: pass -p/--password, or --token to write a token directly")
		}
		var err error
		password, err = readPassword("password (not echoed): ")
		if err != nil {
			return err
		}
	}

	client := api.New(target, "")
	res, err := client.Login(ctx, username, password)
	if err != nil {
		var e *api.Error
		if errors.As(err, &e) && e.IsAuth() {
			return exitcode.Authf("sign-in failed: %s", e.Message)
		}
		return err
	}
	if res.Token == "" {
		return exitcode.Netf("the server did not return a token")
	}

	// 注意：局部变量名不能叫 name，否则会遮蔽 --name 参数，让 login --name 静默失效。
	userName := res.User.Username
	if userName == "" {
		userName = username
	}

	// 到这里才算「登录成功」——此时才落盘，键不存在就新建。
	if err := app.saveLogin(key, target, res.Token, userName); err != nil {
		return err
	}

	if res.MustChangePassword {
		p.Warnf("this account must change its password; do it in the web UI or via /auth/change-password")
	}
	return app.jsonOrHuman(map[string]any{
		"address":   target,
		"server":    key,
		"name":      key,
		"username":  userName,
		"role":      res.User.Role,
		"token":     maskToken(res.Token),
		"expiresAt": res.ExpiresAt,
	}, func() {
		p.Printf("signed in: %s @ %s\n", p.Bold(userName), target)
		p.Printf("token saved to %s (server: %s)\n", app.Cfg.Path(), key)
		if res.ExpiresAt > 0 {
			p.Printf("valid until %s\n", time.Unix(res.ExpiresAt, 0).Local().Format("2006-01-02 15:04:05"))
		}
	})
}

// loginWithToken 直接写入 token；能连上服务端时顺便校验并补全用户名。
func loginWithToken(app *App, ctx context.Context, target, token, key string) error {
	p := app.Printer
	username := ""

	client := api.New(target, token)
	if u, err := client.Me(ctx); err == nil {
		username = u.Username
		p.Verbosef("token verified: %s", username)
	} else {
		var e *api.Error
		if errors.As(err, &e) && e.IsAuth() {
			return exitcode.Authf("token is invalid or expired: %s", e.Message)
		}
		p.Warnf("cannot verify the token (%v); writing it to the config anyway", err)
	}
	if username == "" {
		username = app.Settings.User
	}

	if err := app.saveLogin(key, target, token, username); err != nil {
		return err
	}
	return app.jsonOrHuman(map[string]any{
		"address":  target,
		"server":   key,
		"name":     key,
		"username": username,
		"token":    maskToken(token),
	}, func() {
		if username != "" {
			p.Printf("token saved (user %s @ %s, server: %s)\n", p.Bold(username), target, key)
		} else {
			p.Printf("token saved (%s, server: %s)\n", target, key)
		}
	})
}

// saveLogin 把一次登录的结果写进配置文件，并把该服务器设为当前服务器。
//
// 名字就是键：--name 指定的名字不存在就新建（缺省 default）——
// 这样 deploy login --name 971 一步到位，不必先 server add。
// 已存在时只更新地址/token/用户名，名字（键）保持不变。
func (a *App) saveLogin(key, address, token, user string) error {
	var pUser *string
	if strings.TrimSpace(user) != "" {
		pUser = &user
	}
	if _, err := a.Cfg.UpsertServer(key, &address, &token, pUser, nil, true); err != nil {
		return exitcode.Usagef("%v", err)
	}
	return a.saveConfig()
}
