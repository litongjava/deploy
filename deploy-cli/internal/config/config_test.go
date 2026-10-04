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
	if c.Active().Server != "" {
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
	ctx.Server = "http://127.0.0.1:8080"
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
	if a.Server != ctx.Server || a.Token != ctx.Token || a.User != ctx.User {
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
	c.Active().Server = "http://a"
	c.Active().Token = "ta"
	if err := c.UseContext("prod"); err != nil {
		t.Fatal(err)
	}
	if got := c.Active().Server; got != "http://a" {
		t.Fatalf("新 context 应继承 server，得到 %q", got)
	}
	c.Active().Server = "https://prod.example.com"
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

func TestNormalizeServer(t *testing.T) {
	cases := map[string]string{
		"http://a:8080/":        "http://a:8080",
		"a:8080":                "http://a:8080",
		" https://x/y/ ":        "https://x/y",
		"":                      "",
		"http://127.0.0.1:8080": "http://127.0.0.1:8080",
	}
	for in, want := range cases {
		if got := NormalizeServer(in); got != want {
			t.Fatalf("NormalizeServer(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolvePrecedence(t *testing.T) {
	cfg, _ := Load(filepath.Join(t.TempDir(), "config.yaml"))
	cfg.Active().Server = "http://from-config"
	cfg.Active().Token = "token-config"
	cfg.Active().User = "cfguser"

	env := func(k string) string {
		switch k {
		case EnvServer:
			return "http://from-env"
		case EnvToken:
			return "token-env"
		}
		return ""
	}

	// flag 最高
	s := Resolve("http://from-flag", "token-flag", env, cfg)
	if s.Server != "http://from-flag" || s.Token != "token-flag" {
		t.Fatalf("flag 优先级错误: %+v", s)
	}
	if s.ServerSource != "flag" || s.TokenSource != "flag" {
		t.Fatalf("来源标记错误: %+v", s)
	}

	// 其次 env
	s = Resolve("", "", env, cfg)
	if s.Server != "http://from-env" || s.Token != "token-env" {
		t.Fatalf("env 优先级错误: %+v", s)
	}
	if s.ServerSource != "env" || s.TokenSource != "env" {
		t.Fatalf("来源标记错误: %+v", s)
	}

	// 再其次配置文件
	s = Resolve("", "", func(string) string { return "" }, cfg)
	if s.Server != "http://from-config" || s.Token != "token-config" {
		t.Fatalf("config 优先级错误: %+v", s)
	}
	if s.User != "cfguser" {
		t.Fatalf("user = %q", s.User)
	}

	// 最后默认值
	empty, _ := Load(filepath.Join(t.TempDir(), "config.yaml"))
	s = Resolve("", "", func(string) string { return "" }, empty)
	if s.Server != DefaultServer {
		t.Fatalf("默认 server = %q, want %q", s.Server, DefaultServer)
	}
	if s.Token != "" || s.TokenSource != "none" {
		t.Fatalf("默认 token 应为空: %+v", s)
	}
}

func TestResolveNilConfigAndNilGetenv(t *testing.T) {
	s := Resolve("  ", "  ", nil, nil)
	if s.Server != DefaultServer || s.Token != "" {
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
