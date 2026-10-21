package api

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"deploy-server/internal/runner"
)

// SSH 公钥查看与生成（页面：设置 → 凭据管理）。
//
// 操作对象是**部署进程所属 OS 用户**的 ~/.ssh，而不是数据库里的 git_credential。
// 前者是管理员在服务器上手工 git clone 时实际使用的密钥；后者由 runner/git.go
// 落成每次运行的临时私钥。两者用途不同，不能混为一谈。
//
// 只读公钥；私钥永远不经过接口。

type sshKeyItem struct {
	Type        string `json:"type"`
	KeyPath     string `json:"keyPath"`
	PublicKey   string `json:"publicKey"`
	Fingerprint string `json:"fingerprint"`
}

type sshKeyDTO struct {
	Home string       `json:"home"`
	Keys []sshKeyItem `json:"keys"`
}

// sshKeyTypes 是查看时识别的密钥类型，顺序即展示优先级。
var sshKeyTypes = []string{"ed25519", "rsa", "ecdsa"}

// sshKeyHome 取部署进程所属用户的 home。
//
// 优先 os.UserHomeDir()（按有效 UID 查系统用户，systemd 下也可靠）；
// 取不到时回落到登录 shell 的 HOME，与脚本执行口径一致。
func (s *Server) sshKeyHome() string {
	if h, err := os.UserHomeDir(); err == nil && strings.TrimSpace(h) != "" {
		return strings.TrimSpace(h)
	}
	return strings.TrimSpace(runner.EnvValue(runner.LoginEnv(s.cfg.ShellPreference), "HOME"))
}

// collectSshKeys 扫描 ~/.ssh 下已存在的公钥。
func collectSshKeys(home string) []sshKeyItem {
	keys := make([]sshKeyItem, 0, len(sshKeyTypes))
	if home == "" {
		return keys
	}
	for _, typ := range sshKeyTypes {
		pubPath := filepath.Join(home, ".ssh", "id_"+typ+".pub")
		raw, err := os.ReadFile(pubPath)
		if err != nil {
			continue
		}
		pub := strings.TrimSpace(string(raw))
		if pub == "" {
			continue
		}
		keys = append(keys, sshKeyItem{
			Type:        typ,
			KeyPath:     pubPath,
			PublicKey:   pub,
			Fingerprint: sshKeyFingerprint(pubPath),
		})
	}
	return keys
}

// sshKeyFingerprint 调 ssh-keygen -lf 取指纹；失败时留空，不影响主流程。
func sshKeyFingerprint(pubPath string) string {
	ctx, cancel := timeoutCtx(5 * time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ssh-keygen", "-lf", pubPath).Output()
	if err != nil {
		return ""
	}
	return firstLine(string(out))
}

func (s *Server) getSshKey(c *gin.Context) {
	home := s.sshKeyHome()
	OK(c, sshKeyDTO{Home: home, Keys: collectSshKeys(home)})
}

type generateSshKeyReq struct {
	// Type 默认 ed25519，目前允许 ed25519 / rsa。
	Type string `json:"type"`
}

func (s *Server) generateSshKey(c *gin.Context) {
	var req generateSshKeyReq
	if err := c.ShouldBindJSON(&req); err != nil && err != io.EOF {
		BadRequest(c, `request body must look like {"type":"ed25519"}`)
		return
	}
	typ := strings.ToLower(strings.TrimSpace(req.Type))
	if typ == "" {
		typ = "ed25519"
	}
	if typ != "ed25519" && typ != "rsa" {
		BadRequest(c, "key type must be ed25519 or rsa")
		return
	}

	home := s.sshKeyHome()
	if home == "" {
		Internal(c, "cannot determine the home directory of the deploy user")
		return
	}
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		Internal(c, "failed to create "+sshDir+": "+err.Error())
		return
	}
	// 目录已存在时也校正一次权限（ssh 对过宽的 ~/.ssh 会拒绝使用）。
	_ = os.Chmod(sshDir, 0o700)

	keyPath := filepath.Join(sshDir, "id_"+typ)
	if fileExists(keyPath) || fileExists(keyPath+".pub") {
		Conflict(c, "SSH key already exists: "+keyPath+"; remove it on the server before generating a new one")
		return
	}

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "deploy-server"
	}
	args := []string{"-t", typ, "-N", "", "-C", "deploy@" + hostname, "-q", "-f", keyPath}
	if typ == "rsa" {
		args = append(args, "-b", "4096")
	}
	ctx, cancel := timeoutCtx(30 * time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ssh-keygen", args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		Internal(c, "ssh-keygen failed: "+msg)
		return
	}
	if _, err := os.Stat(keyPath + ".pub"); err != nil {
		Internal(c, "the key was generated but the public key could not be read: "+err.Error())
		return
	}
	s.audit(c, "generate-ssh-key", typ, keyPath)

	OK(c, sshKeyDTO{Home: home, Keys: collectSshKeys(home)})
}
