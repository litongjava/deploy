package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"deploy-server/internal/config"
	"deploy-server/internal/runner"
	"deploy-server/internal/secret"
	"deploy-server/internal/store"
)

// 这一组测试把「登录 → 建项目 → 执行 → 看日志 → Hook 触发 → 取消」
// 整条主链路真的跑一遍。它同时充当需求文档 §11 里 MVP 验收清单的自动化版本。

type testEnv struct {
	ts    *httptest.Server
	srv   *Server
	st    *store.Store
	token string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DEPLOY_JWT_SECRET", "test-jwt-secret")
	t.Setenv("DEPLOY_SECRET_KEY", "test-encryption-key")
	t.Setenv("DEPLOY_DATA_DIR", "")

	cfg, err := config.Load([]string{
		"-data-dir", dir + "/data",
		"-web-root", dir + "/workspaces",
		"-listen", "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	box, err := secret.New(cfg.SecretKey)
	if err != nil {
		t.Fatalf("初始化加密失败: %v", err)
	}
	exec := runner.New(cfg, st, box)
	srv := NewServer(cfg, st, box, exec, "test")

	ts := httptest.NewServer(srv.Router())
	t.Cleanup(func() {
		ts.Close()
		st.Close()
	})
	return &testEnv{ts: ts, srv: srv, st: st}
}

// call 发一个请求并把响应体解成 map。
func (e *testEnv) call(t *testing.T, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, e.ts.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("%s %s 响应不是 JSON: %s", method, path, string(raw))
		}
	}
	return resp.StatusCode, out
}

// callWithHeaders 与 call 相同，但可以额外设置任意请求头。
// Hook 的 X-Deploy-Token 需要它；「路径与头部凭据不一致」这类场景也只能用它构造。
func (e *testEnv) callWithHeaders(t *testing.T, method, path string, headers map[string]string, body any) (int, map[string]any) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, e.ts.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	if len(raw) > 0 {
		// 非 JSON 的响应（例如路由没命中时的空白体）不必让用例失败，
		// 调用方只关心状态码，所以解析失败就返回空 map。
		_ = json.Unmarshal(raw, &out)
	}
	return resp.StatusCode, out
}

func (e *testEnv) mustBootstrap(t *testing.T) {
	t.Helper()
	_, out := e.call(t, "GET", "/api/v1/auth/bootstrap-status", "", nil)
	if data, _ := out["data"].(map[string]any); data["initialized"] == true {
		t.Fatal("新库不应显示已初始化")
	}
	code, out := e.call(t, "POST", "/api/v1/auth/bootstrap", "", map[string]string{
		"username": "admin", "password": "admin12345",
	})
	if code != 200 {
		t.Fatalf("bootstrap 失败: %d %v", code, out)
	}
	data := out["data"].(map[string]any)
	e.token = data["token"].(string)
	if e.token == "" {
		t.Fatal("bootstrap 没有返回 token")
	}
}

func dataMap(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	d, ok := out["data"].(map[string]any)
	if !ok {
		t.Fatalf("响应缺少 data 对象: %v", out)
	}
	return d
}

// ---------- 认证 ----------

func TestAuthFlow(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	// 未带 token 访问受保护接口 → HTTP 401（前端据此跳登录页）
	if code, _ := e.call(t, "GET", "/api/v1/auth/me", "", nil); code != 401 {
		t.Fatalf("未登录访问应返回 401，得到 %d", code)
	}
	// 错误 token 同样是 401
	if code, _ := e.call(t, "GET", "/api/v1/auth/me", "bogus-token", nil); code != 401 {
		t.Fatalf("错误 token 应返回 401，得到 %d", code)
	}
	// 正确 token
	code, out := e.call(t, "GET", "/api/v1/auth/me", e.token, nil)
	if code != 200 {
		t.Fatalf("me 失败: %d", code)
	}
	if dataMap(t, out)["username"] != "admin" {
		t.Fatalf("当前用户不对: %v", out)
	}

	// 密码错误用 HTTP 401
	if code, _ := e.call(t, "POST", "/api/v1/auth/login", "", map[string]string{
		"username": "admin", "password": "wrong-password",
	}); code != 401 {
		t.Fatalf("密码错误应返回 401，得到 %d", code)
	}
	// 正确密码
	code, out = e.call(t, "POST", "/api/v1/auth/login", "", map[string]string{
		"username": "admin", "password": "admin12345",
	})
	if code != 200 {
		t.Fatalf("登录失败: %d %v", code, out)
	}

	// 重复 bootstrap 必须被拒绝
	code, out = e.call(t, "POST", "/api/v1/auth/bootstrap", "", map[string]string{
		"username": "admin2", "password": "admin12345",
	})
	if code != 200 || out["success"] != false {
		t.Fatalf("重复 bootstrap 应被拒绝: %d %v", code, out)
	}
}

