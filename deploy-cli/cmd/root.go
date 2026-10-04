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
	ServerFlag string
	TokenFlag  string
	JSONFlag   bool
	NoColor    bool
	Verbose    bool

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
		Short: "轻量命令式 CI/CD 平台的命令行客户端",
		Long: `deploy 是 CI/CD 平台的命令行客户端。

配置文件：~/.deploy/config.yaml（Windows: %USERPROFILE%\.deploy\config.yaml）
配置优先级：命令行 flag > 环境变量(DEPLOY_SERVER/DEPLOY_TOKEN) > 配置文件 > 默认值

退出码：0 成功 / 1 部署失败 / 2 用法错误 / 3 鉴权失败 / 4 网络或服务不可用`,
		Version:       version.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return app.init()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return exitcode.Usagef("未知命令 %q，执行 `deploy --help` 查看全部命令", args[0])
			}
			return cmd.Help()
		},
	}
	root.SetVersionTemplate("deploy version {{.Version}}\n")
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
	pf.StringVar(&app.ServerFlag, "server", "", "服务端地址（覆盖配置与环境变量），如 http://127.0.0.1:10055")
	pf.StringVar(&app.TokenFlag, "token", "", "API Token 或 JWT（覆盖配置与环境变量）")
	pf.BoolVar(&app.JSONFlag, "json", false, "以 JSON 输出结果，stdout 只输出机器可读内容")
	pf.BoolVar(&app.NoColor, "no-color", false, "禁用彩色输出（非 TTY 自动禁用）")
	pf.BoolVar(&app.Verbose, "verbose", false, "输出调试信息到 stderr")

	root.AddCommand(
		newLoginCmd(app),
		newLogoutCmd(app),
		newWhoamiCmd(app),
		newProjectCmd(app),
		newRunCmd(app),
		newLogsCmd(app),
		newRunsCmd(app),
		newStopCmd(app),
		newDoctorCmd(app),
		newConfigCmd(app),
		newVersionCmd(app),
	)
	return root
}

// init 载入配置、解析优先级、构造打印机。
func (a *App) init() error {
	path, err := config.DefaultPath()
	if err != nil {
		return exitcode.Wrap(exitcode.Usage, err, "定位配置文件失败")
	}
	cfg, err := config.Load(path)
	if err != nil {
		return exitcode.Wrap(exitcode.Usage, err, "")
	}
	a.Cfg = cfg
	a.Settings = config.Resolve(a.ServerFlag, a.TokenFlag, os.Getenv, cfg)
	out, errw := a.out, a.errw
	if out == nil {
		out = os.Stdout
	}
	if errw == nil {
		errw = os.Stderr
	}
	a.Printer = output.New(out, errw, a.JSONFlag, a.NoColor, a.Verbose)
	a.Printer.Verbosef("配置文件: %s", path)
	a.Printer.Verbosef("服务端: %s（来源: %s）", a.Settings.Server, a.Settings.ServerSource)
	a.Printer.Verbosef("token: %s（来源: %s）", maskToken(a.Settings.Token), a.Settings.TokenSource)
	return nil
}

// Client 返回 API 客户端（不需要 token）。
func (a *App) Client() (*api.Client, error) {
	if a.client != nil {
		return a.client, nil
	}
	if strings.TrimSpace(a.Settings.Server) == "" {
		return nil, exitcode.Usagef("未配置服务端地址，请执行 `deploy login -s <URL>` 或设置 DEPLOY_SERVER")
	}
	c := api.New(a.Settings.Server, a.Settings.Token)
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
		return nil, exitcode.Authf("未登录：请先执行 `deploy login`，或设置 DEPLOY_TOKEN / --token")
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
		return exitcode.Wrap(exitcode.Usage, err, "保存配置失败")
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
