package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"deploy-server/internal/store"
)

// gitInfo 是 Prepare 阶段采集到的仓库信息，随后作为内置变量注入用户命令。
type gitInfo struct {
	Commit      string
	CommitShort string
	Branch      string
	Tag         string
	Author      string
	Message     string
	Dirty       string

	// 下面三个只用于「把这次构建到底跑了哪个提交」写进日志与运行记录，
	// 不参与内置变量（GIT_AUTHOR 仍然是 %an，没有邮箱）。
	AuthorLine string // 形如 "litongjava <litongjava001@gmail.com>"
	CommitDate string // git 的 ISO 日期，含时区
	LogBlock   string // `git log -1` 的原始输出，整块打印进运行日志
}

// gitEnv 是执行 git 所需的额外环境与清理函数。
type gitEnv struct {
	extra   []string // 追加到进程环境里的键值
	secrets []string // 需要打码的值（凭据明文）
	cleanup func()
}

func (g *gitEnv) close() {
	if g != nil && g.cleanup != nil {
		g.cleanup()
	}
}

var shaRe = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

// prepareGit 执行 Git 准备阶段。
//
// 设计要点（对应需求 FR-GIT-1..FR-GIT-9）：
//   - 目录不存在 → clone；已是仓库 → fetch + 强制对齐到目标 ref；存在但不是仓库 → 直接报错
//     （宁可失败也不覆盖用户已有目录）
//   - 凭据通过 GIT_ASKPASS / GIT_SSH_COMMAND 注入，**绝不拼进 URL**
//   - 失败时返回 error，调用方据此中止整个 Run，不执行任何用户命令
func (e *Executor) prepareGit(ctx context.Context, job *jobSpec, ref, workdir string, out io.Writer) (gitInfo, []string, error) {
	var info gitInfo

	ge, err := e.buildGitEnv(job)
	if err != nil {
		return info, nil, err
	}
	defer ge.close()

	if ref == "" {
		ref = job.GitBranch
	}
	if ref == "" {
		ref = "main"
	}

	env := append(os.Environ(), ge.extra...)
	isSHA := shaRe.MatchString(ref)

	gitDir := filepath.Join(workdir, ".git")
	_, statErr := os.Stat(gitDir)
	exists := statErr == nil

	if !exists {
		// 目录存在且有内容但不是 git 仓库：拒绝继续，避免把用户的东西覆盖掉。
		if entries, err := os.ReadDir(workdir); err == nil && len(entries) > 0 {
			return info, ge.secrets, fmt.Errorf(
				"workdir %s already exists and is not a Git repository (%d entries); refusing to clone for safety. Empty that directory or use a new one",
				workdir, len(entries))
		}
		if err := os.MkdirAll(filepath.Dir(workdir), 0o750); err != nil {
			return info, ge.secrets, err
		}

		args := []string{"clone", "--progress"}
		if job.GitDepth > 0 && !isSHA {
			args = append(args, "--depth", fmt.Sprint(job.GitDepth))
		}
		if job.GitSubmodule {
			args = append(args, "--recurse-submodules")
		}
		if !isSHA {
			args = append(args, "--branch", ref)
		}
		args = append(args, job.GitURL, workdir)

		fmt.Fprintf(out, "$ git clone %s (%s)\n", job.GitURL, ref)
		if code, err := runCommand(ctx, "git", args, "", env, out, nil); err != nil || code != 0 {
			return info, ge.secrets, fmt.Errorf("git clone failed (exit code %d): %v", code, err)
		}
		if isSHA {
			if code, err := runCommand(ctx, "git", []string{"checkout", "-f", ref}, workdir, env, out, nil); err != nil || code != 0 {
				return info, ge.secrets, fmt.Errorf("checkout %s failed (exit code %d)", ref, code)
			}
		}
	} else {
		if code, err := runCommand(ctx, "git", []string{"fetch", "--prune", "--tags", "origin"}, workdir, env, out, nil); err != nil || code != 0 {
			return info, ge.secrets, fmt.Errorf("git fetch failed (exit code %d): %v", code, err)
		}

		if isSHA {
			if code, _ := runCommand(ctx, "git", []string{"checkout", "-f", ref}, workdir, env, out, nil); code != 0 {
				return info, ge.secrets, fmt.Errorf("checkout %s failed (exit code %d)", ref, code)
			}
		} else {
			// 先试「作为远端分支对齐」。第一次拉取、分支被 rebase 过都能靠它收敛。
			code, _ := runCommand(ctx, "git", []string{"checkout", "-f", "-B", ref, "origin/" + ref}, workdir, env, out, nil)
			if code != 0 {
				// 不是分支（可能是 tag），退回普通 checkout。
				if code2, _ := runCommand(ctx, "git", []string{"checkout", "-f", ref}, workdir, env, out, nil); code2 != 0 {
					return info, ge.secrets, fmt.Errorf("checkout %s failed (neither a branch nor a usable ref)", ref)
				}
			}
		}

		if job.GitClean {
			// gitClean 会删掉未跟踪文件，属危险操作，前端必须二次确认。
			if code, _ := runCommand(ctx, "git", []string{"clean", "-fdx"}, workdir, env, out, nil); code != 0 {
				fmt.Fprintln(out, "*** git clean returned non-zero, ignored ***")
			}
		}
		if job.GitSubmodule {
			if code, _ := runCommand(ctx, "git", []string{"submodule", "update", "--init", "--recursive"}, workdir, env, out, nil); code != 0 {
				return info, ge.secrets, fmt.Errorf("submodule update failed (exit code %d)", code)
			}
		}
	}

	info.Commit = gitCapture(ctx, workdir, env, "rev-parse", "HEAD")
	info.CommitShort = gitCapture(ctx, workdir, env, "rev-parse", "--short", "HEAD")
	info.Branch = gitCapture(ctx, workdir, env, "rev-parse", "--abbrev-ref", "HEAD")
	info.Tag = gitCapture(ctx, workdir, env, "describe", "--tags", "--abbrev=0")
	info.Author = gitCapture(ctx, workdir, env, "log", "-1", "--pretty=%an")
	info.Message = gitCapture(ctx, workdir, env, "log", "-1", "--pretty=%s")

	// 详细提交信息：把「这次构建跑的到底是哪个提交」完整记下来。
	// --decorate 让输出里带上当前 ref（HEAD -> main, origin/main），一眼能看出
	// 拉的是远端哪条线；%ad 用 git 的 ISO 格式（带时区）而不是 %cd/%ci 之外的花样。
	info.AuthorLine = gitCapture(ctx, workdir, env, "log", "-1", "--pretty=%an <%ae>")
	info.CommitDate = gitCapture(ctx, workdir, env, "log", "-1", "--pretty=%ad", "--date=iso")
	info.LogBlock = gitCapture(ctx, workdir, env, "log", "-1", "--decorate", "--date=iso")

	if dirty := gitCapture(ctx, workdir, env, "status", "--porcelain"); dirty != "" {
		info.Dirty = "true"
	} else {
		info.Dirty = "false"
	}
	if info.Branch == "HEAD" {
		info.Branch = ref
	}
	return info, ge.secrets, nil
}