// ---------- 主链路 ----------

func TestProjectRunLifecycle(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	// 建项目：两条命令，一条失败策略为 continue
	code, out := e.call(t, "POST", "/api/v1/projects", e.token, map[string]any{
		"key": "demo-app", "name": "演示项目",
		"workdir": "${WORKSPACE_ROOT}/${PROJECT_KEY}",
		"commands": []map[string]any{
			{"name": "打招呼", "script": "echo hello-from-deploy"},
			{"name": "第二步", "script": "echo second-step"},
		},
		"envs": []map[string]any{
			{"key": "MODE", "value": "test", "isSecret": false},
		},
	})
	if code != 200 {
		t.Fatalf("创建项目失败: %d %v", code, out)
	}
	proj := dataMap(t, out)
	if proj["key"] != "demo-app" {
		t.Fatalf("项目 key 不对: %v", proj)
	}
	if cmds, _ := proj["commands"].([]any); len(cmds) != 2 {
		t.Fatalf("命令没有保存: %v", proj["commands"])
	}

	// key 重复必须冲突
	code, out = e.call(t, "POST", "/api/v1/projects", e.token, map[string]any{
		"key": "demo-app", "name": "重复", "workdir": "/tmp/x",
	})
	if out["success"] != false {
		t.Fatalf("重复 key 应失败: %d %v", code, out)
	}

	// 触发执行
	code, out = e.call(t, "POST", "/api/v1/projects/demo-app/run", e.token, nil)
	if code != 200 {
		t.Fatalf("触发失败: %d %v", code, out)
	}
	runID := int(dataMap(t, out)["runId"].(float64))

	run := e.waitRun(t, runID, 60*time.Second)
	if run["status"] != store.RunSuccess {
		t.Fatalf("执行状态应为 success，实际 %v（错误：%v）", run["status"], run["errorMsg"])
	}
	if run["exitCode"].(float64) != 0 {
		t.Fatalf("退出码应为 0，实际 %v", run["exitCode"])
	}
	if run["buildNumber"].(float64) != 1 {
		t.Fatalf("首次执行 buildNumber 应为 1，实际 %v", run["buildNumber"])
	}

	// 日志内容
	_, out = e.call(t, "GET", fmt.Sprintf("/api/v1/runs/%d/log", runID), e.token, nil)
	logData := dataMap(t, out)
	text := logData["data"].(string)
	for _, want := range []string{"hello-from-deploy", "second-step", "=== Deploy Run #1 ==="} {
		if !strings.Contains(text, want) {
			t.Errorf("日志里缺少 %q\n---\n%s", want, text)
		}
	}
	if logData["eof"] != true {
		t.Errorf("运行结束后的日志 eof 应为 true: %v", logData)
	}

	// 步骤记录
	_, out = e.call(t, "GET", fmt.Sprintf("/api/v1/runs/%d", runID), e.token, nil)
	steps, _ := dataMap(t, out)["steps"].([]any)
	if len(steps) != 2 {
		t.Fatalf("应有 2 个步骤记录，实际 %d", len(steps))
	}

	// 第二次执行，buildNumber 递增
	_, out = e.call(t, "POST", "/api/v1/projects/demo-app/run", e.token, nil)
	if bn := dataMap(t, out)["buildNumber"].(float64); bn != 2 {
		t.Fatalf("第二次执行的 buildNumber 应为 2，实际 %v", bn)
	}
}

// ---------- 失败与取消 ----------

func TestFailureExitCode(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	e.call(t, "POST", "/api/v1/projects", e.token, map[string]any{
		"key": "failing", "name": "会失败", "workdir": "${WORKSPACE_ROOT}/failing",
		"commands": []map[string]any{
			{"name": "失败命令", "script": "exit 3"},
			{"name": "不该执行", "script": "echo SHOULD-NOT-RUN"},
		},
	})
	_, out := e.call(t, "POST", "/api/v1/projects/failing/run", e.token, nil)
	runID := int(dataMap(t, out)["runId"].(float64))

	run := e.waitRun(t, runID, 60*time.Second)
	if run["status"] != store.RunFailed {
		t.Fatalf("状态应为 failed，实际 %v", run["status"])
	}
	if run["exitCode"].(float64) != 3 {
		t.Fatalf("退出码应为 3，实际 %v", run["exitCode"])
	}

	// abort 策略下，第二条命令不能被执行 —— 这是最容易出错的地方
	_, out = e.call(t, "GET", fmt.Sprintf("/api/v1/runs/%d/log", runID), e.token, nil)
	if strings.Contains(dataMap(t, out)["data"].(string), "SHOULD-NOT-RUN") {
		t.Fatal("失败后仍然执行了后续命令，abort 策略没生效")
	}
}

