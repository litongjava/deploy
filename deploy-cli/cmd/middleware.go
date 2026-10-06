package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"deploy-cli/internal/api"
	"deploy-cli/internal/exitcode"
	"deploy-cli/internal/output"
)

// middleware 命令组：安装 / 启停 / 移除中间件（docker、postgresql、mysql、redis、elasticsearch）。
//
// 为什么做成 CLI 命令而不是只让人点网页：
// 需求是「AI 能操作 deploy-cli 进行部署和执行任务，并保持安装这些中间件」。
// 所以这里每个动作都要能**非交互**地一次跑完，并且 --json 给出可解析的结果：
//
//	deploy middleware install redis postgresql --dry-run
//	deploy middleware install redis --json          # 拿到 runId 后跟日志
//	deploy middleware start redis
//	deploy middleware remove elasticsearch --yes
//
// 安装本身是一次普通运行（Run），所以 `deploy logs <runId> -f` 能看实时日志，
// 退出码也沿用全局约定：0 成功 / 1 失败 / 2 用法 / 3 鉴权 / 4 网络。
func newMiddlewareCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "middleware",
		Short: "manage middleware: install, start, stop, remove (docker/postgresql/mysql/redis/elasticsearch)",
		Long: `Manage middleware. Install and start/stop are ordinary runs, so their logs can be followed.

  deploy middleware list                        list middleware and current state
  deploy middleware get redis                   show one middleware (including editable fields)
  deploy middleware install redis               install with the default mode
  deploy middleware install redis -m native     choose the install mode (docker / native)
  deploy middleware install --missing           install everything missing
  deploy middleware install redis --dry-run     dry run: print the plan without changing anything
  deploy middleware install redis -f            rebuild even if installed (data volumes are kept)
  deploy middleware start|stop|restart redis    start / stop / restart
  deploy middleware remove redis --yes          remove the container (**data volumes are kept**)
  deploy middleware logs redis                  show the last 100 log lines
  deploy middleware set redis -p REDIS_PORT=6380  save fields
  deploy middleware fields redis                list editable fields
  deploy middleware script > install.sh         dump the embedded install script

follow works like other commands: use the returned runId to follow logs
  deploy middleware install redis -f   # -f waits for completion and follows logs`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return exitcode.Usagef("unknown subcommand %q; available: list, get, install, start, stop, restart, remove, logs, set, fields, script", args[0])
			}
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		newMiddlewareListCmd(app),
		newMiddlewareGetCmd(app),
		newMiddlewareInstallCmd(app, middlewareActionInstall),
		newMiddlewareInstallCmd(app, middlewareActionStart),
		newMiddlewareInstallCmd(app, middlewareActionStop),
		newMiddlewareInstallCmd(app, middlewareActionRestart),
		newMiddlewareRemoveCmd(app),
		newMiddlewareLogsCmd(app),
		newMiddlewareSetCmd(app),
		newMiddlewareFieldsCmd(app),
		newMiddlewareScriptCmd(app),
	)
	return cmd
}

// 动作常量，与服务器 internal/middleware 的 Action* 一致。
const (
	middlewareActionInstall = "install"
	middlewareActionStart   = "start"
	middlewareActionStop    = "stop"
	middlewareActionRestart = "restart"
	middlewareActionRemove  = "remove"
	middlewareActionLogs    = "logs"
)

func middlewareActionLabel(action string) string {
	switch action {
	case middlewareActionInstall:
		return "install"
	case middlewareActionStart:
		return "start"
	case middlewareActionStop:
		return "stop"
	case middlewareActionRestart:
		return "restart"
	case middlewareActionRemove:
		return "remove"
	case middlewareActionLogs:
		return "logs"
	default:
		return action
	}
}

