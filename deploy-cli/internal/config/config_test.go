package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadMissingFileReturnsEmptyConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".deploy", "config.yaml")
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Current != DefaultContext {
		t.Fatalf("Current = %q", c.Current)
	}
	if c.Active().Address != "" {
		t.Fatalf("空配置的 server 应为空")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := c.Active()
	ctx.Address = "http://127.0.0.1:8080"
	ctx.Token = "eyJhbGciOi.abc.def"
	ctx.User = "admin"
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	a := got.Active()
	if a.Address != ctx.Address || a.Token != ctx.Token || a.User != ctx.User {
		t.Fatalf("round trip mismatch: %+v", a)
	}
	if got.Current != "default" {
		t.Fatalf("Current = %q", got.Current)
	}
}

func TestSaveFileModeIs0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不做 POSIX 权限位断言")
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	c, _ := Load(path)
	c.Active().Token = "secret"
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("权限 = %o, want 600", perm)
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Fatalf("目录权限 = %o, want 700", perm)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("current: [unclosed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("期望解析错误")
	}
}

func TestMultiContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	c, _ := Load(path)
	c.Active().Address = "http://a"
	c.Active().Token = "ta"
	if err := c.UseContext("prod"); err != nil {
		t.Fatal(err)
	}
	if got := c.Active().Address; got != "http://a" {
		t.Fatalf("新 context 应继承 server，得到 %q", got)
	}
	c.Active().Address = "https://prod.example.com"
	c.Active().Token = "tp"
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	got, _ := Load(path)
	if got.Current != "prod" {
		t.Fatalf("Current = %q", got.Current)
	}
	if got.Active().Token != "tp" {
		t.Fatalf("prod token = %q", got.Active().Token)
	}
	if got.Contexts["default"].Token != "ta" {
		t.Fatalf("default token 丢失: %+v", got.Contexts["default"])
	}
}

func TestGetSet(t *testing.T) {
	c, _ := Load(filepath.Join(t.TempDir(), "config.yaml"))
	if err := c.Set("server", "127.0.0.1:9000/"); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Get("server"); got != "http://127.0.0.1:9000" {
		t.Fatalf("server = %q", got)
	}
	if err := c.Set("token", " tt "); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Get("token"); got != "tt" {
		t.Fatalf("token = %q", got)
	}
	if err := c.Set("user", "bob"); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Get("user"); got != "bob" {
		t.Fatalf("user = %q", got)
	}
	if err := c.Set("current", "staging"); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Get("current"); got != "staging" {
		t.Fatalf("current = %q", got)
	}
	if _, err := c.Get("nope"); err == nil {
		t.Fatal("未知键应报错")
	}
	if err := c.Set("nope", "x"); err == nil {
		t.Fatal("未知键应报错")
	}
}