func TestCancelRunningRun(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	e.call(t, "POST", "/api/v1/projects", e.token, map[string]any{
		"key": "slow", "name": "慢任务", "workdir": "${WORKSPACE_ROOT}/slow",
		"commands": []map[string]any{
			{"name": "睡很久", "script": sleepScript(60)},
		},
	})
	_, out := e.call(t, "POST", "/api/v1/projects/slow/run", e.token, nil)
	runID := int(dataMap(t, out)["runId"].(float64))

	// 等它真的跑起来再取消
	e.waitStatus(t, runID, store.RunRunning, 20*time.Second)

	start := time.Now()
	_, out = e.call(t, "POST", fmt.Sprintf("/api/v1/runs/%d/cancel", runID), e.token, nil)
	if out["success"] != true {
		t.Fatalf("取消失败: %v", out)
	}
	run := e.waitFinal(t, runID, 30*time.Second)
	if run["status"] != store.RunCanceled {
		t.Fatalf("状态应为 canceled，实际 %v（错误：%v）", run["status"], run["errorMsg"])
	}
	if elapsed := time.Since(start); elapsed > 25*time.Second {
		t.Fatalf("取消用了 %s，进程没有被及时终止", elapsed)
	}

	// 取消是幂等的
	code, out := e.call(t, "POST", fmt.Sprintf("/api/v1/runs/%d/cancel", runID), e.token, nil)
	if code != 200 || out["success"] != true {
		t.Fatalf("重复取消应幂等成功: %d %v", code, out)
	}
}

// 排队中的 Run 也必须能取消：以前没有进程句柄就直接返回 false，
// 结果是前一条卡死的运行会把后面所有运行永远堵在 queued。
func TestCancelQueuedRun(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	e.call(t, "POST", "/api/v1/projects", e.token, map[string]any{
		"key": "serial", "name": "串行", "workdir": "${WORKSPACE_ROOT}/serial",
		"concurrency": store.ConcurrencyQueue,
		"commands": []map[string]any{
			{"name": "睡很久", "script": sleepScript(60)},
		},
	})

	_, out := e.call(t, "POST", "/api/v1/projects/serial/run", e.token, nil)
	firstID := int(dataMap(t, out)["runId"].(float64))
	e.waitStatus(t, firstID, store.RunRunning, 20*time.Second)

	_, out = e.call(t, "POST", "/api/v1/projects/serial/run", e.token, nil)
	secondID := int(dataMap(t, out)["runId"].(float64))
	e.waitStatus(t, secondID, store.RunQueued, 5*time.Second)

	if code, out := e.call(t, "POST", fmt.Sprintf("/api/v1/runs/%d/cancel", secondID), e.token, nil); code != 200 || out["success"] != true {
		t.Fatalf("取消排队中的运行失败: %d %v", code, out)
	}
	if run := e.waitFinal(t, secondID, 10*time.Second); run["status"] != store.RunCanceled {
		t.Fatalf("排队中的运行应变成 canceled，实际 %v", run["status"])
	}

	// 第一条还在跑，取消掉，别让测试等满 60 秒。
	e.call(t, "POST", fmt.Sprintf("/api/v1/runs/%d/cancel", firstID), e.token, nil)
	if run := e.waitFinal(t, firstID, 30*time.Second); run["status"] != store.RunCanceled {
		t.Fatalf("第一条运行取消失败: %v", run["status"])
	}
}

// ---------- Secret 打码 ----------

