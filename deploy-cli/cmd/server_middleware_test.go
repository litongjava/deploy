package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"deploy-cli/internal/config"
	"deploy-cli/internal/exitcode"
)

// ---------- 服务器（名称 + 地址 + token 存本地文件，--server 指定） ----------

func TestServerAddListUseRemove(t *testing.T) {
	isolateConfig(t)

	// 1) 新增两个服务器：add 默认把新服务器设为当前
	code, out, errw := exec(t, "server", "add", "test",
		"-a", "http://192.168.31.97:10055", "-t", "dk_test_token_123456", "-u", "admin",
		"--comment", "测试机")
	if code != exitcode.OK {
		t.Fatalf("server add exit=%d out=%q err=%q", code, out, errw)
	}
	code, out, _ = exec(t, "server", "add", "prod", "-a", "http://10.0.0.5:10055", "-t", "dk_prod_token_abcdef")
	if code != exitcode.OK {
		t.Fatalf("server add prod exit=%d", code)
	}

	// 2) list：必须同时看到两个，且 token 打码（绝不出明文）
	code, out, _ = exec(t, "server", "list", "--json")
	if code != exitcode.OK {
		t.Fatalf("server list exit=%d", code)
	}
	if strings.Contains(out, "dk_test_token_123456") || strings.Contains(out, "dk_prod_token_abcdef") {
		t.Fatalf("server list 泄露了 token 明文: %q", out)
	}
	var list struct {
		Current string `json:"current"`
		Servers []struct {
			Name    string `json:"name"`
			Address string `json:"address"`
			Active  bool   `json:"active"`
		} `json:"servers"`
	}
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("server list --json 不是合法 JSON: %v (%q)", err, out)
	}
	if len(list.Servers) != 2 {
		t.Fatalf("应有 2 个服务器，实际 %d（%q）", len(list.Servers), out)
	}
	if list.Current != "prod" {
		t.Fatalf("add 之后当前服务器应为最后一个新增的，实际 %q", list.Current)
	}

	// 3) 服务器键非法要提前拒绝（会作为 --server 的值传参）
	for _, bad := range []string{"bad name", "a:b", "x/y"} {
		if code, _, _ := exec(t, "server", "add", bad, "-a", "http://1.2.3.4:10055"); code != exitcode.Usage {
			t.Fatalf("非法服务器键 %q 应返回用法错误，得到 %d", bad, code)
		}
	}

	// 4) get：能取到具体服务器；不存在的服务器报用法错误
	code, out, _ = exec(t, "server", "get", "test", "--json")
	if code != exitcode.OK || !strings.Contains(out, "192.168.31.97") {
		t.Fatalf("server get exit=%d out=%q", code, out)
	}
	if code, _, _ := exec(t, "server", "get", "ghost"); code != exitcode.Usage {
		t.Fatalf("不存在的服务器应返回用法错误，得到 %d", code)
	}

	// 5) use：切回 test
	if code, _, _ := exec(t, "server", "use", "test"); code != exitcode.OK {
		t.Fatal("server use failed")
	}
	code, out, _ = exec(t, "server", "current")
	if code != exitcode.OK || strings.TrimSpace(out) != "test" {
		t.Fatalf("current = %q", out)
	}

	// 6) update：只改传了的字段
	if code, _, _ := exec(t, "server", "update", "test", "-t", "dk_new_token_value"); code != exitcode.OK {
		t.Fatal("server update failed")
	}
	code, out, _ = exec(t, "server", "get", "test", "--json")
	if code != exitcode.OK {
		t.Fatalf("server get exit=%d", code)
	}
	var got struct {
		Address string `json:"address"`
		User    string `json:"user"`
		Comment string `json:"comment"`
	}
	_ = json.Unmarshal([]byte(out), &got)
	if got.Address != "http://192.168.31.97:10055" {
		t.Fatalf("只改 token 不应动地址，实际 %q", got.Address)
	}
	if got.User != "admin" || got.Comment != "测试机" {
		t.Fatalf("只改 token 不应动用户/备注，实际 %+v", got)
	}
	// 一个新字段都不传 → 用法错误，避免「什么都没改却报成功」
	if code, _, _ := exec(t, "server", "update", "test"); code != exitcode.Usage {
		t.Fatalf("什么都不改应返回用法错误，得到 %d", code)
	}

	// 7) remove：非交互必须 --yes
	if code, _, _ := exec(t, "server", "remove", "prod"); code != exitcode.Usage {
		t.Fatalf("非交互 remove 应要求 --yes，得到 %d", code)
	}
	if code, _, _ := exec(t, "server", "remove", "prod", "--yes"); code != exitcode.OK {
		t.Fatal("server remove --yes failed")
	}
	code, out, _ = exec(t, "server", "list", "--json")
	if code != exitcode.OK || strings.Contains(out, "prod") {
		t.Fatalf("删除后不应再有 prod: %q", out)
	}
}

