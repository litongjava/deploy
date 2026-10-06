// Package cmd 实现 deploy CLI 的全部 cobra 命令。
package cmd

import (
	"context"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"deploy-cli/internal/api"
	"deploy-cli/internal/config"
	"deploy-cli/internal/exitcode"
	"deploy-cli/internal/output"
	"deploy-cli/internal/version"
)

// App 持有全局 flag、配置与打印机，供各子命令共享。
type App struct {
	// 全局 flag
	AddressFlag string
	TokenFlag   string
	ServerFlag  string
	JSONFlag    bool
	NoColor     bool
	Verbose     bool

	Cfg      *config.Config
	Printer  *output.Printer
	Settings config.Settings

	// 输出流，测试可注入；nil 表示 os.Stdout / os.Stderr。
	out  io.Writer
	errw io.Writer

	client *api.Client
}

// NewRootCmd 构造命令树。
func NewRootCmd() *cobra.Command { return buildRoot(nil, nil) }

// NewRootCmdWithIO 构造命令树并注入输出流（供测试使用）。
func NewRootCmdWithIO(out, errw io.Writer) *cobra.Command { return buildRoot(out, errw) }

func buildRoot(out, errw io.Writer) *cobra.Command {
	app := &App{out: out, errw: errw}

	root := &cobra.Command{
		Use:   "deploy",
		Short: "command-line client for the lightweight imperative CI/CD platform",
		Long: `deploy is the command-line client for the CI/CD platform.

Servers (name + address + token) are stored in ~/.deploy/config.yaml
(Windows: %USERPROFILE%\.deploy\config.yaml).

A server's name IS its config key: ` + "`--server <name>`" + ` selects it, and
` + "`server update <old> --name <new>`" + ` renames it (current follows the new name).

Precedence: flags (-a/--address, --token) > env vars (DEPLOY_ADDRESS/DEPLOY_TOKEN) > config file (the server named by --server) > defaults

Exit codes: 0 success / 1 deploy failed / 2 usage error / 3 auth failure / 4 network or service unavailable`,
		Version:       version.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return app.init()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return exitcode.Usagef("unknown command %q; run `deploy --help` to see all commands", args[0])
			}
			return cmd.Help()
		},
	}
	root.SetVersionTemplate("deploy version {{.Version}}\n")
	root.Example = `  # sign in to a server (writes the token into the server named by --name; created if missing)
  deploy login -a http://192.168.31.97:10055 -u admin --name 97

  # list servers, then rename one (the name IS the config key; current follows)
  deploy server list
  deploy server update default --name 97

  # run a project against a specific server, without changing the default
  deploy --server 97 run my-app --wait --json`
	if out != nil {
		root.SetOut(out)
	}
	if errw != nil {
		root.SetErr(errw)
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return exitcode.Usagef("%v", err)
	})

	pf := root.PersistentFlags()
	addAddressFlag(pf, &app.AddressFlag, "server address (overrides config and env), e.g. http://127.0.0.1:10055")
	pf.StringVar(&app.TokenFlag, "token", "", "API token or JWT (overrides config and env)")
	pf.StringVar(&app.ServerFlag, "server", "", "use the configured server with this key (overrides the default), e.g. --server test")
	pf.BoolVar(&app.JSONFlag, "json", false, "output JSON; stdout carries machine-readable data only")
	pf.BoolVar(&app.NoColor, "no-color", false, "disable colored output (auto-disabled when not a TTY)")
	pf.BoolVar(&app.Verbose, "verbose", false, "write debug information to stderr")

	root.AddCommand(
		newLoginCmd(app),
		newLogoutCmd(app),
		newWhoamiCmd(app),
		newServerCmd(app),
		newProjectCmd(app),
		newRunCmd(app),
		newLogsCmd(app),
		newRunsCmd(app),
		newStopCmd(app),
		newDoctorCmd(app),
		newMiddlewareCmd(app),
		newConfigCmd(app),
		newVersionCmd(app),
	)
	return root
}

// init 载入配置、解析优先级、构造打印机。
func (a *App) init() error {
	path, err := config.DefaultPath()
	if err != nil {
		return exitcode.Wrap(exitcode.Usage, err, "cannot locate the config file")
	}
	cfg, err := config.Load(path)
	if err != nil {
		return exitcode.Wrap(exitcode.Usage, err, "")
	}
	a.Cfg = cfg
	a.Settings = config.Resolve(a.AddressFlag, a.TokenFlag, a.ServerFlag, os.Getenv, cfg)
	out, errw := a.out, a.errw
	if out == nil {
		out = os.Stdout
	}
	if errw == nil {
		errw = os.Stderr
	}
	a.Printer = output.New(out, errw, a.JSONFlag, a.NoColor, a.Verbose)
	a.Printer.Verbosef("config file: %s", path)
	a.Printer.Verbosef("server: %s (source: %s)", a.Settings.Server, a.Settings.ServerSource)
	a.Printer.Verbosef("address: %s (source: %s)", a.Settings.Address, a.Settings.AddressSource)
	a.Printer.Verbosef("token: %s (source: %s)", maskToken(a.Settings.Token), a.Settings.TokenSource)
	return nil
}

// Client 返回 API 客户端（不需要 token）。
func (a *App) Client() (*api.Client, error) {
	if a.client != nil {
		return a.client, nil
	}
	// `--server` 点名的服务器不在配置文件里：这不是「没配地址」，而是打错了键。
	// 分开报错，否则使用者会去翻配置文件找地址，而问题其实在 --server 上。
	if a.Settings.ServerMissing {
		return nil, exitcode.Usagef("server %q does not exist; run `deploy server list` to see configured servers, "+
			"or create it with `deploy server add %s -a <address> -t <token>`", a.Settings.Server, a.Settings.Server)
	}
	if strings.TrimSpace(a.Settings.Address) == "" {
		return nil, exitcode.Usagef("no server address configured; run `deploy login -a <URL>` or set DEPLOY_ADDRESS")
	}
	c := api.New(a.Settings.Address, a.Settings.Token)
	c.Verbose = a.Verbose
	if a.errw != nil {
		c.LogOut = a.errw
	} else {
		c.LogOut = os.Stderr
	}
	a.client = c
	return c, nil
}

// AuthClient 返回需要鉴权的客户端；未登录时给出退出码 3。
func (a *App) AuthClient() (*api.Client, error) {
	c, err := a.Client()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(c.Token) == "" {
		return nil, exitcode.Authf("not signed in: run `deploy login` first, or set DEPLOY_TOKEN / --token")
	}
	return c, nil
}

// ctx 返回一个带超时的 context。
func (a *App) ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 60*time.Second)
}

// saveConfig 保存配置并把失败归为用法/本地环境错误。
func (a *App) saveConfig() error {
	if err := a.Cfg.Save(); err != nil {
		return exitcode.Wrap(exitcode.Usage, err, "cannot save config")
	}
	return nil
}

// jsonOrHuman：--json 时输出 v，否则调用 human。
func (a *App) jsonOrHuman(v any, human func()) error {
	if a.Printer.JSON {
		return a.Printer.JSONOut(v)
	}
	human()
	return nil
}