func TestSecretIsMaskedInLog(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	const secretValue = "top-secret-token-value"
	code, out := e.call(t, "POST", "/api/v1/projects", e.token, map[string]any{
		"key": "secrets", "name": "含密钥", "workdir": "${WORKSPACE_ROOT}/secrets",
		"commands": []map[string]any{
			// 把密钥显式打印出来：这是最坏情况，也必须被打码
			{"name": "打印密钥", "script": "echo " + secretValue},
		},
		"envs": []map[string]any{
			{"key": "MY_TOKEN", "value": secretValue, "isSecret": true},
		},
	})
	if code != 200 {
		t.Fatalf("创建项目失败: %d %v", code, out)
	}

	// 出参里不能带 Secret 明文
	proj := dataMap(t, out)
	envs, _ := proj["envs"].([]any)
	if len(envs) != 1 {
		t.Fatalf("环境变量数量不对: %v", envs)
	}
	if v := envs[0].(map[string]any)["value"]; v != "" {
		t.Fatalf("Secret 的值不应出现在响应里，得到 %q", v)
	}

	_, out = e.call(t, "POST", "/api/v1/projects/secrets/run", e.token, nil)
	runID := int(dataMap(t, out)["runId"].(float64))
	e.waitFinal(t, runID, 60*time.Second)

	_, out = e.call(t, "GET", fmt.Sprintf("/api/v1/runs/%d/log", runID), e.token, nil)
	text := dataMap(t, out)["data"].(string)
	if strings.Contains(text, secretValue) {
		t.Fatalf("Secret 明文泄漏到日志:\n%s", text)
	}
	if !strings.Contains(text, "***") {
		t.Fatalf("日志里看不到打码痕迹:\n%s", text)
	}
}

// ---------- Hook 触发 ----------

func TestHookTrigger(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	e.call(t, "POST", "/api/v1/projects", e.token, map[string]any{
		"key": "hooked", "name": "被 Hook 触发", "workdir": "${WORKSPACE_ROOT}/hooked",
		"commands": []map[string]any{
			{"name": "输出变量", "script": "echo BUILD=" + envRef("BUILD_NUMBER") + " EXTRA=" + envRef("EXTRA_PARAM")},
		},
	})

	// 生成 Hook 令牌
	code, out := e.call(t, "POST", "/api/v1/projects/hooked/hooks", e.token,
		map[string]string{"name": "测试令牌"})
	if code != 200 {
		t.Fatalf("创建 Hook 失败: %d %v", code, out)
	}
	hook := dataMap(t, out)
	plain, _ := hook["token"].(string)
	if !strings.HasPrefix(plain, "hk_") {
		t.Fatalf("Hook 明文格式不对: %q", plain)
	}

	// 列表里不能出现明文
	_, out = e.call(t, "GET", "/api/v1/projects/hooked/hooks", e.token, nil)
	listRaw, _ := json.Marshal(out["data"])
	if strings.Contains(string(listRaw), plain) {
		t.Fatal("Hook 列表不应返回明文令牌")
	}

	// 免登录触发，默认异步 → HTTP 202
	code, out = e.call(t, "POST", "/api/v1/hooks/"+plain, "", map[string]any{
		"env": map[string]string{"EXTRA_PARAM": "from-hook"},
	})
	if code != 202 {
		t.Fatalf("异步 Hook 应返回 202，得到 %d %v", code, out)
	}
	runID := int(dataMap(t, out)["runId"].(float64))

	run := e.waitRun(t, runID, 60*time.Second)
	if run["status"] != store.RunSuccess {
		t.Fatalf("Hook 触发的执行应成功，实际 %v", run["status"])
	}
	if run["triggerType"] != store.TriggerHook {
		t.Fatalf("触发方式应为 hook，实际 %v", run["triggerType"])
	}

	_, out = e.call(t, "GET", fmt.Sprintf("/api/v1/runs/%d/log", runID), e.token, nil)
	text := dataMap(t, out)["data"].(string)
	if !strings.Contains(text, "BUILD=1") || !strings.Contains(text, "EXTRA=from-hook") {
		t.Fatalf("内置变量/触发参数没有注入:\n%s", text)
	}

	// 同步模式：等结束再返回
	code, out = e.call(t, "POST", "/api/v1/hooks/"+plain, "", map[string]any{"async": false})
	if code != 200 {
		t.Fatalf("同步 Hook 应返回 200，得到 %d %v", code, out)
	}
	if dataMap(t, out)["status"] != store.RunSuccess {
		t.Fatalf("同步 Hook 应返回最终状态: %v", out)
	}

	// 错误令牌 → 401
	if code, _ := e.call(t, "POST", "/api/v1/hooks/hk_invalid_token", "", nil); code != 401 {
		t.Fatalf("无效 Hook 令牌应返回 401，得到 %d", code)
	}
}

