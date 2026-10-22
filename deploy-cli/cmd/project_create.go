package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
		kind             string
		dependencies     []string
		filePath         string
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
		Short: "create a project (interactive wizard or flags)",
		Long: `Create a project.

Non-interactive (CI) example:
  deploy project create --key my-app --name my-app --workdir /srv/workspaces/my-app \
      --command "git pull" --command "build::npm run build" --env MODE=prod

--command is repeatable; use "name::script" to name a command (otherwise it is auto-named).

--file reads a JSON project definition. This is the easy path on Windows, where
$(cat sh\01.sh) does not work, and it keeps the definition in version control.
Explicit flags override the file. Each command sets either "script" (inline) or
"scriptFile" (a path relative to the JSON file):

  {
    "key": "tio-boot-web-hello",
    "name": "tio-boot web hello",
    "workdir": "/srv/deploy/workspaces/tio-boot-web-hello",
    "useGit": true,
    "gitUrl": "https://gitee.com/ppnt/tio-boot-web-hello.git",
    "gitBranch": "main",
    "env": {"APP_PORT": "10062"},
    "commands": [
      {"name": "1. 环境勘察（只读）", "scriptFile": "sh/01-recon.sh"},
      {"name": "2. Maven 构建生产包", "scriptFile": "sh/02-build.sh"}
    ]
  }

Running without flags or --file starts the interactive wizard.`,
		Args: exactArgs(0, "create takes no positional arguments"),
		RunE: func(cmd *cobra.Command, args []string) error {
			p := app.Printer
			interactive := isInteractive()

			// ---- --file：JSON 项目定义（显式 flag 覆盖文件里的值）----
			var fileCmds []api.CommandInput
			if strings.TrimSpace(filePath) != "" {
				pf, baseDir, err := loadProjectFile(filePath)
				if err != nil {
					return err
				}
				if !cmd.Flags().Changed("kind") && pf.Kind != "" {
					kind = pf.Kind
				}
				if !cmd.Flags().Changed("dependencies") && pf.Dependencies != nil {
					dependencies = pf.Dependencies
				}
				if !cmd.Flags().Changed("key") && pf.Key != "" {
					key = pf.Key
				}
				if !cmd.Flags().Changed("name") && pf.Name != "" {
					name = pf.Name
				}
				if !cmd.Flags().Changed("description") && pf.Description != "" {
					description = pf.Description
				}
				if !cmd.Flags().Changed("workdir") && pf.Workdir != "" {
					workdir = pf.Workdir
				}
				if !cmd.Flags().Changed("shell") && pf.Shell != "" {
					shell = pf.Shell
				}
				if !cmd.Flags().Changed("timeout") && pf.TimeoutSec != nil {
					timeoutSec = *pf.TimeoutSec
				}
				if !cmd.Flags().Changed("concurrency") && pf.Concurrency != "" {
					concurrency = pf.Concurrency
				}
				if !cmd.Flags().Changed("hook") && pf.HookEnabled != nil {
					hook = *pf.HookEnabled
				}
				if !cmd.Flags().Changed("use-git") && pf.UseGit != nil {
					useGit = *pf.UseGit
				}
				if !cmd.Flags().Changed("git-url") && pf.GitURL != "" {
					gitURL = pf.GitURL
				}
				if !cmd.Flags().Changed("git-branch") && pf.GitBranch != "" {
					gitBranch = pf.GitBranch
				}
				if !cmd.Flags().Changed("git-subdir") && pf.GitSubdir != "" {
					gitSubdir = pf.GitSubdir
				}
				if !cmd.Flags().Changed("git-depth") && pf.GitDepth != nil {
					gitDepth = *pf.GitDepth
				}
				if !cmd.Flags().Changed("git-clean") && pf.GitClean != nil {
					gitClean = *pf.GitClean
				}
				if !cmd.Flags().Changed("git-submodule") && pf.GitSubmodule != nil {
					gitSubmodule = *pf.GitSubmodule
				}
				if !cmd.Flags().Changed("allow-env-override") && pf.AllowEnvOverride != nil {
					allowEnvOverride = *pf.AllowEnvOverride
				}
				if len(commandSpecs) == 0 {
					if fileCmds, err = pf.commandInputs(baseDir, orDefault(failStrategy, "abort")); err != nil {
						return err
					}
				}
				if len(envPairs) == 0 {
					envPairs = pf.envPairs()
				}
			}

			// ---- key ----
			key = strings.TrimSpace(key)
			if key == "" {
				if !interactive {
					return exitcode.Usagef("non-interactive shell: --key is required")
				}
				var err error
				if key, err = promptLine("project key (letters/digits/-/_; used in the URL)", ""); err != nil {
					return err
				}
				key = strings.TrimSpace(key)
			}
			if !projectKeyRE.MatchString(key) {
				return exitcode.Usagef("invalid project key %q (must start with a letter or digit; only letters/digits/./-/_ allowed)", key)
			}

			// ---- name ----
			name = strings.TrimSpace(name)
			if name == "" {
				if interactive {
					var err error
					if name, err = promptLine("project name", key); err != nil {
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
					return exitcode.Usagef("non-interactive shell: --workdir is required (absolute path on the server)")
				}
				var err error
				if workdir, err = promptLine("workdir", defaultWorkdir(key)); err != nil {
					return err
				}
			}

			// ---- commands ----
			if len(commandSpecs) == 0 && len(fileCmds) == 0 && interactive {
				p.Infof("enter command scripts one by one; press Enter to finish:")
				for i := 1; ; i++ {
					script, err := promptLine(fmt.Sprintf("command %d script", i), "")
					if err != nil {
						return err
					}
					if strings.TrimSpace(script) == "" {
						break
					}
					cname, err := promptLine("  command name", fmt.Sprintf("step %d", i))
					if err != nil {
						return err
					}
					commandSpecs = append(commandSpecs, cname+"::"+script)
				}
			}
			if len(commandSpecs) == 0 && len(fileCmds) == 0 {
				return exitcode.Usagef("at least one command is required; pass --command script (repeatable) or use --file with commands")
			}

			if err := validateProjectDependencies(key, kind, dependencies); err != nil {
				return err
			}
			// ---- 校验枚举值 ----
			if !validShells[shell] {
				return exitcode.Usagef("--shell must be auto/bash/sh/pwsh/cmd, got %q", shell)
			}
			if !validConcurrency[concurrency] {
				return exitcode.Usagef("--concurrency must be queue/allow/cancelPrev, got %q", concurrency)
			}
			if !validFailStrategy[failStrategy] {
				return exitcode.Usagef("--fail-strategy must be abort/continue, got %q", failStrategy)
			}
			if timeoutSec < 0 {
				return exitcode.Usagef("--timeout must not be negative")
			}
			if useGit && strings.TrimSpace(gitURL) == "" {
				return exitcode.Usagef("--git-url is required when --use-git is set")
			}

			envMap, err := parseEnvPairs(envPairs)
			if err != nil {
				return err
			}

			cmds := fileCmds
			if len(cmds) == 0 {
				cmds = make([]api.CommandInput, 0, len(commandSpecs))
			}
			for i, spec := range commandSpecs {
				cname, script := splitCommandSpec(i+1, spec)
				if strings.TrimSpace(script) == "" {
					return exitcode.Usagef("command %d has an empty script", i+1)
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
				Kind: kind, Dependencies: dependencies,
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
				p.Printf("created project %s (id=%d, %d commands, %d env vars)\n",
					p.Bold(proj.Key), proj.ID, len(proj.Commands), len(proj.Envs))
				p.String(output.KeyValue([][2]string{
					{"workdir", orDash(proj.Workdir)},
					{"Shell", orDash(proj.Shell)},
					{"concurrency", orDash(proj.Concurrency)},
				}))
				p.Infof("hint: run `deploy run %s` to trigger a run", proj.Key)
			})
		},
	}

	f := cmd.Flags()
	f.StringVar(&kind, "kind", "app", "project type: app or lib")
	f.StringSliceVar(&dependencies, "dependencies", nil, "dependency project keys; comma-separated or repeatable")
	f.StringVar(&filePath, "file", "", "read the project definition from a JSON file (explicit flags override the file)")
	f.StringVar(&key, "key", "", "project key (unique, used in the URL; letters/digits/-/_)")
	f.StringVar(&name, "name", "", "project name (defaults to the key)")
	f.StringVar(&workdir, "workdir", "", "workdir (absolute path on the server)")
	f.StringVar(&description, "description", "", "description")
	f.StringVar(&shell, "shell", "auto", "shell type: auto/bash/sh/pwsh/cmd")
	f.StringVar(&concurrency, "concurrency", "queue", "concurrency: queue/allow/cancelPrev")
	f.IntVar(&timeoutSec, "timeout", 0, "overall timeout in seconds (0 = unlimited)")
	f.StringArrayVar(&commandSpecs, "command", nil, "command script; repeatable; \"name::script\" sets the name")
	f.StringArrayVar(&envPairs, "env", nil, "env var K=V; repeatable")
	f.StringVar(&failStrategy, "fail-strategy", "abort", "command failure strategy: abort/continue")
	f.BoolVar(&hook, "hook", true, "allow Hook triggers (disable with --hook=false)")
	f.BoolVar(&allowEnvOverride, "allow-env-override", true, "allow -e to override env vars at trigger time")
	f.BoolVar(&useGit, "use-git", false, "enable the Git prepare phase (requires --git-url)")
	f.StringVar(&gitURL, "git-url", "", "Git repository URL")
	f.StringVar(&gitBranch, "git-branch", "main", "default Git branch")
	f.StringVar(&gitSubdir, "git-subdir", "", "Git subdirectory")
	f.IntVar(&gitDepth, "git-depth", 0, "Git shallow clone depth (0 = full)")
	f.BoolVar(&gitClean, "git-clean", false, "clean the workdir before fetching")
	f.BoolVar(&gitSubmodule, "git-submodule", false, "fetch submodules recursively")

	return cmd
}

