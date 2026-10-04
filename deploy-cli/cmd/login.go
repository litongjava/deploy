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
		server      string
		username    string
		password    string
		token       string
		contextName string
	)
	cmd := &cobra.Command{
		Use:   "login",
		Short: "登录并保存 token（也支持 --token 直接写入）",
		Long: `登录到服务端并保存 token。

不带 -p 时交互式输入密码，输入不回显；
--token 可直接写入 API Token / JWT，跳过用户名密码登录。`,
		Args: exactArgs(0, "login 不接受位置参数"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogin(app, server, username, password, token, contextName)
		},
	}
	cmd.Flags().StringVarP(&server, "server", "s", "", "服务端地址，如 http://127.0.0.1:8080")
	cmd.Flags().StringVarP(&username, "username", "u", "", "用户名（默认 admin）")
	cmd.Flags().StringVarP(&password, "password", "p", "", "密码；不传则交互式输入（不回显）")
	cmd.Flags().StringVarP(&token, "token", "t", "", "直接写入 API Token / JWT，跳过登录")
	cmd.Flags().StringVar(&contextName, "context", "", "写入指定 context（默认当前 context）")
	return cmd
}

func runLogin(app *App, server, username, password, token, contextName string) error {
	p := app.Printer

	if contextName != "" {
		if err := app.Cfg.UseContext(contextName); err != nil {
			return exitcode.Usagef("%v", err)
		}
	}
	target := app.Settings.Server
	if strings.TrimSpace(server) != "" {
		target = config.NormalizeServer(server)
	}
	if strings.TrimSpace(target) == "" {
		return exitcode.Usagef("请提供服务端地址：deploy login -s http://127.0.0.1:8080")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if t := strings.TrimSpace(token); t != "" {
		return loginWithToken(app, ctx, target, t)
	}

	if strings.TrimSpace(username) == "" {
		username = app.Settings.User
	}
	if strings.TrimSpace(username) == "" {
		if !isInteractive() {
			return exitcode.Usagef("非交互环境请用 -u/--username 指定用户名，或用 --token 直接写入 token")
		}
		var err error
		username, err = promptLine("用户名", "admin")
		if err != nil {
			return err
		}
	}
	if strings.TrimSpace(password) == "" {
		if !isInteractive() {
			return exitcode.Usagef("非交互环境请用 -p/--password 提供密码，或用 --token 直接写入 token")
		}
		var err error
		password, err = readPassword("密码（输入不回显）: ")
		if err != nil {
			return err
		}
	}

	client := api.New(target, "")
	res, err := client.Login(ctx, username, password)
	if err != nil {
		var e *api.Error
		if errors.As(err, &e) && e.IsAuth() {
			return exitcode.Authf("登录失败：%s", e.Message)
		}
		return err
	}
	if res.Token == "" {
		return exitcode.Netf("服务端未返回 token")
	}

	name := res.User.Username
	if name == "" {
		name = username
	}
	aCtx := app.Cfg.Active()
	aCtx.Server = target
	aCtx.Token = res.Token
	aCtx.User = name
	if err := app.saveConfig(); err != nil {
		return err
	}

	if res.MustChangePassword {
		p.Warnf("该账号需要修改密码，请登录 Web 端或调用 /auth/change-password 修改")
	}
	return app.jsonOrHuman(map[string]any{
		"server":    target,
		"context":   app.Cfg.Current,
		"username":  name,
		"role":      res.User.Role,
		"token":     maskToken(res.Token),
		"expiresAt": res.ExpiresAt,
	}, func() {
		p.Printf("登录成功：%s @ %s\n", p.Bold(name), target)
		p.Printf("token 已保存到 %s（context: %s）\n", app.Cfg.Path(), app.Cfg.Current)
		if res.ExpiresAt > 0 {
			p.Printf("有效期至 %s\n", time.Unix(res.ExpiresAt, 0).Local().Format("2006-01-02 15:04:05"))
		}
	})
}

// loginWithToken 直接写入 token；能连上服务端时顺便校验并补全用户名。
func loginWithToken(app *App, ctx context.Context, target, token string) error {
	p := app.Printer
	username := ""

	client := api.New(target, token)
	if u, err := client.Me(ctx); err == nil {
		username = u.Username
		p.Verbosef("token 校验通过：%s", username)
	} else {
		var e *api.Error
		if errors.As(err, &e) && e.IsAuth() {
			return exitcode.Authf("Token 无效或已过期：%s", e.Message)
		}
		p.Warnf("无法校验 token（%v），仍按原样写入配置", err)
	}
	if username == "" {
		username = app.Settings.User
	}

	aCtx := app.Cfg.Active()
	aCtx.Server = target
	aCtx.Token = token
	if username != "" {
		aCtx.User = username
	}
	if err := app.saveConfig(); err != nil {
		return err
	}
	return app.jsonOrHuman(map[string]any{
		"server":   target,
		"context":  app.Cfg.Current,
		"username": username,
		"token":    maskToken(token),
	}, func() {
		if username != "" {
			p.Printf("已保存 token（用户 %s @ %s，context: %s）\n", p.Bold(username), target, app.Cfg.Current)
		} else {
			p.Printf("已保存 token（%s，context: %s）\n", target, app.Cfg.Current)
		}
	})
}