// TestHookTriggerViaHeader 覆盖「令牌只放请求头」的触发方式。
//
// 这条路径曾经是坏的：handler 里有「路径没有 token 就读 X-Deploy-Token」的退回逻辑，
// 但路由只注册了 /hooks/:token，于是 `/hooks/` 404、`/hooks/xxx` 的路径参数又永远非空，
// 那段逻辑成了死代码 —— 契约文档写了支持请求头，实际用不了。
// 修法是补一条不带参数的路由，这个用例就是防它再退化回去。
func TestHookTriggerViaHeader(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	e.call(t, "POST", "/api/v1/projects", e.token, map[string]any{
		"key": "hdr-hook", "name": "请求头触发", "workdir": "${WORKSPACE_ROOT}/hdr-hook",
		"commands": []map[string]any{{"name": "输出", "script": "echo header-hook-ok"}},
	})
	_, hookOut := e.call(t, "POST", "/api/v1/projects/hdr-hook/hooks", e.token,
		map[string]string{"name": "hdr"})
	hook := dataMap(t, hookOut)
	plain := hook["token"].(string)

	// 1) 路径里不带令牌，令牌只放请求头 → 应受理（202）
	code, out := e.callWithHeaders(t, "POST", "/api/v1/hooks",
		map[string]string{"X-Deploy-Token": plain}, map[string]any{"async": true})
	if code != 202 {
		t.Fatalf("仅请求头触发应返回 202，得到 %d %v", code, out)
	}
	runID := int(dataMap(t, out)["runId"].(float64))

	// 2) 末尾带斜杠也应能用（gin 的 RedirectTrailingSlash 会 307 过来）
	code, _ = e.callWithHeaders(t, "POST", "/api/v1/hooks/",
		map[string]string{"X-Deploy-Token": plain}, map[string]any{"async": true})
	if code != 202 {
		t.Fatalf("POST /api/v1/hooks/ 应返回 202，得到 %d", code)
	}

	// 3) 路径里给了错误令牌时，不能因为头部正确就放行（路径优先，避免两处凭据打架）
	code, _ = e.callWithHeaders(t, "POST", "/api/v1/hooks/hk_path_is_wrong",
		map[string]string{"X-Deploy-Token": plain}, nil)
	if code != 401 {
		t.Fatalf("路径令牌错误时应以路径为准并返回 401，得到 %d", code)
	}

	// 4) 请求头令牌错误 → 401
	code, _ = e.callWithHeaders(t, "POST", "/api/v1/hooks",
		map[string]string{"X-Deploy-Token": "hk_nope"}, nil)
	if code != 401 {
		t.Fatalf("错误请求头令牌应返回 401，得到 %d", code)
	}

	// 5) 这次触发真的跑起来了
	run := e.waitRun(t, runID, 60*time.Second)
	if run["status"] != store.RunSuccess {
		t.Fatalf("请求头触发应执行成功，实际 %v（%v）", run["status"], run["errorMsg"])
	}
	if run["triggerType"] != store.TriggerHook {
		t.Fatalf("触发方式应为 hook，实际 %v", run["triggerType"])
	}
}

// ---------- WebSocket 实时日志 ----------

func TestWebSocketStreamsLogAndExit(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	e.call(t, "POST", "/api/v1/projects", e.token, map[string]any{
		"key": "wsapp", "name": "WS 测试", "workdir": "${WORKSPACE_ROOT}/wsapp",
		"commands": []map[string]any{
			{"name": "输出", "script": "echo websocket-hello"},
		},
	})
	_, out := e.call(t, "POST", "/api/v1/projects/wsapp/run", e.token, nil)
	runID := int(dataMap(t, out)["runId"].(float64))

	wsURL := "ws" + strings.TrimPrefix(e.ts.URL, "http") +
		fmt.Sprintf("/api/v1/ws/runs/%d?token=%s", runID, e.token)
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("建立 WebSocket 失败（HTTP %d）: %v", status, err)
	}
	defer conn.Close()

	var all strings.Builder
	sawExit := false
	sawNextOffset := false
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}
		frame := map[string]any{}
		if err := json.Unmarshal(raw, &frame); err != nil {
			t.Fatalf("帧不是 JSON: %s", string(raw))
		}
		switch frame["type"] {
		case "history", "log":
			all.WriteString(frame["data"].(string))
			if _, ok := frame["nextOffset"]; !ok {
				t.Error("日志帧必须带 nextOffset，否则客户端无法续传")
			}
			sawNextOffset = true
		case "exit":
			sawExit = true
		}
		if sawExit {
			break
		}
	}

	if !sawNextOffset {
		t.Error("没有收到带 nextOffset 的日志帧")
	}
	if !strings.Contains(all.String(), "websocket-hello") {
		t.Errorf("WebSocket 没有推送到完整日志:\n%s", all.String())
	}
	if !sawExit {
		t.Error("没有收到 exit 帧")
	}

	// 未授权连接必须被拒绝（握手阶段返回 401）
	badURL := "ws" + strings.TrimPrefix(e.ts.URL, "http") +
		fmt.Sprintf("/api/v1/ws/runs/%d?token=bad", runID)
	if _, resp, err := websocket.DefaultDialer.Dial(badURL, nil); err == nil {
		t.Error("无有效 token 的 WebSocket 应被拒绝")
	} else if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("应以 401 拒绝，得到 %d", resp.StatusCode)
	}
}