func newMiddlewareListCmd(app *App) *cobra.Command {
	var grouped bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "list all middleware and their status",
		Args:  exactArgs(0, "list takes no positional arguments"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := app.AuthClient()
			if err != nil {
				return err
			}
			ctx, cancel := app.ctx()
			defer cancel()
			list, err := client.Middleware(ctx)
			if err != nil {
				return err
			}
			p := app.Printer
			return app.jsonOrHuman(list, func() {
				if !list.Supported {
					p.Warnf("the server platform is %s: middleware install/start/stop is only available on Linux; here you can only dry-run", list.Platform)
				}
				if !list.DockerAvailable {
					p.Warnf("no usable docker on the server (%s): the container mode cannot run; install docker first or use the native mode",
						orDash(list.DockerVersion))
				}
				rows := make([][]string, 0, len(list.Components))
				for _, c := range list.Components {
					rows = append(rows, []string{
						c.Key,
						c.Name,
						c.EffectiveMode,
						installedText(c.Installed),
						runningText(c.Running),
						orDash(firstLine(c.Version)),
						orDash(c.Path),
					})
				}
				p.String(output.Table([]string{"KEY", "name", "mode", "installed", "running", "version", "location"}, rows))
				p.Println()
				if len(list.Missing) == 0 {
					p.Println(p.Green("all middleware is already installed"))
				} else {
					p.Printf("missing %d item(s): %s\n", len(list.Missing), strings.Join(list.Missing, ", "))
					p.Printf("install (dry-run first to see the plan): deploy middleware install %s --dry-run\n", strings.Join(list.Missing, " "))
				}
				if list.Hint != "" {
					p.Println(p.Dim(list.Hint))
				}
			})
		},
	}
	cmd.Flags().BoolVar(&grouped, "grouped", false, "reserved: group by category")
	return cmd
}

func newMiddlewareGetCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "show one middleware's status and fields",
		Args:  exactArgs(1, "usage: deploy middleware get <key>"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := app.AuthClient()
			if err != nil {
				return err
			}
			ctx, cancel := app.ctx()
			defer cancel()
			list, err := client.Middleware(ctx)
			if err != nil {
				return err
			}
			comp, ok := findMiddleware(list, args[0])
			if !ok {
				return exitcode.Usagef("unknown middleware %q; available: %s", args[0], strings.Join(middlewareKeys(list), ", "))
			}
			p := app.Printer
			return app.jsonOrHuman(comp, func() {
				p.String(output.KeyValue([][2]string{
					{"KEY", comp.Key},
					{"name", comp.Name},
					{"note", orDash(comp.Note)},
					{"installed", boolText(comp.Installed)},
					{"running", boolText(comp.Running)},
					{"install mode", fmt.Sprintf("%s (effective: %s, default: %s, available: %s)",
						orDash(comp.Mode), orDash(comp.EffectiveMode), orDash(comp.DefaultMode), strings.Join(comp.Modes, "/"))},
					{"version", orDash(firstLine(comp.Version))},
					{"location", orDash(comp.Path)},
					{"detail", orDash(comp.Detail)},
				}))
				p.Println()
				p.Println(p.Bold("editable fields"))
				if len(comp.Fields) == 0 {
					p.Println("  (this middleware has no editable fields)")
					return
				}
				rows := make([][]string, 0, len(comp.Fields))
				for _, f := range comp.Fields {
					value := f.Value
					if f.Secret {
						if f.Set {
							value = "set (not shown)"
						} else {
							value = "not set"
						}
					} else if value == "" {
						value = "(using the default)"
					}
					rows = append(rows, []string{f.Key, f.Label, value, f.Default})
				}
				p.String(output.Table([]string{"field", "meaning", "current value", "default value"}, rows))
			})
		},
	}
}