// projectFile 是 `deploy project create --file <path.json>` 读取的项目定义。
//
// 字段名与 API 的 ProjectInput、`deploy project get --json` 对齐，所以可以把现有项目
// 导出后改一改再建。每条命令的脚本二选一：script 内联，或 scriptFile 指向一个文件
// （相对 JSON 文件所在目录，Windows 的 sh\01.sh 写法也能用）。
type projectFile struct {
	Kind             string            `json:"kind"`
	Dependencies     []string          `json:"dependencies"`
	Key              string            `json:"key"`
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	Workdir          string            `json:"workdir"`
	Shell            string            `json:"shell"`
	TimeoutSec       *int              `json:"timeoutSec"`
	Concurrency      string            `json:"concurrency"`
	HookEnabled      *bool             `json:"hookEnabled"`
	UseGit           *bool             `json:"useGit"`
	GitURL           string            `json:"gitUrl"`
	GitBranch        string            `json:"gitBranch"`
	GitSubdir        string            `json:"gitSubdir"`
	GitDepth         *int              `json:"gitDepth"`
	GitClean         *bool             `json:"gitClean"`
	GitSubmodule     *bool             `json:"gitSubmodule"`
	AllowEnvOverride *bool             `json:"allowEnvOverride"`
	Commands         []projectFileCmd  `json:"commands"`
	Envs             []api.EnvVarInput `json:"envs"`
	// Env 是 envs 的简写形式：{"APP_PORT": "10062"}。
	Env map[string]string `json:"env"`
}

