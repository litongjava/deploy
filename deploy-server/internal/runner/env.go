package runner

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"deploy-server/internal/secret"
	"deploy-server/internal/store"
)

// EnvVar 是与存储层解耦的环境变量视图。
//
// 项目把变量存在 project_envs 表里，任务把它们内联存成 JSON —— 执行引擎不该关心
// 这件事，所以两边都先映射成 EnvVar 再进 BuildEnv。
type EnvVar struct {
	Key       string
	Value     string
	SecretEnc string
	IsSecret  bool
}

// EnvVarsFromProject / EnvVarsFromTask 把两种存储形态映射成统一的 EnvVar。
func EnvVarsFromProject(envs []store.ProjectEnv) []EnvVar {
	out := make([]EnvVar, 0, len(envs))
	for _, e := range envs {
		out = append(out, EnvVar{Key: e.Key, Value: e.Value, SecretEnc: e.SecretEnc, IsSecret: e.IsSecret})
	}
	return out
}

func EnvVarsFromTask(envs []store.TaskEnvVar) []EnvVar {
	out := make([]EnvVar, 0, len(envs))
	for _, e := range envs {
		out = append(out, EnvVar{Key: e.Key, Value: e.Value, SecretEnc: e.SecretEnc, IsSecret: e.IsSecret})
	}
	return out
}

// EnvPlan 描述「这次运行该注入哪些环境变量」。
type EnvPlan struct {
	EnvVars     []EnvVar
	Builtins    map[string]string
	Params      map[string]string
	AllowParams bool
	Box         *secret.Box
}

// BuildEnv 按契约里的优先级合并环境变量并产出 exec.Cmd 需要的切片。
//
// 优先级（后者覆盖前者）：
//  1. 服务端自身环境
//  2. 项目/任务的普通环境变量
//  3. 项目/任务的 Secret 环境变量
//  4. 内置变量（BUILD_NUMBER / GIT_COMMIT 等）
//  5. 触发时传入的 env（仅当允许覆盖）
//
// 同时返回「所有 Secret 明文」，供日志打码使用 —— 打码需要的是明文，
// 而注入到进程里的也必须是明文。
func BuildEnv(plan EnvPlan) (env []string, secretValues []string, err error) {
	merged := map[string]string{}
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 {
			merged[kv[:i]] = kv[i+1:]
		}
	}

	// 2 & 3：项目/任务环境变量
	for _, e := range plan.EnvVars {
		if e.Key == "" {
			continue
		}
		if e.IsSecret {
			plain := ""
			if plan.Box != nil && e.SecretEnc != "" {
				plain, err = plan.Box.Decrypt(e.SecretEnc)
				if err != nil {
					return nil, nil, err
				}
			}
			merged[e.Key] = plain
			if plain != "" {
				secretValues = append(secretValues, plain)
			}
			continue
		}
		merged[e.Key] = e.Value
	}

	// 4：内置变量，永远覆盖项目值，保证 BUILD_NUMBER 之类不被用户改坏。
	for k, v := range plan.Builtins {
		merged[k] = v
	}

	// 5：触发参数，仅在允许时生效。
	if plan.AllowParams {
		for k, v := range plan.Params {
			if k == "" {
				continue
			}
			merged[k] = v
		}
	}

	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	env = make([]string, 0, len(keys))
	for _, k := range keys {
		env = append(env, k+"="+merged[k])
	}
	return env, secretValues, nil
}

// ExpandWorkdir 把工作目录里的占位符替换成真实路径。
//
// 支持 ${WORKSPACE_ROOT}、${PROJECT_KEY}、${PROJECT_NAME} 以及等价的
// ${TASK_KEY} / ${TASK_NAME}。任务沿用同一套占位符：一份脚本从项目挪到任务里
// 不该因为变量名不同就跑不起来。
func ExpandWorkdir(raw, webRoot, key, name string) string {
	if raw == "" {
		raw = "${WORKSPACE_ROOT}/${PROJECT_KEY}"
	}
	r := strings.NewReplacer(
		"${WORKSPACE_ROOT}", webRoot,
		"$WORKSPACE_ROOT", webRoot,
		"${PROJECT_KEY}", key,
		"$PROJECT_KEY", key,
		"${PROJECT_NAME}", name,
		"$PROJECT_NAME", name,
		"${TASK_KEY}", key,
		"$TASK_KEY", key,
		"${TASK_NAME}", name,
		"$TASK_NAME", name,
	)
	return r.Replace(raw)
}

// SecretValues 解出所有 Secret 环境变量的明文，供日志打码使用。
// 解密失败的值会被跳过（打码清单少一项，总好过整个 Run 起不来）。
func SecretValues(envs []EnvVar, box *secret.Box) []string {
	var out []string
	for _, e := range envs {
		if !e.IsSecret || e.SecretEnc == "" || box == nil {
			continue
		}
		plain, err := box.Decrypt(e.SecretEnc)
		if err != nil || plain == "" {
			continue
		}
		out = append(out, plain)
	}
	return out
}

// encodeParams / decodeParams 负责触发参数与 Run.ParamsJSON 之间的转换。
func encodeParams(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(raw)
}

func decodeParams(s string) map[string]string {
	if s == "" {
		return nil
	}
	out := map[string]string{}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}
