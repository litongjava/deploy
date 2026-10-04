// sshpoke 是一个只做三件事的 SSH 运维小工具：
//
//	exec      在远端跑一条命令，stdout/stderr 原样流回，远端退出码即本地退出码
//	runscript 把本地 .sh 文件喂给远端 `bash -s` 执行（避开 shell 引号问题）
//	put       上传单个文件（自动创建父目录、保留可执行位）
//	push      递归上传整个目录（增量：大小与 mtime 一致则跳过）
//
// 存在的理由：Windows 自带的 OpenSSH 客户端不读 stdin 里的密码，
// 而部署流程需要在脚本里非交互地跑远端命令与传文件。
//
// 密码来源优先级：-pass > 环境变量 SSHPOKE_PASS。推荐用环境变量，
// 避免密码出现在进程命令行里。
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func main() {
	var (
		host    = flag.String("host", "", "远端主机")
		port    = flag.Int("port", 22, "SSH 端口")
		user    = flag.String("user", "root", "登录用户")
		pass    = flag.String("pass", "", "登录密码（留空则读 SSHPOKE_PASS）")
		timeout = flag.Duration("timeout", 30*time.Minute, "整条命令的超时")
		quiet   = flag.Bool("quiet", false, "push 时少打日志")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "用法:\n"+
			"  sshpoke [flags] exec <命令...>\n"+
			"  sshpoke [flags] runscript <本地.sh>\n"+
			"  sshpoke [flags] put <本地文件> <远端路径>\n"+
			"  sshpoke [flags] push <本地目录> <远端目录>\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		os.Exit(3)
	}
	if *pass == "" {
		*pass = os.Getenv("SSHPOKE_PASS")
	}
	if *host == "" || *pass == "" {
		fmt.Fprintln(os.Stderr, "sshpoke: 必须提供 -host 与密码（-pass 或 SSHPOKE_PASS）")
		os.Exit(3)
	}

	cmd := args[0]
	rest := args[1:]

	// 用一次性 deadline 包住整个动作，避免网络半死时永久挂住。
	done := make(chan struct{})
	var code int
	go func() {
		defer close(done)
		var err error
		code, err = run(cmd, rest, *host, *port, *user, *pass, *quiet)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sshpoke: %v\n", err)
			if code == 0 {
				code = 1
			}
		}
	}()

	select {
	case <-done:
		os.Exit(code)
	case <-time.After(*timeout):
		fmt.Fprintf(os.Stderr, "sshpoke: 超时 %s\n", *timeout)
		os.Exit(1)
	}
}

func run(cmd string, args []string, host string, port int, user, pass string, quiet bool) (int, error) {
	switch cmd {
	case "exec":
		if len(args) == 0 {
			return 3, fmt.Errorf("exec 需要一个命令")
		}
		return doExec(host, port, user, pass, strings.Join(args, " "))
	case "put":
		if len(args) != 2 {
			return 3, fmt.Errorf("put 需要 <本地文件> <远端路径>")
		}
		return doPut(host, port, user, pass, args[0], args[1])
	case "runscript":
		if len(args) != 1 {
			return 3, fmt.Errorf("runscript 需要一个本地 .sh 文件")
		}
		return doRunScript(host, port, user, pass, args[0])
	case "push":
		if len(args) != 2 {
			return 3, fmt.Errorf("push 需要 <本地目录> <远端目录>")
		}
		return doPush(host, port, user, pass, args[0], args[1], quiet)
	default:
		return 3, fmt.Errorf("未知子命令 %q", cmd)
	}
}

func dial(host string, port int, user, pass string) (*ssh.Client, error) {
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(pass)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         20 * time.Second,
	}
	return ssh.Dial("tcp", fmt.Sprintf("%s:%d", host, port), cfg)
}

func doExec(host string, port int, user, pass, command string) (int, error) {
	cl, err := dial(host, port, user, pass)
	if err != nil {
		return 1, fmt.Errorf("连接失败: %w", err)
	}
	defer cl.Close()

	sess, err := cl.NewSession()
	if err != nil {
		return 1, err
	}
	defer sess.Close()

	// 申请一个带宽度的 pty 会让 `systemctl is-active` 之类命令输出颜色码、
	// 也会把 stdout/stderr 合流，反而难解析。这里明确不要 pty。
	var stdout, stderr io.Writer = os.Stdout, os.Stderr
	if w, ok := any(os.Stdout).(io.Writer); ok {
		stdout = w
	}
	sess.Stdout = stdout
	sess.Stderr = stderr

	err = sess.Run(command)
	if err == nil {
		return 0, nil
	}
	if ee, ok := err.(*ssh.ExitError); ok {
		return ee.ExitStatus(), nil
	}
	return 1, err
}

