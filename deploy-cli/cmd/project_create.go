package cmd

import (
	"fmt"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"deploy-cli/internal/api"
	"deploy-cli/internal/exitcode"
	"deploy-cli/internal/output"
)

var projectKeyRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

var (
	validShells       = map[string]bool{"": true, "auto": true, "bash": true, "sh": true, "pwsh": true, "cmd": true}
	validConcurrency  = map[string]bool{"": true, "queue": true, "allow": true, "cancelPrev": true}
	validFailStrategy = map[string]bool{"": true, "abort": true, "continue": true}
)

func newProjectCreateCmd(app *App) *cobra.Command {
	var (
		key              string
		name             string
		workdir          string
		description      string
		shell            string
		concurrency      string
		timeoutSec       int
		commandSpecs     []string
		envPairs         []string
		hook             bool
		allowEnvOverride bool
		useGit           bool
		gitURL           string
		gitBranch        string
		gitSubdir        string
		gitDepth         int
		gitClean         bool
		gitSubmodule     bool
		failStrategy     string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "创建项目（交互式向导或命令行参数）",
		Long: `创建项目。

非交互（CI）示例：
  deploy project create --key my-app --name 我的应用 --workdir /srv/workspaces/my-app \
      --command "git pull" --command "构建::npm run build" --env MODE=prod

--command 可重复；用 "名称::脚本" 的形式可以指定命令名称（省略则自动命名）。
不带参数运行时进入交互式向导。`,
		Args: exactArgs(0, "create 不接受位置参数"),
		RunE: func(cmd *cobra.Command, args []string) error {
			p := app.Printer
			interactive := isInteractive()

			// ---- key ----
			key = strings.TrimSpace(key)
			if key == "" {
				if !interactive {
					return exitcode.Usagef("非交互环境必须提供 --key")
				}
				var err error
				if key, err = promptLine("项目 key（字母/数字/-/_，用于 URL）", ""); err != nil {
					return err
				}
				key = strings.TrimSpace(key)
			}
			if !projectKeyRE.MatchString(key) {
				return exitcode.Usagef("项目 key 不合法：%q（需以字母或数字开头，只能含字母/数字/./-/_）", key)
			}

			// ---- name ----
			name = strings.TrimSpace(name)
			if name == "" {
				if interactive {
					var err error
					if name, err = promptLine("项目名称", key); err != nil {
						return err
					}
				} else {
					name = key
				}
			}

			// ---- workdir ----
			workdir = strings.TrimSpace(workdir)
			if workdir == "" {
				if !interactive {
					return exitcode.Usagef("非交互环境必须提供 --workdir（服务端上的绝对路径）")
				}
				var err error
				if workdir, err = promptLine("工作目录", defaultWorkdir(key)); err != nil {
					return err
				}
			}

			// ---- commands ----
			if len(commandSpecs) == 0 && interactive {
				p.Infof("逐条输入命令脚本，直接回车结束：")
				for i := 1; ; i++ {
					script, err := promptLine(fmt.Sprintf("命令 %d 脚本", i), "")
					if err != nil {
						return err
					}
					if strings.TrimSpace(script) == "" {
						break
					}
					cname, err := promptLine("  命令名称", fmt.Sprintf("步骤 %d", i))
					if err != nil {
						return err
					}
					commandSpecs = append(commandSpecs, cname+"::"+script)
				}
			}
			if len(commandSpecs) == 0 {
				return exitcode.Usagef("至少需要一条命令，请用 --command \"脚本\" 提供（可重复）")
			}

			// ---- 校验枚举值 ----
			if !validShells[shell] {
				return exitcode.Usagef("--shell 只能是 auto/bash/sh/pwsh/cmd，收到 %q", shell)
			}
			if !validConcurrency[concurrency] {
				return exitcode.Usagef("--concurrency 只能是 queue/allow/cancelPrev，收到 %q", concurrency)
			}
			if !validFailStrategy[failStrategy] {
				return exitcode.Usagef("--fail-strategy 只能是 abort/continue，收到 %q", failStrategy)
			}
			if timeoutSec < 0 {
				return exitcode.Usagef("--timeout 不能为负数")
			}
			if useGit && strings.TrimSpace(gitURL) == "" {
				return exitcode.Usagef("启用 --use-git 时必须提供 --git-url")
			}

			envMap, err := parseEnvPairs(envPairs)
			if err != nil {
				return err
			}

			cmds := make([]api.CommandInput, 0, len(commandSpecs))
			for i, spec := range commandSpecs {
				cname, script := splitCommandSpec(i+1, spec)
				if strings.TrimSpace(script) == "" {
					return exitcode.Usagef("第 %d 条命令的脚本为空", i+1)
				}
				cmds = append(cmds, api.CommandInput{
					Name:         cname,
					Script:       script,
					SortOrder:    i + 1,
					Enabled:      true,
					FailStrategy: orDefault(failStrategy, "abort"),
				})
			}
			envs := make([]api.EnvVarInput, 0, len(envMap))
			keys := make([]string, 0, len(envMap))
			for k := range envMap {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				envs = append(envs, api.EnvVarInput{Key: k, Value: envMap[k]})
			}

			in := &api.ProjectInput{
				Key:              key,
				Name:             name,
				Description:      description,
				Workdir:          workdir,
				Shell:            orDefault(shell, "auto"),
				TimeoutSec:       timeoutSec,
				Concurrency:      orDefault(concurrency, "queue"),
				HookEnabled:      hook,
				UseGit:           useGit,
				GitURL:           gitURL,
				GitBranch:        orDefault(gitBranch, "main"),
				GitSubdir:        gitSubdir,
				GitDepth:         gitDepth,
				GitClean:         gitClean,
				GitSubmodule:     gitSubmodule,
				AllowEnvOverride: allowEnvOverride,
				Commands:         cmds,
				Envs:             envs,
			}

			client, err := app.AuthClient()
			if err != nil {
				return err
			}
			ctx, cancel := app.ctx()
			defer cancel()

			proj, err := client.CreateProject(ctx, in)
			if err != nil {
				return err
			}
			return app.jsonOrHuman(proj, func() {
				p.Printf("已创建项目 %s（id=%d，命令 %d 条，环境变量 %d 个）\n",
					p.Bold(proj.Key), proj.ID, len(proj.Commands), len(proj.Envs))
				p.String(output.KeyValue([][2]string{
					{"工作目录", orDash(proj.Workdir)},
					{"Shell", orDash(proj.Shell)},
					{"并发策略", orDash(proj.Concurrency)},
				}))
				p.Infof("提示：执行 `deploy run %s` 触发一次运行", proj.Key)
			})
		},
	}

	f := cmd.Flags()
	f.StringVar(&key, "key", "", "项目 key（唯一，用于 URL，字母/数字/-/_）")
	f.StringVar(&name, "name", "", "项目名称（默认与 key 相同）")
	f.StringVar(&workdir, "workdir", "", "工作目录（服务端绝对路径）")
	f.StringVar(&description, "description", "", "描述")
	f.StringVar(&shell, "shell", "auto", "shell 类型：auto/bash/sh/pwsh/cmd")
	f.StringVar(&concurrency, "concurrency", "queue", "并发策略：queue/allow/cancelPrev")
	f.IntVar(&timeoutSec, "timeout", 0, "整体超时（秒，0=不限）")
	f.StringArrayVar(&commandSpecs, "command", nil, "命令脚本，可重复；\"名称::脚本\" 可指定名称")
	f.StringArrayVar(&envPairs, "env", nil, "环境变量 K=V，可重复")
	f.StringVar(&failStrategy, "fail-strategy", "abort", "命令失败策略：abort/continue")
	f.BoolVar(&hook, "hook", true, "允许 Hook 触发（--hook=false 关闭）")
	f.BoolVar(&allowEnvOverride, "allow-env-override", true, "允许触发时用 -e 覆盖环境变量")
	f.BoolVar(&useGit, "use-git", false, "启用 Git 准备阶段（需配合 --git-url）")
	f.StringVar(&gitURL, "git-url", "", "Git 仓库地址")
	f.StringVar(&gitBranch, "git-branch", "main", "Git 默认分支")
	f.StringVar(&gitSubdir, "git-subdir", "", "Git 子目录")
	f.IntVar(&gitDepth, "git-depth", 0, "Git 浅克隆深度（0=完整）")
	f.BoolVar(&gitClean, "git-clean", false, "拉取前清理工作区")
	f.BoolVar(&gitSubmodule, "git-submodule", false, "递归拉取子模块")

	return cmd
}

// splitCommandSpec 支持 "名称::脚本"；没有 "::" 时自动命名。
func splitCommandSpec(index int, spec string) (string, string) {
	if parts := strings.SplitN(spec, "::", 2); len(parts) == 2 && strings.TrimSpace(parts[0]) != "" {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return fmt.Sprintf("步骤 %d", index), strings.TrimSpace(spec)
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func defaultWorkdir(key string) string {
	if runtime.GOOS == "windows" {
		return `C:\deploy\workspaces\` + key
	}
	return "/srv/deploy/workspaces/" + key
}
