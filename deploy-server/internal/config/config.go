// Package config 负责“默认值 → 配置文件 → 环境变量 → 命令行参数”四层合并。
// 后一层覆盖前一层，与需求文档 §6 非功能需求里的约定一致。
package config

import (
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config 是服务端的全部可配置项。
type Config struct {
	// Listen 是 HTTP 监听地址。
	Listen string `yaml:"listen"`
	// DataDir 存放数据库、日志、密钥。必须可写。
	DataDir string `yaml:"dataDir"`
	// WebRoot 是项目工作目录的默认根，项目里用 ${WORKSPACE_ROOT} 引用。
	WebRoot string `yaml:"webRoot"`
	// ServerURL 是服务端对外地址，会作为 DEPLOY_SERVER_URL 注入给命令。
	ServerURL string `yaml:"serverUrl"`

	// JWTSecret 用于签发登录 token。留空则自动生成并持久化。
	JWTSecret string `yaml:"jwtSecret"`
	// SecretKey 用于加密环境变量与 Git 凭据（AES-GCM）。留空则自动生成并持久化。
	SecretKey string `yaml:"secretKey"`

	TokenTTLHours      int    `yaml:"tokenTtlHours"`
	MaxConcurrentRuns  int    `yaml:"maxConcurrentRuns"`
	LogRetentionDays   int    `yaml:"logRetentionDays"`
	LogMaxBytes        int64  `yaml:"logMaxBytes"`
	HookRateLimit      int    `yaml:"hookRateLimitPerMinute"`
	Visibility         string `yaml:"visibility"` // allReadable | ownerOnly
	AllowRegistration  bool   `yaml:"allowRegistration"`
	GitPrepareTimeoutS int    `yaml:"gitPrepareTimeoutSec"`
	ShellPreference    string `yaml:"shell"` // auto | bash | sh | pwsh | cmd

	// ConfigPath 记录实际加载的配置文件路径（可能为空）。
	ConfigPath string `yaml:"-"`
}

// Default 返回一份可直接使用的默认配置。
func Default() *Config {
	return &Config{
		Listen:             "127.0.0.1:8080",
		DataDir:            "data",
		WebRoot:            "workspaces",
		TokenTTLHours:      24 * 7,
		MaxConcurrentRuns:  5,
		LogRetentionDays:   30,
		LogMaxBytes:        100 << 20, // 100MB
		HookRateLimit:      60,
		Visibility:         "allReadable",
		GitPrepareTimeoutS: 600,
		ShellPreference:    "auto",
	}
}

// Load 解析命令行参数与配置来源，返回最终生效的配置。
//
// 命令行参数用独立的 FlagSet，避免和调用方的 flag 冲突，也方便测试。
func Load(args []string) (*Config, error) {
	cfg := Default()

	fs := flag.NewFlagSet("deploy-server", flag.ContinueOnError)
	var (
		configPath = fs.String("config", "", "配置文件路径（默认依次尝试 ./deploy-server.yaml、$DATA_DIR/deploy-server.yaml）")
		listen     = fs.String("listen", "", "HTTP 监听地址，如 0.0.0.0:8080")
		dataDir    = fs.String("data-dir", "", "数据目录（数据库/日志/密钥）")
		webRoot    = fs.String("web-root", "", "项目工作目录根")
		serverURL  = fs.String("server-url", "", "服务端对外地址")
	)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	// 先定 dataDir，因为配置文件可能就在 dataDir 下。
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	} else if v := os.Getenv("DEPLOY_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}

	// 找配置文件：显式指定 > 当前目录 > dataDir 目录。
	candidates := []string{}
	if *configPath != "" {
		candidates = append(candidates, *configPath)
	} else {
		candidates = append(candidates,
			"deploy-server.yaml",
			filepath.Join(cfg.DataDir, "deploy-server.yaml"),
		)
	}
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			raw, err := os.ReadFile(p)
			if err != nil {
				return nil, fmt.Errorf("读取配置文件 %s 失败: %w", p, err)
			}
			if err := yaml.Unmarshal(raw, cfg); err != nil {
				return nil, fmt.Errorf("解析配置文件 %s 失败: %w", p, err)
			}
			cfg.ConfigPath = p
			break
		}
		if *configPath != "" {
			return nil, fmt.Errorf("配置文件不存在: %s", p)
		}
	}

	// 环境变量
	applyEnv(cfg)

	// 命令行参数优先级最高
	if *listen != "" {
		cfg.Listen = *listen
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}
	if *webRoot != "" {
		cfg.WebRoot = *webRoot
	}
	if *serverURL != "" {
		cfg.ServerURL = *serverURL
	}

	cfg.normalize()

	// 解析绝对路径，避免以后因为工作目录变化而错位。
	if abs, err := filepath.Abs(cfg.DataDir); err == nil {
		cfg.DataDir = abs
	}
	if abs, err := filepath.Abs(cfg.WebRoot); err == nil {
		cfg.WebRoot = abs
	}
	if cfg.ServerURL == "" {
		host := cfg.Listen
		if strings.HasPrefix(host, "0.0.0.0") || strings.HasPrefix(host, "[::]") {
			host = "127.0.0.1" + host[strings.LastIndex(host, ":"):]
		}
		cfg.ServerURL = "http://" + host
	}

	if err := cfg.ensureDirs(); err != nil {
		return nil, err
	}
	if err := cfg.ensureKeys(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("DEPLOY_LISTEN"); v != "" {
		cfg.Listen = v
	}
	if v := os.Getenv("DEPLOY_WEB_ROOT"); v != "" {
		cfg.WebRoot = v
	}
	if v := os.Getenv("DEPLOY_SERVER_URL"); v != "" {
		cfg.ServerURL = v
	}
	if v := os.Getenv("DEPLOY_JWT_SECRET"); v != "" {
		cfg.JWTSecret = v
	}
	if v := os.Getenv("DEPLOY_SECRET_KEY"); v != "" {
		cfg.SecretKey = v
	}
	if v := os.Getenv("DEPLOY_MAX_CONCURRENT_RUNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxConcurrentRuns = n
		}
	}
	if v := os.Getenv("DEPLOY_VISIBILITY"); v != "" {
		cfg.Visibility = v
	}
}