// doRunScript 把本地脚本内容通过 stdin 交给远端 `bash -s`。
// 这样脚本里的引号、括号、$、反引号都不经过任何一层 shell 的参数解析，
// 是喂多行脚本最可靠的方式（PowerShell 会吃掉原生命令参数里的引号）。
func doRunScript(host string, port int, user, pass, localFile string) (int, error) {
	raw, err := os.ReadFile(localFile)
	if err != nil {
		return 1, err
	}
	// 统一成 LF，避免 Windows CRLF 让 bash 把 \r 当成命令的一部分。
	script := strings.ReplaceAll(string(raw), "\r\n", "\n")

	cl, err := dial(host, port, user, pass)
	if err != nil {
		return 1, fmt.Errorf("连接失败: %w", err)
	}
	defer cl.Close()

	sess, err := cl.NewSession()
	if err != nil {
		return 1, err
	}
	defer sess.Close()

	sess.Stdin = strings.NewReader(script)
	sess.Stdout = os.Stdout
	sess.Stderr = os.Stderr

	err = sess.Run("bash -s")
	if err == nil {
		return 0, nil
	}
	if ee, ok := err.(*ssh.ExitError); ok {
		return ee.ExitStatus(), nil
	}
	return 1, err
}

func doPut(host string, port int, user, pass, local, remote string) (int, error) {
	cl, err := dial(host, port, user, pass)
	if err != nil {
		return 1, fmt.Errorf("连接失败: %w", err)
	}
	defer cl.Close()

	sc, err := sftp.NewClient(cl)
	if err != nil {
		return 1, err
	}
	defer sc.Close()

	if err := sc.MkdirAll(path.Dir(remote)); err != nil {
		return 1, fmt.Errorf("创建远端目录失败: %w", err)
	}
	if err := uploadFile(sc, local, remote); err != nil {
		return 1, err
	}
	fmt.Printf("put  %s -> %s\n", local, remote)
	return 0, nil
}

func doPush(host string, port int, user, pass, localDir, remoteDir string, quiet bool) (int, error) {
	info, err := os.Stat(localDir)
	if err != nil {
		return 1, err
	}
	if !info.IsDir() {
		return 3, fmt.Errorf("%s 不是目录", localDir)
	}

	cl, err := dial(host, port, user, pass)
	if err != nil {
		return 1, fmt.Errorf("连接失败: %w", err)
	}
	defer cl.Close()

	sc, err := sftp.NewClient(cl)
	if err != nil {
		return 1, err
	}
	defer sc.Close()

	if err := sc.MkdirAll(remoteDir); err != nil {
		return 1, fmt.Errorf("创建远端目录失败: %w", err)
	}

	var files, skipped int
	err = filepath.WalkDir(localDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(localDir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		remote := path.Join(remoteDir, filepath.ToSlash(rel))
		if d.IsDir() {
			return sc.MkdirAll(remote)
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		// 增量：远端已存在且大小、mtime 一致就跳过
		if rfi, err := sc.Stat(remote); err == nil {
			if rfi.Size() == fi.Size() && !rfi.ModTime().Before(fi.ModTime().Add(-2*time.Second)) {
				skipped++
				return nil
			}
		}
		if err := uploadFile(sc, p, remote); err != nil {
			return fmt.Errorf("上传 %s 失败: %w", rel, err)
		}
		files++
		if !quiet && files%200 == 0 {
			fmt.Printf("... 已上传 %d 个文件\n", files)
		}
		return nil
	})
	if err != nil {
		return 1, err
	}
	fmt.Printf("push %s -> %s : 上传 %d，跳过 %d\n", localDir, remoteDir, files, skipped)
	return 0, nil
}

func uploadFile(sc *sftp.Client, local, remote string) error {
	in, err := os.Open(local)
	if err != nil {
		return err
	}
	defer in.Close()

	fi, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := sc.OpenFile(remote, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return sc.Chmod(remote, fi.Mode().Perm())
}