// newMiddlewareInstallCmd 同时用于 install/start/stop/restart：
// 四个动作的流程完全一样（触发一次运行 → 可选跟随日志），只是 action 不同。
func newMiddlewareInstallCmd(app *App, action string) *cobra.Command {
	var (
		mode     string
		missing  bool
		dryRun   bool
		force    bool
		follow   bool
		noFollow bool
		all      bool
	)
	cmd := &cobra.Command{
		Use:   action + " [key...]",
		Short: middlewareActionLabel(action) + "middleware",
		Long: fmt.Sprintf(`%s middleware.

  deploy middleware %s redis                one middleware
  deploy middleware %s redis postgresql     several middleware
  deploy middleware %s --missing            everything missing (install only)
  deploy middleware %s redis -m native      choose the install mode (docker / native)
  deploy middleware %s redis --dry-run      dry run (install only)
  deploy middleware %s redis -f             follow logs until the run finishes`,
			middlewareActionLabel(action), action, action, action, action, action, action),
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			keys := normalizeKeys(args)
			// 空列表 + install + --missing = 装全部缺失的（服务端也支持空列表语义，
			// 但显式要求 --missing 更不容易误触发）。
			if len(keys) == 0 && !missing {
				if action != middlewareActionInstall || !all {
					return exitcode.Usagef("specify at least one middleware, or use --missing (install) for everything missing")
				}
			}
			client, err := app.AuthClient()
			if err != nil {
				return err
			}
			ctx, cancel := app.ctx()
			defer cancel()

			// 服务端在非 Linux 上会拒绝真安装，但它会**先**返回 400 而不是等日志。
			// 这里不预先判断平台：让服务端做唯一裁决，避免两处判断口径不一致。
			res, err := client.InstallMiddleware(ctx, &api.MiddlewareInstallRequest{
				Components: keys,
				Mode:       mode,
				Action:     action,
				DryRun:     dryRun,
				Force:      force,
			})
			if err != nil {
				return err
			}
			p := app.Printer

			if res.NothingToDo {
				return app.jsonOrHuman(res, func() {
					p.Println(orDash(res.Message))
					p.Println("(all middleware is installed; pass -f/--force to rebuild)")
				})
			}

			app.Printer.Infof("%s%s: %s", middlewareActionLabel(action),
				map[bool]string{true: "(dry run)", false: ""}[dryRun],
				strings.Join(res.Components, ", "))
			app.Printer.Infof("triggered run %s (status %s)", runHead(res.RunID, res.BuildNumber), res.Status)
			if !followNow(follow, noFollow) {
				return app.jsonOrHuman(res, func() {
					p.Printf("run ID #%d (build #%d)  %s\n", res.RunID, res.BuildNumber, p.Status(res.Status))
					p.Printf("follow logs: deploy logs %d -f\n", res.RunID)
				})
			}
			return app.followTriggeredRun(client, res.RunID, res.BuildNumber)
		},
	}
	cmd.Flags().StringVarP(&mode, "mode", "m", "", "install mode: docker (default) or native")
	cmd.Flags().BoolVar(&missing, "missing", false, "process all missing middleware (install only)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "dry run: print what would happen without changing anything (install only)")
	cmd.Flags().BoolVarP(&force, "force", "F", false, "rebuild even if installed (container mode keeps data volumes)")
	cmd.Flags().BoolVar(&all, "all", false, "process all middleware (same as --missing, install only)")
	// follow 默认 **开启**：装中间件是长任务，默认就把人/脚本带到日志上更符合直觉。
	// 同时提供 --no-follow（脚本/AI 用），否则「触发后就返回」这件事没法从命令行表达。
	cmd.Flags().BoolVar(&follow, "follow", true, "follow logs until the run finishes")
	cmd.Flags().BoolVar(&noFollow, "no-follow", false, "trigger only, do not follow logs (for scripts/AI)")
	return cmd
}

func newMiddlewareRemoveCmd(app *App) *cobra.Command {
	var (
		yes      bool
		force    bool
		follow   bool
		noFollow bool
	)
	cmd := &cobra.Command{
		Use:   "remove <key...>",
		Short: "remove middleware (only containers/packages; data volumes and directories are kept)",
		Long: `Remove middleware.

**No data is deleted**: container mode only removes the container; the named volume (deploy-mw-<key>-data) is kept as is;
native mode does not uninstall system packages and only prints the manual command. To wipe data completely, run:
  docker volume rm deploy-mw-<key>-data`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			keys := normalizeKeys(args)
			if !yes {
				if !isInteractive() {
					return exitcode.Usagef("non-interactive shell: pass --yes to confirm: deploy middleware remove %s --yes", strings.Join(keys, " "))
				}
				ans, err := promptLine(fmt.Sprintf("remove %s? (data volumes are kept) type yes to continue", strings.Join(keys, " ")), "")
				if err != nil {
					return err
				}
				if !strings.EqualFold(ans, "yes") && !strings.EqualFold(ans, "y") {
					app.Printer.Println("cancelled")
					return nil
				}
			}
			client, err := app.AuthClient()
			if err != nil {
				return err
			}
			ctx, cancel := app.ctx()
			defer cancel()
			res, err := client.InstallMiddleware(ctx, &api.MiddlewareInstallRequest{
				Components: keys, Action: middlewareActionRemove, Force: force,
			})
			if err != nil {
				return err
			}
			app.Printer.Infof("remove: %s", strings.Join(keys, ", "))
			if !followNow(follow, noFollow) {
				return app.jsonOrHuman(res, func() {
					app.Printer.Printf("run ID #%d  %s\n", res.RunID, res.Status)
					app.Printer.Printf("follow logs: deploy logs %d -f\n", res.RunID)
				})
			}
			return app.followTriggeredRun(client, res.RunID, res.BuildNumber)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip interactive confirmation")
	cmd.Flags().BoolVar(&force, "force", false, "treat a missing container as success")
	cmd.Flags().BoolVar(&follow, "follow", true, "follow logs until the run finishes")
	cmd.Flags().BoolVar(&noFollow, "no-follow", false, "trigger only, do not follow logs (for scripts/AI)")
	return cmd
}

func newMiddlewareLogsCmd(app *App) *cobra.Command {
	var (
		follow   bool
		noFollow bool
	)
	cmd := &cobra.Command{
		Use:   "logs <key...>",
		Short: "show recent middleware logs (container logs in docker mode, systemd logs in native mode)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			keys := normalizeKeys(args)
			client, err := app.AuthClient()
			if err != nil {
				return err
			}
			ctx, cancel := app.ctx()
			defer cancel()
			res, err := client.InstallMiddleware(ctx, &api.MiddlewareInstallRequest{
				Components: keys, Action: middlewareActionLogs,
			})
			if err != nil {
				return err
			}
			app.Printer.Infof("logs: %s (run %s)", strings.Join(keys, ", "), runHead(res.RunID, res.BuildNumber))
			if !followNow(follow, noFollow) {
				return app.jsonOrHuman(res, func() {
					app.Printer.Printf("run ID #%d  %s\n", res.RunID, res.Status)
					app.Printer.Printf("follow logs: deploy logs %d\n", res.RunID)
				})
			}
			return app.followTriggeredRun(client, res.RunID, res.BuildNumber)
		},
	}
	cmd.Flags().BoolVar(&follow, "follow", true, "follow logs until the run finishes")
	cmd.Flags().BoolVar(&noFollow, "no-follow", false, "trigger only, do not follow logs (for scripts/AI)")
	return cmd
}