// --server 必须真的把请求打到对应的服务器上（这是本需求的核心用法）。
func TestServerFlagSelectsServer(t *testing.T) {
	isolateConfig(t)

	// 两个「服务器」：一个真服务器，一个指向必然连不上的地址。
	f := &fakeAPI{}
	srv := startFake(t, f)
	if code, _, _ := exec(t, "server", "add", "good", "-a", srv.URL, "-t", f.token, "-u", "admin"); code != exitcode.OK {
		t.Fatal("server add good failed")
	}
	if code, _, _ := exec(t, "server", "add", "bad", "-a", "http://127.0.0.1:1", "-t", "x"); code != exitcode.OK {
		t.Fatal("server add bad failed")
	}

	// 当前服务器是 bad（add 会设为当前），显式 --server good 应绕过它
	code, out, errw := exec(t, "--server", "good", "whoami", "--json")
	if code != exitcode.OK {
		t.Fatalf("--server good 应能用，exit=%d out=%q err=%q", code, out, errw)
	}
	if f.lastAuthHeader != f.token {
		t.Fatalf("Authorization = %q，应使用 good 服务器保存的 token", f.lastAuthHeader)
	}

	// 反向确认：不加 --server 时用的是 bad，应当连不上（退出码 4）
	if code, _, _ = exec(t, "whoami"); code != exitcode.Network {
		t.Fatalf("默认服务器是 bad，应返回网络错误，得到 %d", code)
	}

	// 点名一个不存在的服务器：必须报用法错误并提示怎么建，而不是去连 default 地址
	code, _, errw = exec(t, "--server", "ghost", "whoami")
	if code != exitcode.Usage {
		t.Fatalf("不存在的服务器应返回用法错误，得到 %d（%s）", code, errw)
	}
	if !strings.Contains(errw, "ghost") {
		t.Fatalf("错误信息应包含服务器键，实际 %q", errw)
	}
}

