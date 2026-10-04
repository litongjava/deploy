// Package config 负责 ~/.deploy/config.yaml 的读写与配置优先级解析。
//
// 优先级（高 → 低）：命令行 flag > 环境变量 > 配置文件 > 默认值。
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// 默认值。
const (
	// DefaultContext 是未显式切换时使用的 context 名。
	DefaultContext = "default"
	// DefaultServer 是没有任何配置时的兜底服务端地址。
	DefaultServer = "http://127.0.0.1:8080"
	// EnvServer / EnvToken / EnvConfig 是支持的环境变量。
	EnvServer = "DEPLOY_SERVER"
	EnvToken  = "DEPLOY_TOKEN"
	EnvConfig = "DEPLOY_CONFIG"
)

// Context 是一个服务端 + 凭证的组合。
type Context struct {
	Server string `yaml:"server,omitempty"`
	Token  string `yaml:"token,omitempty"`
	User   string `yaml:"user,omitempty"`
}

// Config 是配置文件的根结构。
type Config struct {
	Current  string              `yaml:"current"`
	Contexts map[string]*Context `yaml:"contexts"`

	path string
}

// DefaultPath 返回 ~/.deploy/config.yaml（Windows 下即 %USERPROFILE%\.deploy\config.yaml）。
func DefaultPath() (string, error) {
	if p := os.Getenv(EnvConfig); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法确定用户主目录: %w", err)
	}
	return filepath.Join(home, ".deploy", "config.yaml"), nil
}

// Load 读取配置文件；文件不存在时返回带默认值的空配置（不报错）。
func Load(path string) (*Config, error) {
	c := &Config{Current: DefaultContext, Contexts: map[string]*Context{}, path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取配置文件 %s 失败: %w", path, err)
	}
	if len(b) == 0 {
		return c, nil
	}
	if err := yaml.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("解析配置文件 %s 失败（YAML 格式错误）: %w", path, err)
	}
	c.path = path
	if c.Contexts == nil {
		c.Contexts = map[string]*Context{}
	}
	for name, ctx := range c.Contexts {
		if ctx == nil {
			c.Contexts[name] = &Context{}
		}
	}
	if c.Current == "" {
		c.Current = DefaultContext
	}
	return c, nil
}

// Path 返回该配置对应的文件路径。
func (c *Config) Path() string { return c.path }

// SetPath 覆盖配置文件路径（测试用）。
func (c *Config) SetPath(p string) { c.path = p }

// Active 返回当前 context 的副本；不存在时创建。
func (c *Config) Active() *Context {
	if c.Contexts == nil {
		c.Contexts = map[string]*Context{}
	}
	if c.Current == "" {
		c.Current = DefaultContext
	}
	ctx, ok := c.Contexts[c.Current]
	if !ok || ctx == nil {
		ctx = &Context{}
		c.Contexts[c.Current] = ctx
	}
	return ctx
}

// ContextNames 返回全部 context 名。
func (c *Config) ContextNames() []string {
	names := make([]string, 0, len(c.Contexts))
	for k := range c.Contexts {
		names = append(names, k)
	}
	return names
}

// UseContext 切换当前 context；不存在时以当前 context 的 server 为模板新建。
func (c *Config) UseContext(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("context 名不能为空")
	}
	if c.Contexts == nil {
		c.Contexts = map[string]*Context{}
	}
	if _, ok := c.Contexts[name]; !ok {
		tmpl := c.Active()
		c.Contexts[name] = &Context{Server: tmpl.Server}
	}
	c.Current = name
	return nil
}

// Save 原子写入配置文件：目录 0700、文件 0600（Windows 上尽力而为）。
func (c *Config) Save() error {
	if c.path == "" {
		return errors.New("配置路径为空")
	}
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("创建配置目录 %s 失败: %w", dir, err)
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}
	// Windows 上 os.Chmod 基本无效，但仍尽力而为；Unix 上确保 0600。
	_ = os.Chmod(tmp, 0o600)
	if err := os.Rename(tmp, c.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("保存配置文件失败: %w", err)
	}
	_ = os.Chmod(c.path, 0o600)
	return nil
}

// 支持的配置键。
const (
	KeyServer  = "server"
	KeyToken   = "token"
	KeyUser    = "user"
	KeyCurrent = "current"
)

// Get 读取配置项的当前有效值。
// 特殊键 current 返回当前 context 名；server/token/user 读当前 context。
func (c *Config) Get(key string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case KeyCurrent:
		if c.Current == "" {
			return DefaultContext, nil
		}
		return c.Current, nil
	case KeyServer:
		return c.Active().Server, nil
	case KeyToken:
		return c.Active().Token, nil
	case KeyUser:
		return c.Active().User, nil
	case "path":
		return c.path, nil
	default:
		return "", fmt.Errorf("未知配置项 %q（可用: server, token, user, current）", key)
	}
}

// Set 写入配置项并返回是否成功。
func (c *Config) Set(key, value string) error {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case KeyCurrent:
		return c.UseContext(value)
	case KeyServer:
		c.Active().Server = NormalizeServer(value)
		return nil
	case KeyToken:
		c.Active().Token = strings.TrimSpace(value)
		return nil
	case KeyUser:
		c.Active().User = strings.TrimSpace(value)
		return nil
	default:
		return fmt.Errorf("未知配置项 %q（可用: server, token, user, current）", key)
	}
}

// NormalizeServer 规范化服务端地址：补 scheme、去掉结尾斜杠。
func NormalizeServer(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	return strings.TrimRight(s, "/")
}

// Settings 是解析优先级之后的最终运行参数。
type Settings struct {
	Server string
	Token  string
	User   string
	// 来源说明，仅用于 --verbose 与 whoami 展示。
	ServerSource string
	TokenSource  string
}

// Getenv 抽象环境变量读取，便于测试。
type Getenv func(string) string

// Resolve 按 flag > 环境变量 > 配置文件 > 默认值 的顺序解析。
func Resolve(flagServer, flagToken string, getenv Getenv, cfg *Config) Settings {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	var s Settings
	var active *Context
	if cfg != nil {
		active = cfg.Active()
	} else {
		active = &Context{}
	}

	switch {
	case strings.TrimSpace(flagServer) != "":
		s.Server, s.ServerSource = NormalizeServer(flagServer), "flag"
	case strings.TrimSpace(getenv(EnvServer)) != "":
		s.Server, s.ServerSource = NormalizeServer(getenv(EnvServer)), "env"
	case strings.TrimSpace(active.Server) != "":
		s.Server, s.ServerSource = NormalizeServer(active.Server), "config"
	default:
		s.Server, s.ServerSource = DefaultServer, "default"
	}

	switch {
	case strings.TrimSpace(flagToken) != "":
		s.Token, s.TokenSource = strings.TrimSpace(flagToken), "flag"
	case strings.TrimSpace(getenv(EnvToken)) != "":
		s.Token, s.TokenSource = strings.TrimSpace(getenv(EnvToken)), "env"
	case strings.TrimSpace(active.Token) != "":
		s.Token, s.TokenSource = strings.TrimSpace(active.Token), "config"
	default:
		s.Token, s.TokenSource = "", "none"
	}

	s.User = active.User
	if s.User == "" {
		s.User = getenv("DEPLOY_USER")
	}
	return s
}