func newMiddlewareSetCmd(app *App) *cobra.Command {
	var (
		pairs   []string
		mode    string
		confirm bool
	)
	cmd := &cobra.Command{
		Use:   "set <key>",
		Short: "save middleware install fields (port / password / version / data dir)",
		Long: `Save middleware install fields; they take effect on the next install/rebuild.

  deploy middleware set redis -p REDIS_PORT=6380 -p REDIS_PASSWORD=secret
  deploy middleware set postgresql -p PG_VERSION=17 --mode native
  deploy middleware set redis -p REDIS_PASSWORD=          # empty string = clear the password (back to a generated one)

Password fields (PG_PASSWORD / MYSQL_ROOT_PASSWORD / REDIS_PASSWORD) are stored encrypted on the server;
the API never returns them in clear text and only reports "set".`,
		Args: exactArgs(1, "usage: deploy middleware set <key> -p KEY=VALUE [--mode docker|native]"),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := strings.TrimSpace(args[0])
			fields, err := parseMiddlewarePairs(pairs)
			if err != nil {
				return err
			}
			if len(fields) == 0 && mode == "" {
				return exitcode.Usagef("pass at least one -p KEY=VALUE or --mode")
			}
			if confirm && !isInteractive() {
				return exitcode.Usagef("non-interactive shell: do not pass --confirm")
			}
			client, err := app.AuthClient()
			if err != nil {
				return err
			}
			ctx, cancel := app.ctx()
			defer cancel()
			res, err := client.UpdateMiddleware(ctx, key, fields, mode)
			if err != nil {
				return err
			}
			p := app.Printer
			return app.jsonOrHuman(res, func() {
				p.Printf("saved fields for %s\n", p.Bold(key))
				if mode != "" {
					p.Printf("install mode: %s\n", mode)
				}
				for k, v := range fields {
					if isSecretField(k) && v != "" {
						v = "(stored encrypted)"
					}
					p.Printf("  %s = %s\n", k, v)
				}
			})
		},
	}
	cmd.Flags().StringArrayVarP(&pairs, "param", "p", nil, "field in the form KEY=VALUE; repeatable")
	cmd.Flags().StringVar(&mode, "mode", "", "install mode: docker or native")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "reserved: used when a second confirmation is needed")
	return cmd
}

func newMiddlewareFieldsCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "fields [key]",
		Short: "list a middleware's editable fields (no key lists all)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := app.AuthClient()
			if err != nil {
				return err
			}
			ctx, cancel := app.ctx()
			defer cancel()
			list, err := client.Middleware(ctx)
			if err != nil {
				return err
			}
			p := app.Printer

			type fieldView struct {
				Component string `json:"component"`
				Key       string `json:"key"`
				Label     string `json:"label"`
				Default   string `json:"default"`
				Value     string `json:"value"`
				Secret    bool   `json:"secret"`
				Set       bool   `json:"set"`
				Help      string `json:"help"`
			}
			views := make([]fieldView, 0, 16)
			for _, c := range list.Components {
				if len(args) == 1 && c.Key != strings.TrimSpace(args[0]) {
					continue
				}
				for _, f := range c.Fields {
					views = append(views, fieldView{
						Component: c.Key, Key: f.Key, Label: f.Label, Default: f.Default,
						Value: f.Value, Secret: f.Secret, Set: f.Set, Help: f.Help,
					})
				}
			}
			if len(args) == 1 && len(views) == 0 {
				return exitcode.Usagef("unknown middleware %q; available: %s", args[0], strings.Join(middlewareKeys(list), ", "))
			}
			return app.jsonOrHuman(map[string]any{"fields": views}, func() {
				rows := make([][]string, 0, len(views))
				for _, v := range views {
					value := v.Value
					if v.Secret {
						value = map[bool]string{true: "set", false: "not set"}[v.Set]
					} else if value == "" {
						value = "(using the default)"
					}
					rows = append(rows, []string{v.Component, v.Key, v.Label, value, v.Default})
				}
				p.String(output.Table([]string{"middleware", "field", "meaning", "current value", "default value"}, rows))
			})
		},
	}
}

func newMiddlewareScriptCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "script",
		Short: "print the embedded middleware install script (redirect to a file to run it manually)",
		Args:  exactArgs(0, "script takes no positional arguments"),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := app.AuthClient()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			body, err := client.MiddlewareScript(ctx)
			if err != nil {
				return err
			}
			if app.Printer.JSON {
				return app.Printer.JSONOut(map[string]string{"script": body})
			}
			app.Printer.Printf("%s", body)
			return nil
		},
	}
}

// followNow 组合 --follow 与 --no-follow：后者是一键关闭（脚本里写 `--no-follow` 比
// `--follow=false` 更直观），两者同时给时以 --no-follow 为准。
func followNow(follow, noFollow bool) bool { return follow && !noFollow }

// followTriggeredRun 跟随一次「已触发」的运行直到结束。
//
// 单独抽出来是因为 middleware 的四个动作（install/start/stop/restart/remove/logs）
// 都要走同一段：装好信号处理 → followRun → 按退出码返回。
// 中间件安装往往会跑几分钟（拉镜像），Ctrl+C 应只断开跟随、不能取消安装，
// 所以 cancelOnInterrupt 固定为 false —— 与 `deploy logs -f` 的语义一致。
func (a *App) followTriggeredRun(client *api.Client, runID int64, buildNumber int) error {
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return a.followRun(sigCtx, client, runID, buildNumber, false)
}

// ---------- 小工具 ----------

// normalizeKeys 去掉空串与重复项，保持输入顺序（顺序决定安装顺序，docker 必须在前）。
func normalizeKeys(args []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(args))
	for _, a := range args {
		for _, part := range strings.Split(a, ",") {
			part = strings.TrimSpace(part)
			if part == "" || seen[part] {
				continue
			}
			seen[part] = true
			out = append(out, part)
		}
	}
	return out
}

func findMiddleware(list *api.MiddlewareList, key string) (api.MiddlewareComponent, bool) {
	key = strings.TrimSpace(key)
	for _, c := range list.Components {
		if c.Key == key {
			return c, true
		}
	}
	return api.MiddlewareComponent{}, false
}

func middlewareKeys(list *api.MiddlewareList) []string {
	out := make([]string, 0, len(list.Components))
	for _, c := range list.Components {
		out = append(out, c.Key)
	}
	return out
}

func installedText(ok bool) string {
	if ok {
		return "✔"
	}
	return "✘"
}

func runningText(ok bool) string {
	if ok {
		return "running"
	}
	return "-"
}

// firstLine 取多行文本的第一行（版本字符串有时是多行的，例如 java -version）。
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// parseMiddlewarePairs 解析 -p KEY=VALUE；KEY 不能为空，允许 VALUE 为空串（= 清空该参数）。
func parseMiddlewarePairs(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		i := strings.Index(p, "=")
		if i <= 0 {
			return nil, exitcode.Usagef("fields must be KEY=VALUE, got %q", p)
		}
		k := strings.TrimSpace(p[:i])
		if k == "" {
			return nil, exitcode.Usagef("field name must not be empty: %q", p)
		}
		out[k] = p[i+1:]
	}
	return out, nil
}

// isSecretField 判断是否是密码类参数（仅用于提示文案：值不回显）。
func isSecretField(key string) bool {
	return strings.Contains(key, "PASSWORD")
}