// ---------- 权限 ----------

func TestPermissionIsolation(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	// 管理员建一个普通用户
	_, out := e.call(t, "POST", "/api/v1/users", e.token, map[string]any{
		"username": "bob", "password": "bob12345678", "nickname": "Bob", "role": "user",
	})
	if out["success"] != true {
		t.Fatalf("创建用户失败: %v", out)
	}

	// admin 建的项目
	e.call(t, "POST", "/api/v1/projects", e.token, map[string]any{
		"key": "admin-proj", "name": "管理员的项目", "workdir": "${WORKSPACE_ROOT}/admin-proj",
		"commands": []map[string]any{{"name": "x", "script": "echo x"}},
	})

	// bob 登录
	_, out = e.call(t, "POST", "/api/v1/auth/login", "", map[string]string{
		"username": "bob", "password": "bob12345678",
	})
	bobToken := out["data"].(map[string]any)["token"].(string)

	// bob 能看见（allReadable 默认），但不能改也不能执行
	code, out := e.call(t, "GET", "/api/v1/projects/admin-proj", bobToken, nil)
	if code != 200 || out["success"] != true {
		t.Fatalf("普通用户应能只读查看: %d %v", code, out)
	}
	if dataMap(t, out)["canEdit"] != false {
		t.Fatal("普通用户对自己以外的项目 canEdit 应为 false")
	}
	_, out = e.call(t, "POST", "/api/v1/projects/admin-proj/run", bobToken, nil)
	if out["success"] != false {
		t.Fatalf("普通用户不应能执行他人项目: %v", out)
	}
	// 也不能删
	_, out = e.call(t, "DELETE", "/api/v1/projects/admin-proj", bobToken, nil)
	if out["success"] != false {
		t.Fatalf("普通用户不应能删除他人项目: %v", out)
	}
	// 用户管理仅 admin
	if _, out = e.call(t, "GET", "/api/v1/users", bobToken, nil); out["success"] != false {
		t.Fatalf("普通用户不应能访问用户管理: %v", out)
	}
}

// ---------- 辅助 ----------

func sleepScript(sec int) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("Start-Sleep -Seconds %d", sec)
	}
	return fmt.Sprintf("sleep %d", sec)
}

// envRef 给出「引用一个环境变量」在当前平台 shell 下的正确写法。
//
// 这不是测试专用的小聪明，而是真实存在的跨平台差异：
// bash 写 ${NAME}，PowerShell 写 $env:NAME。测试用错语法会得到空串，
// 于是把一个平台的正确行为误判成 bug。文档里也标了这一点。
func envRef(name string) string {
	if runtime.GOOS == "windows" {
		return "$env:" + name
	}
	return "${" + name + "}"
}

func (e *testEnv) waitStatus(t *testing.T, runID int, status string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, out := e.call(t, "GET", fmt.Sprintf("/api/v1/runs/%d", runID), e.token, nil)
		if dataMap(t, out)["status"] == status {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("等待状态 %s 超时", status)
}

func (e *testEnv) waitFinal(t *testing.T, runID int, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last map[string]any
	for time.Now().Before(deadline) {
		_, out := e.call(t, "GET", fmt.Sprintf("/api/v1/runs/%d", runID), e.token, nil)
		last = dataMap(t, out)
		switch last["status"] {
		case store.RunSuccess, store.RunFailed, store.RunCanceled, store.RunTimeout, store.RunError:
			return last
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("等待运行结束超时，最后状态: %v", last["status"])
	return nil
}

func (e *testEnv) waitRun(t *testing.T, runID int, timeout time.Duration) map[string]any {
	return e.waitFinal(t, runID, timeout)
}
