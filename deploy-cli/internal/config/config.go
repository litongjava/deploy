// Package config 负责 ~/.deploy/config.yaml 的读写与配置优先级解析。
//
// 优先级（高 → 低）：命令行 flag > 环境变量 > 配置文件 > 默认值。
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// 默认值。
const (
	// DefaultContext 是未显式切换时使用的 context 名。
	DefaultContext = "default"
	// DefaultAddress 是没有任何配置时的兜底服务端地址。
	DefaultAddress = "http://127.0.0.1:10055"
	// EnvAddress / EnvToken / EnvConfig 是支持的环境变量。
	EnvAddress = "DEPLOY_ADDRESS"
	// EnvServer 是 EnvAddress 的历史名（早期版本用它表示地址），仍然接受，但优先级更低。
	EnvServer = "DEPLOY_SERVER"
	EnvToken  = "DEPLOY_TOKEN"
	EnvConfig = "DEPLOY_CONFIG"
)

// Context 是一台「服务器」的地址 + 凭证组合。
//
// 配置文件里 contexts 的**键就是服务器的名字**：--server 用它定位，
// `server update <旧名> --name <新名>` 改的就是这个键。
// 名字只有一个，不再有独立的显示标签；老配置里遗留的 name: 字段读取时忽略，
// 重新保存后自然消失（见 UnmarshalYAML）。
type Context struct {
	// Address 是服务端地址；早期字段名是 server，读取时仍然兼容（见 UnmarshalYAML）。
	Address string `yaml:"address,omitempty"`
	Token   string `yaml:"token,omitempty"`
	User    string `yaml:"user,omitempty"`
	Comment string `yaml:"comment,omitempty"`
}

