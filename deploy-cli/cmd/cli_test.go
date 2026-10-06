package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"

	"deploy-cli/internal/exitcode"
)

// exec 在进程内执行一次命令，返回退出码与 stdout/stderr。
func exec(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	nonInteractive(t)
	var out, errw bytes.Buffer
	root := NewRootCmdWithIO(&out, &errw)
	root.SetArgs(args)
	err := root.Execute()
	code := exitcode.Of(err)
	if err != nil && code == exitcode.OK {
		t.Fatalf("命令 %v 返回了错误却映射到退出码 0: %v", args, err)
	}
	if err != nil {
		t.Logf("命令 %v → exit=%d err=%v", args, code, err)
		// 复刻 main() 的行为：非静默错误写到 stderr
		var silent *exitcode.Error
		if !(errors.As(err, &silent) && silent.Silent()) {
			fmt.Fprintf(&errw, "错误: %v\n", err)
		}
	}
	return code, out.String(), errw.String()
}

// nonInteractive 强制把 stdin 视为非终端，保证测试不会阻塞在交互输入上。
func nonInteractive(t *testing.T) {
	t.Helper()
	old := interactiveFn
	interactiveFn = func() bool { return false }
	t.Cleanup(func() { interactiveFn = old })
}

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

// isolateConfig 把配置文件指到临时目录，并清空可能干扰的环境变量。
func isolateConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".deploy", "config.yaml")
	t.Setenv("DEPLOY_CONFIG", path)
	t.Setenv("DEPLOY_SERVER", "")
	t.Setenv("DEPLOY_TOKEN", "")
	t.Setenv("DEPLOY_USER", "")
	t.Setenv("NO_COLOR", "1")
	return path
}

// fakeAPI 实现契约里 CLI 用到的最小接口集。
type fakeAPI struct {
	mu             sync.Mutex
	t              *testing.T
	token          string
	runStatus      string
	runStatuses    []string // 依次返回；耗尽后沿用最后一个
	runCalls       int
	runExitCode    int
	triggerStatus  string
	unauthorized   bool
	logs           string
	wsPayload      string
	wsExitStatus   string
	lastRunBody    map[string]any
	lastCreateBody map[string]any
	lastAuthHeader string
	cancelCalls    int
}