func (c *Config) normalize() {
	if c.MaxConcurrentRuns <= 0 {
		c.MaxConcurrentRuns = 5
	}
	if c.LogRetentionDays < 0 {
		c.LogRetentionDays = 0
	}
	if c.LogMaxBytes <= 0 {
		c.LogMaxBytes = 100 << 20
	}
	if c.HookRateLimit <= 0 {
		c.HookRateLimit = 60
	}
	if c.TokenTTLHours <= 0 {
		c.TokenTTLHours = 24 * 7
	}
	if c.GitPrepareTimeoutS <= 0 {
		c.GitPrepareTimeoutS = 600
	}
	switch c.Visibility {
	case "ownerOnly", "allReadable":
	default:
		c.Visibility = "allReadable"
	}
	if c.ShellPreference == "" {
		c.ShellPreference = "auto"
	}
}

func (c *Config) ensureDirs() error {
	for _, d := range []string{c.DataDir, c.LogDir(), c.WebRoot} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", d, err)
		}
	}
	return nil
}

// LogDir 是运行日志的落盘目录。
func (c *Config) LogDir() string { return filepath.Join(c.DataDir, "logs") }

// DBPath 是 SQLite 数据库文件路径。
func (c *Config) DBPath() string { return filepath.Join(c.DataDir, "deploy.db") }

// ensureKeys 保证 JWT 与加密密钥存在。
// 明文写在配置文件里很危险，所以默认生成到 dataDir 下的独立文件（0600）。
func (c *Config) ensureKeys() error {
	if c.JWTSecret == "" {
		v, err := loadOrCreateKey(filepath.Join(c.DataDir, "jwt.key"))
		if err != nil {
			return err
		}
		c.JWTSecret = v
	}
	if c.SecretKey == "" {
		v, err := loadOrCreateKey(filepath.Join(c.DataDir, "secret.key"))
		if err != nil {
			return err
		}
		c.SecretKey = v
	}
	return nil
}

func loadOrCreateKey(path string) (string, error) {
	if raw, err := os.ReadFile(path); err == nil {
		if s := strings.TrimSpace(string(raw)); s != "" {
			return s, nil
		}
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	key := base64.RawURLEncoding.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("写入密钥文件 %s 失败: %w", path, err)
	}
	return key, nil
}
