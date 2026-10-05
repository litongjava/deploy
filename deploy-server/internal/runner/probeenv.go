package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// 这个文件解决的是一类「自检说谎」的问题。
//
// Unix 下项目脚本是用 `bash -lc` 跑的（见 resolveShell），登录 shell 会读
// /etc/profile 与 /etc/profile.d/*.sh —— 服务器上手工解压的 JDK / Maven / Go
// 往往正是靠这些文件把 $JAVA_HOME/bin、$MVN_HOME/bin 追加进 PATH 的。
// 而服务进程自己只有 systemd 给的最小 PATH：
//
//	PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
//
// 于是会出现「在服务器上 mvn -v 明明能跑，环境自检却说不存在」：
// 差的不是工具，是自检用了服务进程的环境、而执行用的是登录 shell 的环境。
// 自检与工具链状态必须按**执行口径**探测，否则它的结论会和真实构建结果相反。

// LoginEnv 返回项目脚本执行时实际生效的环境变量。
//
// 选中的 shell 不是登录 shell 时（例如全局或项目把 shell 设成 sh，走 `sh -c`），
// 执行环境就是服务进程自己的环境，此时直接返回 os.Environ()，同样与执行口径一致。
func LoginEnv(globalShell string) []string {
	if IsWindows() {
		return os.Environ()
	}
	exe, args := resolveShell("", "", globalShell)
	if len(args) == 0 || args[0] != "-lc" {
		return os.Environ()
	}

	// env -0：NUL 分隔、不做任何转义，PATH 里带空格也不会被拆错。
	if out, err := shellOutput(exe, 5*time.Second, "env -0"); err == nil {
		if env := parseEnvironBlob(out); envLookup(env, "PATH") != "" {
			return env
		}
	}

	// 兜底：拿不到整份环境时，至少把登录 shell 的 PATH 换进来 —— 这正是
	// 「装了却找不到」的那一项；其余变量沿用服务进程自己的。
	if out, err := shellOutput(exe, 5*time.Second, `printf '%s' "$PATH"`); err == nil {
		if p := strings.TrimSpace(out); p != "" {
			return withEnvValue(os.Environ(), "PATH", p)
		}
	}
	return os.Environ()
}

// shellOutput 用给定的 shell 执行一段脚本并取回 stdout（stderr 丢弃）。
func shellOutput(exe string, timeout time.Duration, script string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-lc", script)
	cmd.Stderr = nil
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// parseEnvironBlob 解析 `env -0` 的输出（NUL 分隔的 KEY=VALUE）。
//
// 登录 shell 可能往 stdout 里夹带欢迎语之类的杂输出，所以只认「合法键名的
// KEY=VALUE」，其余一律丢掉，不能因为一行噪音就把整份环境判死。
func parseEnvironBlob(blob string) []string {
	parts := strings.Split(blob, "\x00")
	env := make([]string, 0, len(parts))
	for _, p := range parts {
		i := strings.IndexByte(p, '=')
		if i <= 0 || !validEnvKey(p[:i]) {
			continue
		}
		env = append(env, p)
	}
	return env
}

// validEnvKey 判断是不是一个合法的环境变量名（POSIX：字母/下划线开头，后接字母数字下划线）。
func validEnvKey(k string) bool {
	for i := 0; i < len(k); i++ {
		c := k[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return k != ""
}

// LookPathIn 在给定环境的 PATH 里解析可执行文件；env 为 nil 或当前平台是 Windows 时，
// 退回 exec.LookPath（即服务进程自己的 PATH）。
//
// 这里刻意**不**在登录 shell 的 PATH 找不到时回退到 exec.LookPath：探测要的就是
// 「脚本跑起来时能不能找到」。若登录 shell 把 PATH 设得比服务进程更窄，那构建时
// 也会找不到，如实报告才对。
func LookPathIn(name string, env []string) (string, error) {
	if len(env) == 0 || IsWindows() || strings.ContainsAny(name, `/\`) {
		return exec.LookPath(name)
	}
	return lookPathInDirs(name, envLookup(env, "PATH"))
}

// lookPathInDirs 在 PATH 串里逐个目录找可执行文件。
// 单独抽出来是为了在任何平台上都能测（LookPathIn 在 Windows 上会直接走 exec.LookPath）。
func lookPathInDirs(name, pathEnv string) (string, error) {
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		if isExecutableFile(candidate) {
			return candidate, nil
		}
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

// isExecutableFile 判断路径是不是「普通文件且有执行位」。
// 不用 exec.LookPath 是因为后者只看当前进程的 PATH，与这里要问的问题不是一回事。
func isExecutableFile(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return false
	}
	return fi.Mode().Perm()&0111 != 0
}

// envLookup 取环境变量切片里某个键的值（取最后一个，与 exec 的覆盖语义一致）。
func envLookup(env []string, key string) string {
	prefix := key + "="
	val := ""
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			val = kv[len(prefix):]
		}
	}
	return val
}

// EnvValue 是 envLookup 的导出形式：给「按执行环境展开探针路径」这类调用用。
func EnvValue(env []string, key string) string { return envLookup(env, key) }

// withEnvValue 复制一份环境并覆盖某个键（不存在则追加）。
func withEnvValue(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			if !replaced {
				out = append(out, prefix+value)
				replaced = true
			}
			continue
		}
		out = append(out, kv)
	}
	if !replaced {
		out = append(out, prefix+value)
	}
	return out
}