func (f *fakeAPI) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Username, Password string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Password != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"success":false,"errorCode":40100,"errorMessage":"用户名或密码错误","showType":0}`))
			return
		}
		writeOK(w, map[string]any{
			"token": f.token, "expiresAt": 0, "mustChangePassword": false,
			"user": map[string]any{"id": 1, "username": req.Username, "role": "admin", "status": "active"},
		})
	})
	mux.HandleFunc("GET /api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		if !f.authorized(w, r) {
			return
		}
		writeOK(w, map[string]any{"id": 1, "username": "admin", "nickname": "管理员", "role": "admin", "status": "active"})
	})
	mux.HandleFunc("POST /api/v1/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		writeOK(w, nil)
	})
	mux.HandleFunc("GET /api/v1/projects", func(w http.ResponseWriter, r *http.Request) {
		if !f.authorized(w, r) {
			return
		}
		writeOK(w, map[string]any{
			"list": []map[string]any{{
				"id": 1, "key": "my-app", "name": "我的应用",
				"workdir": "/srv/workspaces/my-app", "lastRunAt": "2026-01-01T10:00:00+08:00",
				"lastRunStatus": "success", "archived": false,
			}},
			"total": 1, "page": 1, "pageSize": 50,
		})
	})
	mux.HandleFunc("POST /api/v1/projects", func(w http.ResponseWriter, r *http.Request) {
		if !f.authorized(w, r) {
			return
		}
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.lastCreateBody = in
		in["id"] = 2
		writeOK(w, in)
	})
	mux.HandleFunc("GET /api/v1/projects/{key}", func(w http.ResponseWriter, r *http.Request) {
		if !f.authorized(w, r) {
			return
		}
		writeOK(w, map[string]any{
			"id": 1, "key": r.PathValue("key"), "name": "我的应用", "workdir": "/srv/w",
			"shell": "auto", "concurrency": "queue", "hookEnabled": true, "canEdit": true,
			"commands": []map[string]any{{"id": 10, "name": "构建", "script": "npm run build", "sortOrder": 1, "enabled": true, "failStrategy": "abort"}},
			"envs":     []map[string]any{{"id": 5, "key": "MODE", "value": "prod", "isSecret": false}},
		})
	})
	mux.HandleFunc("POST /api/v1/projects/{key}/run", func(w http.ResponseWriter, r *http.Request) {
		if !f.authorized(w, r) {
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.lastRunBody = body
		writeOK(w, map[string]any{
			"runId": 88, "projectKey": r.PathValue("key"), "buildNumber": 13,
			"status": f.triggerStatus, "logUrl": "/api/v1/runs/88/log",
		})
	})
	mux.HandleFunc("GET /api/v1/runs", func(w http.ResponseWriter, r *http.Request) {
		if !f.authorized(w, r) {
			return
		}
		writeOK(w, map[string]any{
			"list": []map[string]any{{
				"id": 88, "projectKey": "my-app", "projectName": "我的应用", "buildNumber": 13,
				"status": "success", "triggerType": "cli", "triggerUserName": "admin",
				"startedAt": "2026-01-01T10:00:00+08:00", "durationMs": 8123, "exitCode": 0,
			}},
			"total": 1, "page": 1, "pageSize": 20,
		})
	})
	mux.HandleFunc("GET /api/v1/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !f.authorized(w, r) {
			return
		}
		f.mu.Lock()
		status := f.runStatus
		if len(f.runStatuses) > 0 {
			idx := f.runCalls
			if idx >= len(f.runStatuses) {
				idx = len(f.runStatuses) - 1
			}
			status = f.runStatuses[idx]
		}
		f.runCalls++
		f.mu.Unlock()
		writeOK(w, map[string]any{
			"id": 88, "projectKey": "my-app", "buildNumber": 13, "status": status,
			"exitCode": f.runExitCode, "durationMs": 8123, "startedAt": "2026-01-01T10:00:00+08:00",
		})
	})
	mux.HandleFunc("GET /api/v1/runs/{id}/log", func(w http.ResponseWriter, r *http.Request) {
		if !f.authorized(w, r) {
			return
		}
		writeOK(w, map[string]any{"offset": 0, "nextOffset": len(f.logs), "eof": true, "data": f.logs})
	})
	// 契约 7.4：token 走 query，服务端先补历史再推增量。
	mux.HandleFunc("GET /api/v1/ws/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("token"); got != f.token {
			f.t.Errorf("WebSocket token = %q, want %q", got, f.token)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			f.t.Logf("upgrade failed: %v", err)
			return
		}
		defer conn.Close()
		payload := f.wsPayload
		if payload == "" {
			payload = "hello from ws\n"
		}
		_ = conn.WriteJSON(map[string]any{"type": "log", "runId": 88, "nextOffset": len(payload), "data": payload})
		_ = conn.WriteJSON(map[string]any{
			"type": "exit", "runId": 88, "status": f.wsExitStatus,
			"exitCode": f.runExitCode, "durationMs": 1500, "nextOffset": len(payload),
		})
	})
	mux.HandleFunc("POST /api/v1/runs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		if !f.authorized(w, r) {
			return
		}
		f.cancelCalls++
		writeOK(w, nil)
	})
	mux.HandleFunc("GET /api/v1/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeOK(w, map[string]any{"status": "ok", "version": "0.5.0", "time": "2026-01-01T00:00:00+08:00"})
	})
	mux.HandleFunc("GET /api/v1/system/doctor", func(w http.ResponseWriter, r *http.Request) {
		if !f.authorized(w, r) {
			return
		}
		writeOK(w, map[string]any{
			"os": "linux", "arch": "amd64", "hostname": "srv", "goVersion": "go1.24.2",
			"serverVersion": "0.1.0", "uptimeSec": 12, "webRoot": "/srv/w", "webRootWritable": true,
			"dataDir": "/srv/d", "dataDirWritable": true,
			"shell": map[string]any{"selected": "bash", "path": "/usr/bin/bash", "available": []string{"bash", "sh"}},
			"checks": []map[string]any{
				{"name": "git", "found": true, "version": "git version 2.39.5", "path": "/usr/bin/git"},
				{"name": "docker", "found": false, "hint": "apt install -y docker.io"},
			},
		})
	})
	return mux
}

func (f *fakeAPI) authorized(w http.ResponseWriter, r *http.Request) bool {
	f.mu.Lock()
	f.lastAuthHeader = r.Header.Get("Authorization")
	unauth := f.unauthorized
	f.mu.Unlock()
	if unauth {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"errorCode":40100,"errorMessage":"未登录或登录已过期","showType":0}`))
		return false
	}
	if f.lastAuthHeader == "" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"errorCode":40100,"errorMessage":"缺少 Authorization","showType":0}`))
		return false
	}
	return true
}

func writeOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	body := map[string]any{"success": true}
	if data != nil {
		body["data"] = data
	}
	_ = json.NewEncoder(w).Encode(body)
}

func startFake(t *testing.T, f *fakeAPI) *httptest.Server {
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
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	return srv
}

// ---------- 纯本地行为 ----------

func TestVersionCommand(t *testing.T) {
	isolateConfig(t)
	code, out, _ := exec(t, "version")
	if code != exitcode.OK {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(out, "deploy ") {
		t.Fatalf("stdout = %q", out)
	}

	code, out, _ = exec(t, "version", "--json")
	if code != exitcode.OK {
		t.Fatalf("exit=%d", code)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("stdout 不是 JSON: %v (%q)", err, out)
	}
	if payload["version"] == "" {
		t.Fatalf("version 字段为空: %q", out)
	}
}

func TestVersionFlag(t *testing.T) {
	isolateConfig(t)
	code, out, _ := exec(t, "--version")
	if code != exitcode.OK || !strings.Contains(out, "deploy version") {
		t.Fatalf("exit=%d out=%q", code, out)
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	isolateConfig(t)
	code, _, errw := exec(t, "nosuchcmd")
	if code != exitcode.Usage {
		t.Fatalf("exit=%d want %d", code, exitcode.Usage)
	}
	if !strings.Contains(errw, "unknown command") {
		t.Fatalf("stderr = %q", errw)
	}
}

func TestUnknownSubcommandIsUsageError(t *testing.T) {
	isolateConfig(t)
	for _, args := range [][]string{{"project", "nope"}, {"runs", "nope"}, {"config", "nope"}} {
		if code, _, _ := exec(t, args...); code != exitcode.Usage {
			t.Fatalf("%v exit=%d want %d", args, code, exitcode.Usage)
		}
	}
}

func TestUnknownFlagIsUsageError(t *testing.T) {
	isolateConfig(t)
	code, _, errw := exec(t, "run", "--nope")
	if code != exitcode.Usage {
		t.Fatalf("exit=%d want %d (%s)", code, exitcode.Usage, errw)
	}
}

func TestMissingArgsIsUsageError(t *testing.T) {
	isolateConfig(t)
	for _, args := range [][]string{
		{"run"}, {"logs"}, {"stop"}, {"project", "get"},
		{"logs", "abc"}, {"stop", "0"}, {"run", "k", "-e", "NOVALUE"},
	} {
		if code, _, _ := exec(t, args...); code != exitcode.Usage {
			t.Fatalf("%v exit=%d want %d", args, code, exitcode.Usage)
		}
	}
}

func TestHelpDoesNotFail(t *testing.T) {
	isolateConfig(t)
	for _, args := range [][]string{{"--help"}, {"run", "--help"}, {"project", "create", "--help"}} {
		code, out, _ := exec(t, args...)
		if code != exitcode.OK {
			t.Fatalf("%v exit=%d", args, code)
		}
		if !strings.Contains(out, "Usage:") {
			t.Fatalf("%v 未输出用法: %q", args, out)
		}
	}
}

func TestAuthRequiredReturnsExitCode3(t *testing.T) {
	isolateConfig(t)
	for _, args := range [][]string{
		{"whoami"}, {"project", "list"}, {"project", "get", "k"}, {"runs", "list"},
		{"doctor"}, {"stop", "1"},
	} {
		if code, _, _ := exec(t, args...); code != exitcode.Auth {
			t.Fatalf("%v exit=%d want %d", args, code, exitcode.Auth)
		}
	}
}

// ---------- 配置读写 ----------

func TestConfigSetGetRoundTrip(t *testing.T) {
	path := isolateConfig(t)

	code, _, _ := exec(t, "config", "set", "address", "http://127.0.0.1:9999")
	if code != exitcode.OK {
		t.Fatalf("config set exit=%d", code)
	}
	code, out, _ := exec(t, "config", "get", "address")
	if code != exitcode.OK || strings.TrimSpace(out) != "http://127.0.0.1:9999" {
		t.Fatalf("config get = %q (exit=%d)", out, code)
	}

	if code, _, _ = exec(t, "config", "set", "token", "dk_secret_token_value"); code != exitcode.OK {
		t.Fatalf("set token exit=%d", code)
	}
	// 全量视图必须打码
	code, out, _ = exec(t, "config", "get")
	if code != exitcode.OK || strings.Contains(out, "dk_secret_token_value") {
		t.Fatalf("全量配置泄露 token: %q", out)
	}
	// 显式取 token 才输出明文
	code, out, _ = exec(t, "config", "get", "token")
	if code != exitcode.OK || strings.TrimSpace(out) != "dk_secret_token_value" {
		t.Fatalf("config get token = %q", out)
	}

	// 配置文件确实落在临时目录
	code, out, _ = exec(t, "config", "path")
	if code != exitcode.OK || strings.TrimSpace(out) != path {
		t.Fatalf("config path = %q, want %q", strings.TrimSpace(out), path)
	}

	// 切换到新 context
	if code, _, _ = exec(t, "config", "set", "current", "prod"); code != exitcode.OK {
		t.Fatalf("set current exit=%d", code)
	}
	code, out, _ = exec(t, "config", "get", "current")
	if code != exitcode.OK || strings.TrimSpace(out) != "prod" {
		t.Fatalf("current = %q", out)
	}
	// 新 context 的 token 应为空
	code, out, _ = exec(t, "config", "get", "token")
	if code != exitcode.OK || strings.TrimSpace(out) != "" {
		t.Fatalf("新 context token = %q", out)
	}
	// 回到 default，token 仍在
	if code, _, _ = exec(t, "config", "set", "current", "default"); code != exitcode.OK {
		t.Fatalf("set current default exit=%d", code)
	}
	code, out, _ = exec(t, "config", "get", "token")
	if strings.TrimSpace(out) != "dk_secret_token_value" {
		t.Fatalf("default token = %q", out)
	}

	if code, _, _ = exec(t, "config", "set", "nope", "x"); code != exitcode.Usage {
		t.Fatalf("未知键 exit=%d want %d", code, exitcode.Usage)
	}
}

func TestEnvVarsBeatConfigFile(t *testing.T) {
	isolateConfig(t)
	f := &fakeAPI{}
	srv := startFake(t, f)

	if code, _, _ := exec(t, "config", "set", "address", "http://127.0.0.1:1"); code != exitcode.OK {
		t.Fatal("config set failed")
	}
	// 环境变量应当覆盖配置文件
	t.Setenv("DEPLOY_ADDRESS", srv.URL)
	t.Setenv("DEPLOY_TOKEN", f.token)
	code, out, _ := exec(t, "whoami")
	if code != exitcode.OK {
		t.Fatalf("whoami exit=%d (%s)", code, out)
	}
	if f.lastAuthHeader != f.token {
		t.Fatalf("Authorization = %q", f.lastAuthHeader)
	}
}

// ---------- 端到端（进程内 + fake API） ----------

func TestLoginProjectRunStopFlow(t *testing.T) {
	isolateConfig(t)
	f := &fakeAPI{}
	srv := startFake(t, f)

	// login
	code, out, errw := exec(t, "login", "-a", srv.URL, "-u", "admin", "-p", "pw")
	if code != exitcode.OK {
		t.Fatalf("login exit=%d out=%q err=%q", code, out, errw)
	}

	// whoami（用保存下来的 token）
	code, out, _ = exec(t, "whoami", "--json")
	if code != exitcode.OK {
		t.Fatalf("whoami exit=%d", code)
	}
	var who struct {
		Address string `json:"address"`
		User    struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal([]byte(out), &who); err != nil {
		t.Fatalf("whoami JSON: %v (%q)", err, out)
	}
	if who.User.Username != "admin" || who.Address != srv.URL {
		t.Fatalf("whoami = %+v", who)
	}

	// project list
	code, out, _ = exec(t, "project", "list")
	if code != exitcode.OK || !strings.Contains(out, "my-app") || !strings.Contains(out, "我的应用") {
		t.Fatalf("project list exit=%d out=%q", code, out)
	}

	// project get
	code, out, _ = exec(t, "project", "get", "my-app")
	if code != exitcode.OK || !strings.Contains(out, "npm run build") {
		t.Fatalf("project get exit=%d out=%q", code, out)
	}

	// project create（非交互）
	code, out, _ = exec(t, "project", "create",
		"--key", "svc", "--name", "服务", "--workdir", "/srv/svc",
		"--command", "git pull", "--command", "构建::npm ci", "--env", "MODE=prod")
	if code != exitcode.OK {
		t.Fatalf("project create exit=%d out=%q", code, out)
	}

	// project create 缺参数 → 用法错误
	if code, _, _ = exec(t, "project", "create", "--key", "bad key", "--workdir", "/x", "--command", "ls"); code != exitcode.Usage {
		t.Fatalf("非法 key 应返回 2，得到 %d", code)
	}

	// project create --file：Windows 上没有 $(cat ...)，用 JSON + scriptFile 表达
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sh", "01-recon.sh"), []byte("echo recon\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sh", "02-build.sh"), []byte("mvn -q package\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec := map[string]any{
		"key": "from-file", "name": "来自文件", "workdir": "/srv/from-file",
		"useGit": true, "gitUrl": "https://example.com/x.git", "gitBranch": "main",
		"env": map[string]string{"APP_PORT": "10062"},
		"commands": []map[string]any{
			{"name": "环境勘察", "scriptFile": "sh/01-recon.sh"},
			{"name": "Maven 构建", "scriptFile": filepath.Join("sh", "02-build.sh")},
		},
	}
	b, _ := json.Marshal(spec)
	specPath := filepath.Join(dir, "project.json")
	if err := os.WriteFile(specPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errw = exec(t, "project", "create", "--file", specPath)
	if code != exitcode.OK {
		t.Fatalf("project create --file exit=%d out=%q err=%q", code, out, errw)
	}
	if f.lastCreateBody["key"] != "from-file" || f.lastCreateBody["workdir"] != "/srv/from-file" {
		t.Fatalf("文件里的项目字段没进请求体: %+v", f.lastCreateBody)
	}
	if f.lastCreateBody["useGit"] != true || f.lastCreateBody["gitUrl"] != "https://example.com/x.git" {
		t.Fatalf("文件里的 git 字段没进请求体: %+v", f.lastCreateBody)
	}
	cmds, _ := f.lastCreateBody["commands"].([]any)
	if len(cmds) != 2 {
		t.Fatalf("commands = %+v", f.lastCreateBody["commands"])
	}
	first, _ := cmds[0].(map[string]any)
	if first["name"] != "环境勘察" || first["script"] != "echo recon\n" {
		t.Fatalf("第一条命令 = %+v", first)
	}
	second, _ := cmds[1].(map[string]any)
	if second["script"] != "mvn -q package\n" {
		t.Fatalf("scriptFile 未按 Windows 反斜杠读取: %+v", second)
	}
	envs, _ := f.lastCreateBody["envs"].([]any)
	if len(envs) != 1 {
		t.Fatalf("envs = %+v", f.lastCreateBody["envs"])
	}
	if e0, _ := envs[0].(map[string]any); e0["key"] != "APP_PORT" || e0["value"] != "10062" {
		t.Fatalf("env 简写没解析: %+v", envs[0])
	}

	// 显式 flag 覆盖文件；且不因为没传 --command 就丢掉文件里的命令
	code, out, errw = exec(t, "project", "create", "--file", specPath, "--key", "override-key")
	if code != exitcode.OK {
		t.Fatalf("--file + --key 覆盖 exit=%d err=%q", code, errw)
	}
	if f.lastCreateBody["key"] != "override-key" {
		t.Fatalf("显式 --key 应覆盖文件: %+v", f.lastCreateBody)
	}
	if cs, _ := f.lastCreateBody["commands"].([]any); len(cs) != 2 {
		t.Fatalf("覆盖 key 时命令应仍来自文件: %+v", f.lastCreateBody["commands"])
	}

	// 文件不存在 / scriptFile 缺失 → 用法错误（退出码 2），不是 panic
	if code, _, _ = exec(t, "project", "create", "--file", filepath.Join(dir, "nope.json")); code != exitcode.Usage {
		t.Fatalf("不存在的 --file 应返回 2，得到 %d", code)
	}
	badSpec := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(badSpec, []byte(`{"key":"bad","workdir":"/x","commands":[{"name":"a","scriptFile":"sh/missing.sh"}]}`), 0o600)
	if code, _, _ = exec(t, "project", "create", "--file", badSpec); code != exitcode.Usage {
		t.Fatalf("缺失 scriptFile 应返回 2，得到 %d", code)
	}

	// run（不跟随）
	code, out, _ = exec(t, "run", "my-app", "-e", "TAG=v1", "--branch", "main")
	if code != exitcode.OK || !strings.Contains(out, "#88") {
		t.Fatalf("run exit=%d out=%q", code, out)
	}
	if f.lastRunBody["branch"] != "main" {
		t.Fatalf("触发请求体 = %+v", f.lastRunBody)
	}
	if env, ok := f.lastRunBody["env"].(map[string]any); !ok || env["TAG"] != "v1" {
		t.Fatalf("触发请求 env = %+v", f.lastRunBody["env"])
	}

	// run --wait（成功的运行 → 0）
	code, out, _ = exec(t, "run", "my-app", "--wait")
	if code != exitcode.OK {
		t.Fatalf("run --wait exit=%d out=%q", code, out)
	}

	// logs（一次性拉取）
	f.logs = "hello\nworld\n"
	code, out, _ = exec(t, "logs", "88")
	if code != exitcode.OK || out != f.logs {
		t.Fatalf("logs exit=%d out=%q", code, out)
	}

	// logs --json → JSONL
	code, out, _ = exec(t, "logs", "88", "--json")
	if code != exitcode.OK {
		t.Fatalf("logs --json exit=%d", code)
	}
	line := strings.Split(strings.TrimSpace(out), "\n")[0]
	var ev struct {
		Type string `json:"type"`
		Data string `json:"data"`
	}
	if err := json.Unmarshal([]byte(line), &ev); err != nil || ev.Type != "log" {
		t.Fatalf("logs --json 不是 JSONL: %q (%v)", line, err)
	}

	// runs list
	code, out, _ = exec(t, "runs", "list", "--project", "my-app", "--status", "success")
	if code != exitcode.OK || !strings.Contains(out, "88") {
		t.Fatalf("runs list exit=%d out=%q", code, out)
	}
	if code, _, _ = exec(t, "runs", "list", "--status", "bogus"); code != exitcode.Usage {
		t.Fatalf("非法 --status 应返回 2，得到 %d", code)
	}

	// stop
	code, out, _ = exec(t, "stop", "88")
	if code != exitcode.OK || f.cancelCalls != 1 {
		t.Fatalf("stop exit=%d calls=%d out=%q", code, f.cancelCalls, out)
	}

	// doctor
	code, out, _ = exec(t, "doctor")
	if code != exitcode.OK {
		t.Fatalf("doctor exit=%d", code)
	}
	if !strings.Contains(out, "git") || !strings.Contains(out, "docker") || !strings.Contains(out, "apt install -y docker.io") {
		t.Fatalf("doctor 输出缺少自检项: %q", out)
	}

	// doctor --json 必须能解析且 stdout 干净
	code, out, _ = exec(t, "doctor", "--json")
	if code != exitcode.OK {
		t.Fatalf("doctor --json exit=%d", code)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("doctor --json 不是合法 JSON: %v (%q)", err, out)
	}
	if doc["serverVersion"] != "0.1.0" {
		t.Fatalf("doctor JSON = %+v", doc)
	}

	// logout
	code, _, _ = exec(t, "logout")
	if code != exitcode.OK {
		t.Fatalf("logout exit=%d", code)
	}
	if code, _, _ = exec(t, "whoami"); code != exitcode.Auth {
		t.Fatalf("登出后 whoami 应返回 3，得到 %d", code)
	}
}

// run -f：真实 WebSocket 路径（握手 + 帧解析 + offset 续拉 + ANSI 剥离）。
func TestRunFollowOverWebSocket(t *testing.T) {
	isolateConfig(t)
	f := &fakeAPI{
		runStatuses:  []string{"running", "running", "failed"},
		runExitCode:  2,
		wsPayload:    "\x1b[31mfailed output\x1b[0m\n",
		wsExitStatus: "failed",
	}
	srv := startFake(t, f)

	if code, _, _ := exec(t, "login", "-a", srv.URL, "-u", "admin", "-p", "pw"); code != exitcode.OK {
		t.Fatal("login failed")
	}

	code, out, errw := exec(t, "run", "my-app", "-f")
	if code != exitcode.DeployFailed {
		t.Fatalf("exit=%d want %d (out=%q err=%q)", code, exitcode.DeployFailed, out, errw)
	}
	if !strings.Contains(out, "failed output") {
		t.Fatalf("未收到 WebSocket 日志: %q", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("非 TTY 下 ANSI 未被剥离: %q", out)
	}
	if !strings.Contains(out, "run ID #88 (build #13)") {
		t.Fatalf("摘要应同时标注全局 run ID 与项目内构建号: %q", out)
	}
	if strings.Contains(out, "运行 #88  ") {
		t.Fatalf("摘要不应再使用裸 run ID 格式: %q", out)
	}

	// --json -f：stdout 必须是干净 JSONL + 最后一行结果 JSON
	f.runCalls = 0
	f.runStatuses = []string{"running", "success"}
	f.runExitCode = 0
	f.wsExitStatus = "success"
	code, out, _ = exec(t, "run", "my-app", "-f", "--json")
	if code != exitcode.OK {
		t.Fatalf("exit=%d (out=%q)", code, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		t.Fatalf("JSON 输出行数不足: %q", out)
	}
	var last map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatalf("最后一行不是 JSON: %v (%q)", err, lines[len(lines)-1])
	}
	if last["status"] != "success" || last["runId"] != float64(88) {
		t.Fatalf("结果 JSON = %+v", last)
	}
	if last["buildNumber"] != float64(13) {
		t.Fatalf("结果 JSON 应带 buildNumber: %+v", last)
	}
	for i, line := range lines[:len(lines)-1] {
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("第 %d 行不是 JSONL: %v (%q)", i, err, line)
		}
		if ev["type"] != "log" {
			t.Fatalf("第 %d 行类型 = %v", i, ev["type"])
		}
	}
}

// 摘要里的运行标识必须同时区分全局 run ID 与项目内构建号。
func TestRunHeadFormatting(t *testing.T) {
	if got := runHead(3, 2); got != "run ID #3 (build #2)" {
		t.Fatalf("runHead(3,2) = %q", got)
	}
	if got := runHead(3, 0); got != "run ID #3" {
		t.Fatalf("构建号未知时应只显示 run ID，得到 %q", got)
	}
}

// 运行失败 → 退出码 1。
func TestRunWaitFailedReturnsExitCode1(t *testing.T) {
	isolateConfig(t)
	f := &fakeAPI{runStatus: "failed", runExitCode: 2}
	srv := startFake(t, f)

	if code, _, _ := exec(t, "login", "-a", srv.URL, "-u", "admin", "-p", "pw"); code != exitcode.OK {
		t.Fatal("login failed")
	}
	code, out, errw := exec(t, "run", "my-app", "--wait")
	if code != exitcode.DeployFailed {
		t.Fatalf("run --wait exit=%d want %d (out=%q err=%q)", code, exitcode.DeployFailed, out, errw)
	}
	if !strings.Contains(out, "failed") {
		t.Fatalf("摘要缺少状态: %q", out)
	}
}

// 401 → 退出码 3。
func TestUnauthorizedReturnsExitCode3(t *testing.T) {
	isolateConfig(t)
	f := &fakeAPI{unauthorized: true}
	srv := startFake(t, f)

	if code, _, _ := exec(t, "login", "-a", srv.URL, "-u", "admin", "-p", "pw"); code != exitcode.OK {
		t.Fatal("login failed")
	}
	code, _, errw := exec(t, "project", "list")
	if code != exitcode.Auth {
		t.Fatalf("exit=%d want %d (%s)", code, exitcode.Auth, errw)
	}
}

// token 被服务端拒绝时，--token 直接写入必须失败且不落盘。
func TestLoginWithBadTokenDoesNotSave(t *testing.T) {
	isolateConfig(t)
	f := &fakeAPI{unauthorized: true}
	srv := startFake(t, f)

	code, _, _ := exec(t, "login", "-a", srv.URL, "--token", "bad-token")
	if code != exitcode.Auth {
		t.Fatalf("exit=%d want %d", code, exitcode.Auth)
	}
	code, out, _ := exec(t, "config", "get", "token")
	if code != exitcode.OK || strings.TrimSpace(out) != "" {
		t.Fatalf("无效 token 不应落盘，得到 %q", out)
	}
}

// 登录密码错误 → 退出码 3。
func TestLoginWrongPasswordExitCode3(t *testing.T) {
	isolateConfig(t)
	f := &fakeAPI{}
	srv := startFake(t, f)

	code, _, errw := exec(t, "login", "-a", srv.URL, "-u", "admin", "-p", "wrong")
	if code != exitcode.Auth {
		t.Fatalf("exit=%d want %d", code, exitcode.Auth)
	}
	if !strings.Contains(errw, "sign-in failed") {
		t.Fatalf("stderr = %q", errw)
	}
}

// 服务端不可达 → 退出码 4。
func TestServerDownReturnsExitCode4(t *testing.T) {
	isolateConfig(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	code, _, errw := exec(t, "login", "-a", url, "-u", "admin", "-p", "pw")
	if code != exitcode.Network {
		t.Fatalf("exit=%d want %d (%s)", code, exitcode.Network, errw)
	}
}

// 非交互环境缺少密码必须报用法错误，而不是卡住等待输入。
func TestLoginNonInteractiveRequiresPassword(t *testing.T) {
	isolateConfig(t)
	code, _, _ := exec(t, "login", "-a", "http://127.0.0.1:8080", "-u", "admin")
	if code != exitcode.Usage {
		t.Fatalf("exit=%d want %d", code, exitcode.Usage)
	}
}

// --address/--token 全局 flag 优先级最高（覆盖配置文件里的地址）。
func TestGlobalFlagsBeatEverything(t *testing.T) {
	isolateConfig(t)
	f := &fakeAPI{}
	srv := startFake(t, f)

	if code, _, _ := exec(t, "config", "set", "address", "http://127.0.0.1:1"); code != exitcode.OK {
		t.Fatal("config set failed")
	}
	code, out, _ := exec(t, "--address", srv.URL, "--token", "flag-token", "whoami", "--json")
	if code != exitcode.OK {
		t.Fatalf("exit=%d out=%q", code, out)
	}
	if f.lastAuthHeader != "flag-token" {
		t.Fatalf("Authorization = %q", f.lastAuthHeader)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("whoami --json 不是合法 JSON: %v (%q)", err, out)
	}
	if got["address"] != srv.URL {
		t.Fatalf("address = %v, want %v", got["address"], srv.URL)
	}
}

// 配置损坏 → 用法错误（2），而不是崩溃。
func TestBrokenConfigIsUsageError(t *testing.T) {
	path := isolateConfig(t)
	if err := writeFile(path, "current: [unclosed\n"); err != nil {
		t.Fatal(err)
	}
	code, _, errw := exec(t, "version")
	if code != exitcode.Usage {
		t.Fatalf("exit=%d want %d (%s)", code, exitcode.Usage, errw)
	}
}