func TestNormalizeAddress(t *testing.T) {
	cases := map[string]string{
		"http://a:8080/":        "http://a:8080",
		"a:8080":                "http://a:8080",
		" https://x/y/ ":        "https://x/y",
		"":                      "",
		"http://127.0.0.1:8080": "http://127.0.0.1:8080",
	}
	for in, want := range cases {
		if got := NormalizeAddress(in); got != want {
			t.Fatalf("NormalizeAddress(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolvePrecedence(t *testing.T) {
	cfg, _ := Load(filepath.Join(t.TempDir(), "config.yaml"))
	cfg.Active().Address = "http://from-config"
	cfg.Active().Token = "token-config"
	cfg.Active().User = "cfguser"

	env := func(k string) string {
		switch k {
		case EnvAddress:
			return "http://from-env"
		case EnvToken:
			return "token-env"
		}
		return ""
	}

	// 历史名 DEPLOY_SERVER 仍然作为地址生效（优先级低于 DEPLOY_ADDRESS）
	legacyEnv := func(k string) string {
		if k == EnvServer {
			return "http://from-legacy-env"
		}
		return ""
	}
	if s := Resolve("", "", "", legacyEnv, cfg); s.Address != "http://from-legacy-env" || s.AddressSource != "env" {
		t.Fatalf("DEPLOY_SERVER 应仍被接受: %+v", s)
	}

	// flag 最高
	s := Resolve("http://from-flag", "token-flag", "", env, cfg)
	if s.Address != "http://from-flag" || s.Token != "token-flag" {
		t.Fatalf("flag 优先级错误: %+v", s)
	}
	if s.AddressSource != "flag" || s.TokenSource != "flag" {
		t.Fatalf("来源标记错误: %+v", s)
	}

	// 其次 env
	s = Resolve("", "", "", env, cfg)
	if s.Address != "http://from-env" || s.Token != "token-env" {
		t.Fatalf("env 优先级错误: %+v", s)
	}
	if s.AddressSource != "env" || s.TokenSource != "env" {
		t.Fatalf("来源标记错误: %+v", s)
	}

	// 再其次配置文件
	s = Resolve("", "", "", func(string) string { return "" }, cfg)
	if s.Address != "http://from-config" || s.Token != "token-config" {
		t.Fatalf("config 优先级错误: %+v", s)
	}
	if s.User != "cfguser" {
		t.Fatalf("user = %q", s.User)
	}

	// 最后默认值
	empty, _ := Load(filepath.Join(t.TempDir(), "config.yaml"))
	s = Resolve("", "", "", func(string) string { return "" }, empty)
	if s.Address != DefaultAddress {
		t.Fatalf("默认 server = %q, want %q", s.Address, DefaultAddress)
	}
	if s.Token != "" || s.TokenSource != "none" {
		t.Fatalf("默认 token 应为空: %+v", s)
	}
}

func TestResolveNilConfigAndNilGetenv(t *testing.T) {
	s := Resolve("  ", "  ", "", nil, nil)
	if s.Address != DefaultAddress || s.Token != "" {
		t.Fatalf("got %+v", s)
	}
}

func TestDefaultPathUsesEnvOverride(t *testing.T) {
	t.Setenv(EnvConfig, filepath.Join("C:", "tmp", "c.yaml"))
	p, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(p, "c.yaml") {
		t.Fatalf("path = %q", p)
	}
}

func TestActiveCreatesMissingContext(t *testing.T) {
	c := &Config{Current: "ghost"}
	if c.Active() == nil {
		t.Fatal("Active 应自动创建 context")
	}
	if _, ok := c.Contexts["ghost"]; !ok {
		t.Fatal("contexts 未写入")
	}
}

// --- 键就是名字：改名 = 改键，current 跟着走；不再有独立的 name 标签 ---

func TestRenameServerMovesKeyAndCurrent(t *testing.T) {
	c, _ := Load(filepath.Join(t.TempDir(), "config.yaml"))
	addr, token := "http://a", "ta"
	if _, err := c.UpsertServer("971", &addr, &token, nil, nil, true); err != nil {
		t.Fatalf("UpsertServer: %v", err)
	}
	if err := c.RenameServer("971", "97"); err != nil {
		t.Fatalf("RenameServer: %v", err)
	}
	if _, ok := c.Contexts["971"]; ok {
		t.Fatalf("旧键应消失，contexts=%v", c.ContextNames())
	}
	ctx, ok := c.Contexts["97"]
	if !ok {
		t.Fatalf("新键应存在，contexts=%v", c.ContextNames())
	}
	if ctx.Address != "http://a" || ctx.Token != "ta" {
		t.Fatalf("改名应保留字段: %+v", ctx)
	}
	if c.Current != "97" {
		t.Fatalf("current 应跟到新键，得到 %q", c.Current)
	}
}

func TestRenameServerRejectsCollisionAndEmpty(t *testing.T) {
	c, _ := Load(filepath.Join(t.TempDir(), "config.yaml"))
	if _, err := c.UpsertServer("971", nil, nil, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpsertServer("97", nil, nil, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	if err := c.RenameServer("971", "97"); err == nil {
		t.Fatal("改成已存在的名字应报错")
	}
	if err := c.RenameServer("971", ""); err == nil {
		t.Fatal("改成空名应报错")
	}
	if err := c.RenameServer("ghost", "x"); err == nil {
		t.Fatal("对不存在的名字应报错")
	}
	if err := c.RenameServer("971", "971"); err != nil {
		t.Fatalf("同名应是无操作: %v", err)
	}
}

func TestConfigDoesNotWriteNameField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	c, _ := Load(path)
	addr := "http://a"
	if _, err := c.UpsertServer("97", &addr, nil, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "name:") {
		t.Fatalf("配置不应再写 name 标签:\n%s", raw)
	}
}

// 老配置文件即使带遗留的 name: 标签，加载后也只看键（name 被忽略）。
func TestLegacyNameLabelIsIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	legacy := "current: 971\ncontexts:\n    971:\n        name: \"97\"\n        address: http://192.168.31.97:10055\n        token: eyJhbGci\n"
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	svcs := c.Servers(func(s string) string { return s })
	if len(svcs) != 1 {
		t.Fatalf("期望 1 台服务器，得到 %d", len(svcs))
	}
	if svcs[0].Key != "971" || svcs[0].Name != "971" {
		t.Fatalf("键才是名字，期望 key=name=971，得到 key=%q name=%q", svcs[0].Key, svcs[0].Name)
	}
	if svcs[0].Address != "http://192.168.31.97:10055" {
		t.Fatalf("address = %q", svcs[0].Address)
	}
}

// 老配置文件里的地址键是 server:：读取时必须仍然生效（见 Context.UnmarshalYAML）。
func TestLegacyConfigWithoutNameStillLists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	legacy := "current: default\ncontexts:\n    default:\n        server: http://192.168.31.97:10055\n        token: eyJhbGci\n        user: admin\n"
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	svcs := c.Servers(func(s string) string { return s })
	if len(svcs) != 1 {
		t.Fatalf("期望 1 台服务器，得到 %d", len(svcs))
	}
	if svcs[0].Name != "default" {
		t.Fatalf("名字应是键 default，得到 %q", svcs[0].Name)
	}
	if svcs[0].Address != "http://192.168.31.97:10055" {
		t.Fatalf("server = %q", svcs[0].Address)
	}
}

// 写出的配置文件用 address: 键；旧文件的 server: 键仍能读（兼容逻辑见 Context.UnmarshalYAML）。
func TestSaveWritesAddressKeyNotServerKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	addr, token := "http://127.0.0.1:8080", "dk_abc"
	if _, err := c.UpsertServer("97", &addr, &token, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "address: http://127.0.0.1:8080") {
		t.Fatalf("应写出 address: 键:\n%s", b)
	}
	if strings.Contains(string(b), "server:") {
		t.Fatalf("不应再写出 server: 键:\n%s", b)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, ok := back.GetServer("97")
	if !ok || ctx.Address != "http://127.0.0.1:8080" || ctx.Token != "dk_abc" {
		t.Fatalf("round-trip 失败: %+v", ctx)
	}
}