// 配置文件结构：服务器键是 contexts 的键，地址与 token 落在文件里。
func TestServerPersistedToConfigFile(t *testing.T) {
	path := isolateConfig(t)
	if code, _, _ := exec(t, "server", "add", "test",
		"-a", "192.168.31.97:10055", "-t", "dk_abc123456789", "-u", "admin"); code != exitcode.OK {
		t.Fatal("server add failed")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("配置文件应已创建: %v", err)
	}
	body := string(raw)
	// 地址会被规范化补上 scheme
	if !strings.Contains(body, "http://192.168.31.97:10055") {
		t.Fatalf("配置文件里应有补全 scheme 的地址:\n%s", body)
	}
	if !strings.Contains(body, "test:") {
		t.Fatalf("配置文件里应以服务器键作为键:\n%s", body)
	}
	// token 必须真的落盘（否则下次还得重新加），但文件权限要收紧
	if !strings.Contains(body, "dk_abc123456789") {
		t.Fatalf("token 应保存在本地文件里:\n%s", body)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 && os.PathSeparator != '\\' {
		t.Fatalf("配置文件权限过宽: %v", info.Mode().Perm())
	}
}

// ---------- 中间件 ----------

// middlewareFake 在 fakeAPI 的基础上补上中间件接口。
func middlewareFake(t *testing.T, f *fakeAPI) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	base := f.handler()
	mux.Handle("/", base)
	mux.HandleFunc("GET /api/v1/system/middleware", func(w http.ResponseWriter, r *http.Request) {
		if !f.authorized(w, r) {
			return
		}
		writeOK(w, map[string]any{
			"supported": true, "platform": "linux/amd64",
			"dockerAvailable": true, "dockerVersion": "Docker version 27.1.1, build 6312585",
			"components": []map[string]any{
				{"key": "docker", "name": "Docker（容器运行时）", "installed": true, "running": true,
					"modes": []string{"native"}, "defaultMode": "native", "mode": "native", "effectiveMode": "native",
					"nativeSupported": true, "version": "Docker version 27.1.1", "path": "/usr/bin/docker",
					"fields": []map[string]any{}},
				{"key": "redis", "name": "Redis", "installed": false, "running": false,
					"modes": []string{"docker", "native"}, "defaultMode": "docker", "mode": "docker",
					"effectiveMode": "docker", "nativeSupported": true,
					"path": "将创建容器 deploy-mw-redis，数据卷 deploy-mw-redis-data",
					"fields": []map[string]any{
						{"key": "REDIS_VERSION", "label": "版本", "default": "7", "value": ""},
						{"key": "REDIS_PORT", "label": "端口", "default": "6379", "value": "6380"},
						{"key": "REDIS_PASSWORD", "label": "密码", "default": "", "value": "", "secret": true, "set": true},
					}},
			},
			"missing": []string{"redis"}, "running": []string{"docker"},
			"actions":    []string{"install", "start", "stop", "restart", "upgrade", "logs", "remove"},
			"projectKey": "system-middleware",
		})
	})
	mux.HandleFunc("POST /api/v1/system/middleware/install", func(w http.ResponseWriter, r *http.Request) {
		if !f.authorized(w, r) {
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.lastCreateBody = body
		writeOK(w, map[string]any{
			"runId": 501, "buildNumber": 7, "status": "queued",
			"projectKey": "system-middleware", "action": body["action"], "dryRun": body["dryRun"],
			"components": body["components"], "logUrl": "/api/v1/runs/501/log",
		})
	})
	mux.HandleFunc("PUT /api/v1/system/middleware/{key}", func(w http.ResponseWriter, r *http.Request) {
		if !f.authorized(w, r) {
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.lastCreateBody = body
		writeOK(w, map[string]any{
			"key": r.PathValue("key"), "mode": body["mode"],
			"fields": []map[string]any{{"key": "REDIS_PORT", "value": "6380"}},
		})
	})
	return mux
}

func TestMiddlewareListAndGet(t *testing.T) {
	isolateConfig(t)
	f := &fakeAPI{}
	srv := startFakeWith(t, f, middlewareFake(t, f))
	if code, _, _ := exec(t, "server", "add", "test", "-a", srv.URL, "-t", f.token); code != exitcode.OK {
		t.Fatal("server add failed")
	}

	// list --json：字段可解析、缺失项可读
	code, out, _ := exec(t, "middleware", "list", "--json")
	if code != exitcode.OK {
		t.Fatalf("middleware list exit=%d out=%q", code, out)
	}
	var list struct {
		DockerAvailable bool     `json:"dockerAvailable"`
		Missing         []string `json:"missing"`
		Components      []struct {
			Key             string   `json:"key"`
			Modes           []string `json:"modes"`
			NativeSupported bool     `json:"nativeSupported"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("不是合法 JSON: %v (%q)", err, out)
	}
	if !list.DockerAvailable || len(list.Missing) != 1 || list.Missing[0] != "redis" {
		t.Fatalf("list = %+v", list)
	}

	// get：密码只显示「已设置」，绝不出明文
	code, out, _ = exec(t, "middleware", "get", "redis")
	if code != exitcode.OK {
		t.Fatalf("middleware get exit=%d", code)
	}
	if !strings.Contains(out, "set (not shown)") {
		t.Fatalf("密码参数应显示为已设置: %q", out)
	}
	if !strings.Contains(out, "6380") {
		t.Fatalf("应显示已保存的端口: %q", out)
	}
	if code, _, _ := exec(t, "middleware", "get", "ghost"); code != exitcode.Usage {
		t.Fatalf("未知中间件应返回用法错误，得到 %d", code)
	}

	// fields：可列出全部参数
	code, out, _ = exec(t, "middleware", "fields", "--json")
	if code != exitcode.OK || !strings.Contains(out, "REDIS_PORT") {
		t.Fatalf("middleware fields exit=%d out=%q", code, out)
	}
}

// 每个动作都要能非交互触发，并把正确的 action/mode/组件传给服务器端。
func TestMiddlewareActionsBuildCorrectRequest(t *testing.T) {
	isolateConfig(t)
	f := &fakeAPI{}
	srv := startFakeWith(t, f, middlewareFake(t, f))
	if code, _, _ := exec(t, "server", "add", "test", "-a", srv.URL, "-t", f.token); code != exitcode.OK {
		t.Fatal("server add failed")
	}

	cases := []struct {
		args       []string
		wantAction string
		wantKeys   []string
		wantMode   string
		wantDryRun bool
	}{
		{[]string{"middleware", "install", "redis", "--no-follow"}, "install", []string{"redis"}, "", false},
		{[]string{"middleware", "install", "redis", "postgresql", "--no-follow"}, "install", []string{"redis", "postgresql"}, "", false},
		{[]string{"middleware", "install", "redis", "-m", "native", "--no-follow"}, "install", []string{"redis"}, "native", false},
		{[]string{"middleware", "install", "redis", "--dry-run", "--no-follow"}, "install", []string{"redis"}, "", true},
		{[]string{"middleware", "start", "redis", "--no-follow"}, "start", []string{"redis"}, "", false},
		{[]string{"middleware", "stop", "redis", "--no-follow"}, "stop", []string{"redis"}, "", false},
		{[]string{"middleware", "restart", "redis", "--no-follow"}, "restart", []string{"redis"}, "", false},
		{[]string{"middleware", "logs", "redis", "--no-follow"}, "logs", []string{"redis"}, "", false},
	}
	for _, c := range cases {
		f.lastCreateBody = nil
		code, out, errw := exec(t, append(c.args, "--json")...)
		if code != exitcode.OK {
			t.Fatalf("%v exit=%d out=%q err=%q", c.args, code, out, errw)
		}
		if f.lastCreateBody == nil {
			t.Fatalf("%v 没有发出请求", c.args)
		}
		if got, _ := f.lastCreateBody["action"].(string); got != c.wantAction {
			t.Errorf("%v action = %q, want %q", c.args, got, c.wantAction)
		}
		got, _ := f.lastCreateBody["components"].([]any)
		if len(got) != len(c.wantKeys) {
			t.Fatalf("%v components = %v, want %v", c.args, got, c.wantKeys)
		}
		for i := range c.wantKeys {
			if got[i] != c.wantKeys[i] {
				t.Errorf("%v components[%d] = %v, want %v", c.args, i, got[i], c.wantKeys[i])
			}
		}
		if mode, _ := f.lastCreateBody["mode"].(string); mode != c.wantMode {
			t.Errorf("%v mode = %q, want %q", c.args, mode, c.wantMode)
		}
		if dr, _ := f.lastCreateBody["dryRun"].(bool); dr != c.wantDryRun {
			t.Errorf("%v dryRun = %v, want %v", c.args, dr, c.wantDryRun)
		}
	}

	// 不带任何组件、也不是 --missing → 用法错误（避免误触发「全部」）
	if code, _, _ := exec(t, "middleware", "install"); code != exitcode.Usage {
		t.Fatalf("未指定组件应返回用法错误，得到 %d", code)
	}
	// stop 不接受空列表（服务器端也会拒绝）
	if code, _, _ := exec(t, "middleware", "stop"); code != exitcode.Usage {
		t.Fatalf("stop 未指定组件应返回用法错误，得到 %d", code)
	}
}

func TestMiddlewareSetParams(t *testing.T) {
	isolateConfig(t)
	f := &fakeAPI{}
	srv := startFakeWith(t, f, middlewareFake(t, f))
	if code, _, _ := exec(t, "server", "add", "test", "-a", srv.URL, "-t", f.token); code != exitcode.OK {
		t.Fatal("server add failed")
	}

	code, out, _ := exec(t, "middleware", "set", "redis",
		"-p", "REDIS_PORT=6380", "-p", "REDIS_PASSWORD=secret123", "--mode", "native", "--json")
	if code != exitcode.OK {
		t.Fatalf("middleware set exit=%d out=%q", code, out)
	}
	fields, _ := f.lastCreateBody["fields"].(map[string]any)
	if fields["REDIS_PORT"] != "6380" || fields["REDIS_PASSWORD"] != "secret123" {
		t.Fatalf("参数没传对: %+v", f.lastCreateBody)
	}
	if f.lastCreateBody["mode"] != "native" {
		t.Fatalf("mode 没传对: %+v", f.lastCreateBody)
	}

	// 人类可读输出绝不能回显密码明文
	code, out, _ = exec(t, "middleware", "set", "redis", "-p", "REDIS_PASSWORD=secret123")
	if code != exitcode.OK {
		t.Fatalf("middleware set exit=%d", code)
	}
	if strings.Contains(out, "secret123") {
		t.Fatalf("输出泄露了密码明文: %q", out)
	}

	// 传空串 = 清空（要有明确的表达方式，否则密码永远换不掉）
	f.lastCreateBody = nil
	if code, _, _ := exec(t, "middleware", "set", "redis", "-p", "REDIS_PASSWORD="); code != exitcode.OK {
		t.Fatal("清空密码应成功")
	}
	fields, _ = f.lastCreateBody["fields"].(map[string]any)
	if v, ok := fields["REDIS_PASSWORD"]; !ok || v != "" {
		t.Fatalf("空串应原样传给服务器端表示清空: %+v", f.lastCreateBody)
	}

	// 什么都不传 → 用法错误
	if code, _, _ := exec(t, "middleware", "set", "redis"); code != exitcode.Usage {
		t.Fatalf("没给参数应返回用法错误，得到 %d", code)
	}
	// 格式错误的 -p
	if code, _, _ := exec(t, "middleware", "set", "redis", "-p", "NOVALUE"); code != exitcode.Usage {
		t.Fatalf("非法 -p 应返回用法错误，得到 %d", code)
	}
}

// 未登录时中间件命令要返回退出码 3（与其他命令一致）。
func TestMiddlewareRequiresAuth(t *testing.T) {
	isolateConfig(t)
	for _, args := range [][]string{
		{"middleware", "list"}, {"middleware", "get", "redis"},
		{"middleware", "install", "redis"}, {"middleware", "stop", "redis"},
		{"middleware", "fields"}, {"middleware", "script"},
	} {
		if code, _, _ := exec(t, args...); code != exitcode.Auth {
			t.Fatalf("%v exit=%d want %d", args, code, exitcode.Auth)
		}
	}
}

// ---------- 辅助 ----------

// startFakeWith 用自定义 handler 起一个假服务器端（中间件用例需要扩展路由）。
func startFakeWith(t *testing.T, f *fakeAPI, h http.Handler) *httptest.Server {
	t.Helper()
	f.t = t
	if f.token == "" {
		f.token = "jwt-test-token"
	}
	if f.runStatus == "" {
		f.runStatus = "success"
	}
	if f.triggerStatus == "" {
		f.triggerStatus = "queued"
	}
	if f.wsExitStatus == "" {
		f.wsExitStatus = f.runStatus
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// 配置文件里 contexts 的键就是服务器键：直接断言 YAML 结构，避免只测命令输出。
func TestConfigContextShape(t *testing.T) {
	path := isolateConfig(t)
	if code, _, _ := exec(t, "server", "add", "97", "-a", "http://192.168.31.97:10055",
		"-t", "dk_97token000000", "-u", "admin", "--comment", "测试机"); code != exitcode.OK {
		t.Fatal("server add failed")
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, ok := cfg.GetServer("97")
	if !ok {
		t.Fatalf("文件名 %s 里应能按名字取到服务器", filepath.Base(path))
	}
	if ctx.Address != "http://192.168.31.97:10055" || ctx.Token != "dk_97token000000" || ctx.User != "admin" {
		t.Fatalf("服务器内容不对: %+v", ctx)
	}
	if cfg.Current != "97" {
		t.Fatalf("add 应把新服务器设为当前，实际 %q", cfg.Current)
	}
	// ValidateServerName 的边界
	for _, bad := range []string{"", "  ", "a b", "a:b", "a/b", strings.Repeat("x", 65)} {
		if err := config.ValidateServerName(bad); err == nil {
			t.Errorf("服务器键 %q 应被拒绝", bad)
		}
	}
	for _, good := range []string{"test", "97", "测试机", "prod-2", "a.b"} {
		if err := config.ValidateServerName(good); err != nil {
			t.Errorf("服务器键 %q 应被接受: %v", good, err)
		}
	}
}

// --- 键就是名字：login 的写入目标只认 --name（缺省 default），改名 = 改键 ---

// login 的规则：
//  1. --name 指定的服务器已存在 → 写 token、切 current 到它，名字（键）不变；
//  2. --name 指定的服务器不存在 → 登录成功后自动新建（不再要求先 server add）；
//  3. 不传 --name → 写进 default（有则更新，没有则新建）。
func TestLoginWritesIntoNameOrDefault(t *testing.T) {
	path := isolateConfig(t)
	f := &fakeAPI{}
	srv := startFake(t, f)

	if code, _, _ := exec(t, "server", "add", "default", "-a", srv.URL, "-t", f.token); code != exitcode.OK {
		t.Fatal("server add default failed")
	}
	if code, _, _ := exec(t, "server", "add", "97", "-a", srv.URL, "-t", f.token, "--no-set-current"); code != exitcode.OK {
		t.Fatal("server add 97 failed")
	}

	// 1) 名字存在：写 token、切到 97，键不变
	if code, _, errw := exec(t, "login", "-a", srv.URL, "-u", "admin", "-p", "pw", "--name", "97"); code != exitcode.OK {
		t.Fatalf("login --name 97 应成功，exit=%d err=%q", code, errw)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Current != "97" {
		t.Fatalf("current = %q，应为 97", cfg.Current)
	}
	if _, ok := cfg.Contexts["default"]; !ok {
		t.Fatal("default 键不该消失")
	}
	if _, ok := cfg.Contexts["97"]; !ok {
		t.Fatal("97 键不该消失")
	}

	// 2) 名字不存在：登录成功后自动建出该服务器，并带上地址与 token
	code, _, errw := exec(t, "login", "-a", srv.URL, "-u", "admin", "-p", "pw", "--name", "ghost")
	if code != exitcode.OK {
		t.Fatalf("login --name ghost 应自动建键，exit=%d err=%q", code, errw)
	}
	cfg2, _ := config.Load(path)
	ghost, ok := cfg2.Contexts["ghost"]
	if !ok {
		t.Fatal("不存在的 --name 应在登录成功后新建该服务器")
	}
	if ghost.Address != srv.URL || ghost.Token != f.token {
		t.Fatalf("新建的服务器应带上地址与 token: %+v", ghost)
	}
	if cfg2.Current != "ghost" {
		t.Fatalf("current = %q，应为 ghost", cfg2.Current)
	}

	// 3) 不传 --name：写进 default（已存在则更新）
	if code, _, errw := exec(t, "login", "-a", srv.URL, "-u", "admin", "-p", "pw"); code != exitcode.OK {
		t.Fatalf("login（不带 --name）应成功，exit=%d err=%q", code, errw)
	}
	cfg3, _ := config.Load(path)
	if cfg3.Current != "default" {
		t.Fatalf("不带 --name 时应写进 default，current = %q", cfg3.Current)
	}
	if _, ok := cfg3.Contexts["ghost"]; !ok {
		t.Fatal("写 default 不应影响其它服务器")
	}

	// 4) 键就是名字：server update --name 改名 = 改键，current 跟着走
	if code, _, _ := exec(t, "server", "update", "default", "--name", "备用机"); code != exitcode.OK {
		t.Fatal("server update --name failed")
	}
	code, out, _ := exec(t, "server", "list", "--json")
	if code != exitcode.OK {
		t.Fatalf("server list exit=%d", code)
	}
	var parsed struct {
		Current string `json:"current"`
		Servers []struct {
			Key  string `json:"key"`
			Name string `json:"name"`
		} `json:"servers"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("server list --json 解析失败: %v\n%s", err, out)
	}
	found := false
	for _, s := range parsed.Servers {
		if s.Key == "default" {
			t.Fatalf("旧名 default 应已不存在: %s", out)
		}
		if s.Key == "备用机" {
			found = true
			if s.Name != "备用机" {
				t.Fatalf("NAME = %q，应为 备用机", s.Name)
			}
		}
	}
	if !found {
		t.Fatalf("list 里应出现改名后的 备用机: %s", out)
	}
	if parsed.Current != "备用机" {
		t.Fatalf("current = %q，应为 备用机（改名后 current 跟随）", parsed.Current)
	}
}

// -s 简写按设计已移除：必须明确报错，而不是被当成别的 flag。
func TestLoginShorthandSRemoved(t *testing.T) {
	isolateConfig(t)
	code, _, errw := exec(t, "login", "-s", "http://127.0.0.1:1", "-u", "admin", "-p", "x")
	if code != exitcode.Usage {
		t.Fatalf("exit=%d，应为用法错误 2", code)
	}
	if !strings.Contains(errw, "s") {
		t.Fatalf("错误信息应提到 -s，实际 %q", errw)
	}
}