// TestGitAccess 真的去 ls-remote 一次，用来在项目配置页验证
// 「地址 + 凭据」这组搭配能不能拉通。失败时返回的错误里会带上 git 的原始输出。
func (e *Executor) TestGitAccess(ctx context.Context, gitURL string, credID *uint) (string, error) {
	probe := &jobSpec{UseGit: true, GitURL: gitURL, GitCredentialID: credID}
	ge, err := e.buildGitEnv(probe)
	if err != nil {
		return "", err
	}
	defer ge.close()

	env := append(os.Environ(), ge.extra...)
	var buf bytes.Buffer
	code, err := runCommand(ctx, "git", []string{"ls-remote", "--heads", "--tags", gitURL}, "", env, &buf, nil)
	if err != nil {
		return "", fmt.Errorf("failed to run git: %w", err)
	}
	if code != 0 {
		msg := strings.TrimSpace(buf.String())
		if msg == "" {
			msg = fmt.Sprintf("git ls-remote returned exit code %d (common causes: wrong URL, invalid credentials, no network)", code)
		}
		return "", errors.New(redact(msg, ge.secrets))
	}
	return buf.String(), nil
}

// redact 在错误信息里抹掉凭据，避免它顺着错误响应漏出去。
func redact(s string, secrets []string) string {
	for _, sec := range secrets {
		if len(sec) >= 3 {
			s = strings.ReplaceAll(s, sec, "***")
		}
	}
	return s
}