// projectFileCmd 是文件里的一条命令。
type projectFileCmd struct {
	Name            string `json:"name"`
	Script          string `json:"script"`
	ScriptFile      string `json:"scriptFile"`
	Enabled         *bool  `json:"enabled"`
	FailStrategy    string `json:"failStrategy"`
	WorkdirOverride string `json:"workdirOverride"`
	TimeoutSec      int    `json:"timeoutSec"`
	ShellOverride   string `json:"shellOverride"`
}

// loadProjectFile 读取并解析 --file；同时返回 JSON 所在目录（用于解析 scriptFile）。
func loadProjectFile(path string) (*projectFile, string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, "", exitcode.Usagef("cannot read --file %s: %v", path, err)
	}
	var pf projectFile
	if err := json.Unmarshal(b, &pf); err != nil {
		return nil, "", exitcode.Usagef("cannot parse --file %s as JSON: %v", path, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return &pf, filepath.Dir(abs), nil
}

// commandInputs 把文件里的命令转成 API 入参；scriptFile 相对 JSON 目录读取。
func (pf *projectFile) commandInputs(baseDir, defaultFailStrategy string) ([]api.CommandInput, error) {
	out := make([]api.CommandInput, 0, len(pf.Commands))
	for i, c := range pf.Commands {
		script := c.Script
		if strings.TrimSpace(c.ScriptFile) != "" {
			if strings.TrimSpace(script) != "" {
				return nil, exitcode.Usagef("command %d sets both script and scriptFile; pick one", i+1)
			}
			p := c.ScriptFile
			if !filepath.IsAbs(p) {
				p = filepath.Join(baseDir, filepath.FromSlash(p))
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return nil, exitcode.Usagef("command %d: cannot read scriptFile %s: %v", i+1, c.ScriptFile, err)
			}
			script = strings.ReplaceAll(string(b), "\r\n", "\n")
		}
		if strings.TrimSpace(script) == "" {
			return nil, exitcode.Usagef("command %d has an empty script (set script or scriptFile)", i+1)
		}
		name := strings.TrimSpace(c.Name)
		if name == "" {
			name = fmt.Sprintf("step %d", i+1)
		}
		enabled := true
		if c.Enabled != nil {
			enabled = *c.Enabled
		}
		out = append(out, api.CommandInput{
			Name:            name,
			Script:          script,
			SortOrder:       i + 1,
			Enabled:         enabled,
			FailStrategy:    orDefault(c.FailStrategy, defaultFailStrategy),
			WorkdirOverride: c.WorkdirOverride,
			TimeoutSec:      c.TimeoutSec,
			ShellOverride:   c.ShellOverride,
		})
	}
	return out, nil
}

// envPairs 把 envs 数组与 env 简写映射合并成 K=V（同名以 envs 为准），供 parseEnvPairs 复用。
func (pf *projectFile) envPairs() []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(pf.Envs)+len(pf.Env))
	for _, e := range pf.Envs {
		k := strings.TrimSpace(e.Key)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k+"="+e.Value)
	}
	keys := make([]string, 0, len(pf.Env))
	for k := range pf.Env {
		tk := strings.TrimSpace(k)
		if tk != "" && !seen[tk] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		tk := strings.TrimSpace(k)
		if seen[tk] {
			continue
		}
		seen[tk] = true
		out = append(out, tk+"="+pf.Env[k])
	}
	return out
}

// splitCommandSpec 支持 "name::script"；没有 "::" 时自动命名。
func splitCommandSpec(index int, spec string) (string, string) {
	if parts := strings.SplitN(spec, "::", 2); len(parts) == 2 && strings.TrimSpace(parts[0]) != "" {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return fmt.Sprintf("step %d", index), strings.TrimSpace(spec)
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