// UnmarshalYAML 读取一台服务器，并兼容早期配置文件里的 `server:` 键。
//
// 早期版本把地址字段写成 `server:`；现在「服务器」指的是整条配置，地址字段叫 `address:`。
// 老文件即使不迁移也不会丢地址：这里读一次旧键补上。
func (c *Context) UnmarshalYAML(value *yaml.Node) error {
	var raw struct {
		Address string `yaml:"address"`
		Server  string `yaml:"server"` // legacy
		Token   string `yaml:"token"`
		User    string `yaml:"user"`
		Comment string `yaml:"comment"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	// 遗留的 name: 标签不在这里解码 —— 键才是名字，yaml 会直接忽略它。
	c.Token, c.User, c.Comment = raw.Token, raw.User, raw.Comment
	c.Address = raw.Address
	if c.Address == "" {
		c.Address = raw.Server
	}
	return nil
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
		return "", fmt.Errorf("cannot determine the user home directory: %w", err)
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
		return nil, fmt.Errorf("cannot read config file %s: %w", path, err)
	}
	if len(b) == 0 {
		return c, nil
	}
	if err := yaml.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("cannot parse config file %s (invalid YAML): %w", path, err)
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

// RenameServer 把服务器 oldKey 改名为 newKey —— 键就是名字，这是唯一的改名入口。
//
// 改名会把地址/token/用户名/备注整条搬过去；若改的正是当前服务器，current 一并
// 指向新名字，避免留下「current 指向一个不存在的键」的坏状态。
// newKey 已存在、为空或非法时报错，由调用方翻译成用法错误。
func (c *Config) RenameServer(oldKey, newKey string) error {
	oldKey = strings.TrimSpace(oldKey)
	newKey = strings.TrimSpace(newKey)
	if c.Contexts == nil {
		return fmt.Errorf("server %q does not exist", oldKey)
	}
	ctx, ok := c.Contexts[oldKey]
	if !ok || ctx == nil {
		return fmt.Errorf("server %q does not exist", oldKey)
	}
	if err := ValidateServerName(newKey); err != nil {
		return err
	}
	if newKey == oldKey {
		return nil
	}
	if _, exists := c.Contexts[newKey]; exists {
		return fmt.Errorf("server %q already exists; remove it first or pick another name", newKey)
	}
	delete(c.Contexts, oldKey)
	c.Contexts[newKey] = ctx
	if c.Current == oldKey {
		c.Current = newKey
	}
	return nil
}

// ContextNames 返回全部 context 名。
func (c *Config) ContextNames() []string {
	names := make([]string, 0, len(c.Contexts))
	for k := range c.Contexts {
		names = append(names, k)
	}
	return names
}

// UseContext 切换当前服务器；不存在时以当前服务器的地址为模板新建。
func (c *Config) UseContext(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("server key must not be empty")
	}
	if c.Contexts == nil {
		c.Contexts = map[string]*Context{}
	}
	if _, ok := c.Contexts[name]; !ok {
		tmpl := c.Active()
		c.Contexts[name] = &Context{Address: tmpl.Address}
	}
	c.Current = name
	return nil
}

// ValidateServerName 校验服务器键。
//
// 允许中文与点/短横线（使用者会写「测试机」），但禁止空白与冒号 ——
// 前者会让 `--server` 的命令行传参要靠引号、后者会和 URL 的 scheme 混淆。
func ValidateServerName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("server key must not be empty")
	}
	if len([]rune(name)) > 64 {
		return errors.New("server key must be at most 64 characters")
	}
	if strings.ContainsAny(name, " \t\r\n:/\\") {
		return fmt.Errorf("server key must not contain spaces, colons or slashes: %q", name)
	}
	return nil
}

// Server 是一台服务器的展示视图（token 打码后的值由调用方决定）。
type Server struct {
	// Key 与 Name 同值：contexts 下的键就是服务器的名字。
	// 两个字段都保留，是为了不破坏已有的 JSON 消费方。
	Key     string `json:"key" yaml:"key"`
	Name    string `json:"name" yaml:"name"`
	Address string `json:"address" yaml:"address"`
	User    string `json:"user" yaml:"user"`
	Comment string `json:"comment" yaml:"comment"`
	Token   string `json:"token" yaml:"token"` // 调用方负责打码，绝不写明文
	Active  bool   `json:"active" yaml:"active"`
}

// Servers 返回全部服务器，按键排序；active 标记当前默认的那一台。
// mask 用于处理 token（测试里可传恒等函数以断言真实值）。
//
// 会跳过「空的 default」：配置文件不存在时 Load 会造一个 current=default 的空壳，
// 把它列进服务器清单只会让使用者以为自己配过一台叫 default 的服务器。
// 一旦 default 里真的填了地址或 token，就照常列出。
func (c *Config) Servers(mask func(string) string) []Server {
	names := c.ContextNames()
	sort.Strings(names)
	if mask == nil {
		mask = func(s string) string { return s }
	}
	out := make([]Server, 0, len(names))
	for _, n := range names {
		ctx := c.Contexts[n]
		if n == DefaultContext && isEmptyContext(ctx) {
			continue
		}
		// 键就是名字：Key 与 Name 同值。
		srv := Server{Key: n, Name: n, Active: n == c.Current}
		if ctx != nil {
			srv.Address = ctx.Address
			srv.User = ctx.User
			srv.Comment = ctx.Comment
			srv.Token = mask(ctx.Token)
		}
		out = append(out, srv)
	}
	return out
}

// isEmptyContext 报告一个 context 是否什么都没配。
func isEmptyContext(ctx *Context) bool {
	if ctx == nil {
		return true
	}
	return ctx.Address == "" && ctx.Token == "" && ctx.User == "" && ctx.Comment == ""
}

// GetServer 按名字取一台服务器；第二个返回值为 false 表示不存在。
func (c *Config) GetServer(name string) (*Context, bool) {
	ctx, ok := c.Contexts[strings.TrimSpace(name)]
	if !ok || ctx == nil {
		return nil, false
	}
	return ctx, true
}

// UpsertServer 新建或更新一台服务器，并把它设为当前服务器。
//
// 指针参数区分「不改」与「改成空」：nil 表示保持原值，指向空串表示清空。
// 这样 `deploy server add prod -a URL -t TOKEN` 不会因为没传 --user 而清掉用户名。
//
// 改名不在这里做 —— 键与值是两件事，改名请用 RenameServer。
func (c *Config) UpsertServer(srvKey string, address, token, user, comment *string, setCurrent bool) (*Context, error) {
	srvKey = strings.TrimSpace(srvKey)
	if err := ValidateServerName(srvKey); err != nil {
		return nil, err
	}
	if c.Contexts == nil {
		c.Contexts = map[string]*Context{}
	}
	ctx, ok := c.Contexts[srvKey]
	if !ok || ctx == nil {
		ctx = &Context{}
		c.Contexts[srvKey] = ctx
	}
	if address != nil {
		ctx.Address = NormalizeAddress(*address)
	}
	if token != nil {
		ctx.Token = strings.TrimSpace(*token)
	}
	if user != nil {
		ctx.User = strings.TrimSpace(*user)
	}
	if comment != nil {
		ctx.Comment = strings.TrimSpace(*comment)
	}
	if setCurrent {
		c.Current = srvKey
	}
	return ctx, nil
}

// RemoveServer 删除一台服务器；删的是当前服务器时，把 current 挪到剩下的第一个。
func (c *Config) RemoveServer(srvKey string) error {
	srvKey = strings.TrimSpace(srvKey)
	if _, ok := c.Contexts[srvKey]; !ok {
		return fmt.Errorf("server %q does not exist", srvKey)
	}
	delete(c.Contexts, srvKey)
	if c.Current != srvKey {
		return nil
	}
	names := c.ContextNames()
	sort.Strings(names)
	if len(names) == 0 {
		c.Current = DefaultContext
		return nil
	}
	c.Current = names[0]
	return nil
}

// Save 原子写入配置文件：目录 0700、文件 0600（Windows 上尽力而为）。
func (c *Config) Save() error {
	if c.path == "" {
		return errors.New("config path is empty")
	}
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return configWriteError(c.path, err)
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("cannot serialize config: %w", err)
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return configWriteError(c.path, err)
	}
	// Windows 上 os.Chmod 基本无效，但仍尽力而为；Unix 上确保 0600。
	_ = os.Chmod(tmp, 0o600)
	if err := os.Rename(tmp, c.path); err != nil {
		_ = os.Remove(tmp)
		return configWriteError(c.path, err)
	}
	_ = os.Chmod(c.path, 0o600)
	return nil
}

// configWriteError 把「配置文件写不进去」翻译成一句英文的、可操作的建议。
//
// 实机场景：受管环境（沙箱、受限令牌、只读 home）不允许进程写 %USERPROFILE%/.deploy ——
// MkdirAll 能过、WriteFile 报 Access is denied。此时唯一可靠的办法是用 DEPLOY_CONFIG
// 把配置指到可写目录，所以错误信息里必须直接给出这个办法。
func configWriteError(target string, err error) error {
	return fmt.Errorf("cannot write config file %s: %w (point %s at a writable path, "+
		"e.g. set %s=D:/path/to/config.yaml, then retry)", target, err, EnvConfig, EnvConfig)
}

// 支持的配置键。
//
// address 是服务端地址；server 是它的历史名（早期配置项就叫 server），仍然接受。
const (
	KeyAddress      = "address"
	KeyServerLegacy = "server"
	KeyToken        = "token"
	KeyUser         = "user"
	KeyCurrent      = "current"
)

// Get 读取配置项的当前有效值。
// 特殊键 current 返回当前服务器键；address/token/user 读当前服务器。
func (c *Config) Get(key string) (string, error) {
	switch k := strings.ToLower(strings.TrimSpace(key)); k {
	case KeyCurrent:
		if c.Current == "" {
			return DefaultContext, nil
		}
		return c.Current, nil
	case KeyAddress, KeyServerLegacy:
		return c.Active().Address, nil
	case KeyToken:
		return c.Active().Token, nil
	case KeyUser:
		return c.Active().User, nil
	case "path":
		return c.path, nil
	default:
		return "", fmt.Errorf("unknown config key %q (available: address, server, token, user, current)", key)
	}
}

// Set 写入配置项并返回是否成功。
func (c *Config) Set(key, value string) error {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case KeyCurrent:
		return c.UseContext(value)
	case KeyAddress, KeyServerLegacy:
		c.Active().Address = NormalizeAddress(value)
		return nil
	case KeyToken:
		c.Active().Token = strings.TrimSpace(value)
		return nil
	case KeyUser:
		c.Active().User = strings.TrimSpace(value)
		return nil
	default:
		return fmt.Errorf("unknown config key %q (available: address, server, token, user, current)", key)
	}
}

// NormalizeAddress 规范化服务端地址：补 scheme、去掉结尾斜杠。
func NormalizeAddress(s string) string {
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
	// Server 是本次实际使用的服务器（`--server` 指定，或配置文件里的 current）。
	Server string
	// Address 是最终使用的服务端地址。
	Address string
	Token   string
	User    string
	// 来源说明，仅用于 --verbose 与 whoami 展示。
	AddressSource string
	TokenSource   string
	// ServerSource 说明服务器是显式指定还是取自配置（flag / config）。
	ServerSource string
	// ServerMissing 为 true 表示 `--server` 点名的服务器在配置文件里不存在。
	// 这里不直接报错，交给调用方决定：读取类命令应报错，`server add` 属于首次创建。
	ServerMissing bool
}

// Getenv 抽象环境变量读取，便于测试。
type Getenv func(string) string

// Resolve 按 flag > 环境变量 > 配置文件 > 默认值 的顺序解析。
//
// server 非空时改用该服务器（`deploy --server <key> run ...`），
// 这是需求里「部署时通过命令行参数指定服务器」的落点：
// 显式指定 > 配置文件里的 current。指定的服务器不存在时按空配置继续，
// 由调用方（AuthClient 等）报出可读的错误。
func Resolve(flagAddress, flagToken, server string, getenv Getenv, cfg *Config) Settings {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	var s Settings
	var active *Context
	server = strings.TrimSpace(server)
	if cfg != nil {
		if server != "" {
			if ctx, ok := cfg.GetServer(server); ok {
				active = ctx
				s.Server, s.ServerSource = server, "flag"
			} else {
				active = &Context{}
				s.Server, s.ServerSource, s.ServerMissing = server, "flag", true
			}
		} else {
			active = cfg.Active()
			s.Server, s.ServerSource = cfg.Current, "config"
		}
	} else {
		active = &Context{}
		if server != "" {
			s.Server, s.ServerSource, s.ServerMissing = server, "flag", true
		}
	}

	switch {
	case strings.TrimSpace(flagAddress) != "":
		s.Address, s.AddressSource = NormalizeAddress(flagAddress), "flag"
	case strings.TrimSpace(getenv(EnvAddress)) != "":
		s.Address, s.AddressSource = NormalizeAddress(getenv(EnvAddress)), "env"
	case strings.TrimSpace(getenv(EnvServer)) != "":
		// 历史名 DEPLOY_SERVER，优先级低于 DEPLOY_ADDRESS。
		s.Address, s.AddressSource = NormalizeAddress(getenv(EnvServer)), "env"
	case strings.TrimSpace(active.Address) != "":
		s.Address, s.AddressSource = NormalizeAddress(active.Address), "config"
	default:
		s.Address, s.AddressSource = DefaultAddress, "default"
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