func gitCapture(ctx context.Context, dir string, env []string, args ...string) string {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// buildGitEnv 按凭据类型准备 git 的运行环境。
//
// HTTPS：写一个临时 askpass 脚本，git 需要用户名/密码时会调用它，
// 凭据从环境变量读取。这样 token 不会出现在 URL、remote 配置或进程参数里。
// SSH：把私钥写到临时文件（0600），用 GIT_SSH_COMMAND 指定，并关掉主机指纹交互。
func (e *Executor) buildGitEnv(job *jobSpec) (*gitEnv, error) {
	ge := &gitEnv{cleanup: func() {}}
	if job.GitCredentialID == nil {
		return ge, nil
	}

	var cred store.Credential
	if err := e.st.DB().First(&cred, *job.GitCredentialID).Error; err != nil {
		return nil, fmt.Errorf("credential %d not found: %w", *job.GitCredentialID, err)
	}

	plain, err := e.box.Decrypt(cred.SecretEnc)
	if err != nil {
		return nil, err
	}
	passphrase, err := e.box.Decrypt(cred.Passphrase)
	if err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "deploy-git-")
	if err != nil {
		return nil, err
	}
	ge.cleanup = func() { _ = os.RemoveAll(dir) }

	switch cred.Type {
	case "https_token":
		script := filepath.Join(dir, "askpass.sh")
		body := "#!/bin/sh\n" +
			"case \"$1\" in\n" +
			"  *sername*) printf '%s' \"$DEPLOY_GIT_USER\" ;;\n" +
			"  *) printf '%s' \"$DEPLOY_GIT_PASS\" ;;\n" +
			"esac\n"
		if IsWindows() {
			script = filepath.Join(dir, "askpass.cmd")
			body = "@echo off\r\n" +
				"echo %~1 | findstr /I /C:\"sername\" >nul\r\n" +
				"if %errorlevel%==0 (echo|set /p=\"%DEPLOY_GIT_USER%\") else (echo|set /p=\"%DEPLOY_GIT_PASS%\")\r\n"
		}
		if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
			ge.close()
			return nil, err
		}
		ge.extra = append(ge.extra,
			"GIT_ASKPASS="+script,
			"DEPLOY_GIT_USER="+cred.Username,
			"DEPLOY_GIT_PASS="+plain,
			"GIT_TERMINAL_PROMPT=0",
		)
		ge.secrets = append(ge.secrets, plain)
		if cred.Username != "" {
			ge.secrets = append(ge.secrets, cred.Username)
		}

	case "ssh_key":
		keyPath := filepath.Join(dir, "id_deploy")
		if err := os.WriteFile(keyPath, []byte(plain), 0o600); err != nil {
			ge.close()
			return nil, err
		}
		sshCmd := fmt.Sprintf("ssh -i %q -o IdentitiesOnly=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=%s",
			keyPath, filepath.Join(dir, "known_hosts"))
		ge.extra = append(ge.extra,
			"GIT_SSH_COMMAND="+sshCmd,
			"GIT_TERMINAL_PROMPT=0",
		)
		if passphrase != "" {
			// 带口令的私钥需要 ssh-agent，MVP 阶段明确不支持，给出可操作的错误。
			ge.close()
			return nil, fmt.Errorf("SSH keys with a passphrase are not supported yet; use an https_token credential or generate a key without a passphrase")
		}
		ge.secrets = append(ge.secrets, plain)

	default:
		ge.close()
		return nil, fmt.Errorf("unknown credential type: %s", cred.Type)
	}

	return ge, nil
}
