package api

import (
	"io"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"
)

// 环境工具链一键安装：清单、权限、命令拼装、脚本下载。
//
// 一律用 dryRun 触发，保证测试**绝不会真的去装东西**（脚本的 dry-run 会跳过所有
// 有副作用的动作）；真正执行的安装由人在页面上点。
func TestToolchainEndpoints(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	// 1) 清单：包含全部组件、给出各自状态
	code, out := e.call(t, "GET", "/api/v1/system/toolchains", e.token, nil)
	if code != 200 {
		t.Fatalf("清单接口失败: %d %v", code, out)
	}
	data := dataMap(t, out)
	comps, _ := data["components"].([]any)
	if len(comps) < 8 {
		t.Fatalf("组件数量不对: %v", data["components"])
	}
	keys := map[string]bool{}
	for _, item := range comps {
		m := item.(map[string]any)
		k, _ := m["key"].(string)
		keys[k] = true
		for _, f := range []string{"name", "note", "installed"} {
			if _, ok := m[f]; !ok {
				t.Errorf("组件 %s 缺少字段 %s: %v", k, f, m)
			}
		}
	}
	for _, want := range []string{"cc", "java", "maven", "nvm", "node", "pnpm", "go", "rust"} {
		if !keys[want] {
			t.Errorf("清单里缺少组件 %s", want)
		}
	}
	if _, ok := data["scriptUrl"].(string); !ok {
		t.Errorf("清单应给出脚本下载地址: %v", data)
	}

	// 2) 未知组件必须被拒绝（防注入：组件名直接进命令行）
	_, out = e.call(t, "POST", "/api/v1/system/toolchains/install", e.token, map[string]any{
		"components": []string{"go; rm -rf /"},
	})
	assertBadRequest(t, out, "unknown component")

	// 3) dry-run 触发：应创建内置系统项目与一条命令，并产生一个 Run
	code, out = e.call(t, "POST", "/api/v1/system/toolchains/install", e.token, map[string]any{
		"components": []string{"go", "pnpm"}, "dryRun": true,
	})
	if code != 200 || out["success"] != true {
		t.Fatalf("触发安装失败: %d %v", code, out)
	}
	install := dataMap(t, out)
	runID := int(install["runId"].(float64))
	if runID == 0 {
		t.Fatalf("应返回 runId: %v", install)
	}
	if got, _ := install["projectKey"].(string); got != "system-toolchain" {
		t.Fatalf("应使用内置系统项目，实际 %v", got)
	}
	cmdText, _ := install["command"].(string)
	for _, want := range []string{"install.sh", "--only go,pnpm", "--dry-run"} {
		if !strings.Contains(cmdText, want) {
			t.Errorf("安装命令 %q 缺少 %q", cmdText, want)
		}
	}
	// 等它跑完：一来确认 dry-run 不会失败得太离谱，二来别让运行中的日志文件
	// 卡住 t.TempDir 的清理（Windows 上会直接报“文件被占用”）。
	e.waitRun(t, runID, 60*time.Second)

	// 4) 内置项目与命令要真的落库，且命令脚本可复用
	_, out = e.call(t, "GET", "/api/v1/projects/system-toolchain", e.token, nil)
	proj := dataMap(t, out)
	cmds, _ := proj["commands"].([]any)
	if len(cmds) != 1 {
		t.Fatalf("系统项目应只有一条命令: %v", proj["commands"])
	}
	script, _ := cmds[0].(map[string]any)["script"].(string)
	if !strings.Contains(script, "install.sh") || !strings.Contains(script, "--only") {
		t.Fatalf("命令脚本不对: %q", script)
	}
	// 系统项目不给 Hook（避免被外部触发）
	if hook, _ := proj["hookEnabled"].(bool); hook {
		t.Errorf("系统项目不应启用 Hook: %v", proj["hookEnabled"])
	}
	// 再触发一次必须复用同一个命令（不新增行）
	_, out = e.call(t, "POST", "/api/v1/system/toolchains/install", e.token, map[string]any{
		"components": []string{"java", "maven"}, "dryRun": true,
	})
	if out["success"] != true {
		t.Fatalf("第二次触发失败: %v", out)
	}
	e.waitRun(t, int(dataMap(t, out)["runId"].(float64)), 60*time.Second)
	_, out = e.call(t, "GET", "/api/v1/projects/system-toolchain", e.token, nil)
	if cmds, _ := dataMap(t, out)["commands"].([]any); len(cmds) != 1 {
		t.Fatalf("重复触发不应新增命令行: %v", cmds)
	}
	if script, _ := dataMap(t, out)["commands"].([]any)[0].(map[string]any)["script"].(string); !strings.Contains(script, "--only java,maven") {
		t.Fatalf("命令脚本应被更新成最新一次选择: %q", script)
	}

	// 5) 非 Linux 平台：真安装要被挡下，只留 dry-run
	if runtime.GOOS != "linux" {
		_, out = e.call(t, "POST", "/api/v1/system/toolchains/install", e.token, map[string]any{
			"components": []string{"go"},
		})
		if out["success"] != false {
			t.Fatalf("%s 上真安装应被拒绝: %v", runtime.GOOS, out)
		}
	}

	// 6) 脚本下载接口要返回真正的脚本内容
	body := e.getText(t, "/api/v1/system/toolchains/script", e.token)
	for _, want := range []string{"ALL_COMPONENTS=", "--dry-run", "install_nvm", "install_go", "install_rust"} {
		if !strings.Contains(body, want) {
			t.Errorf("下载到的脚本缺少 %q", want)
		}
	}

	// 7) 非管理员不能触发安装（改设置、装软件都是管理员动作）
	e.call(t, "POST", "/api/v1/users", e.token, map[string]any{
		"username": "bob", "password": "bob12345678", "role": "user",
	})
	_, out = e.call(t, "POST", "/api/v1/auth/login", "", map[string]any{
		"username": "bob", "password": "bob12345678",
	})
	bobToken, _ := dataMap(t, out)["token"].(string)
	if bobToken == "" {
		t.Fatalf("普通用户登录失败: %v", out)
	}
	// 清单可以看（登录即可）
	if code, _ := e.call(t, "GET", "/api/v1/system/toolchains", bobToken, nil); code != 200 {
		t.Fatalf("普通用户应能查看清单，得到 %d", code)
	}
	// 安装不行
	_, out = e.call(t, "POST", "/api/v1/system/toolchains/install", bobToken, map[string]any{
		"components": []string{"go"}, "dryRun": true,
	})
	if out["success"] != false || !strings.Contains(out["errorMessage"].(string), "admin") {
		t.Fatalf("普通用户不应能触发安装: %v", out)
	}
}

// getText 取一个纯文本接口的响应体（e.call 只解析 JSON）。
func (e *testEnv) getText(t *testing.T, path, token string) string {
	t.Helper()
	req, err := http.NewRequest("GET", e.ts.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("%s 返回 %d: %s", path, resp.StatusCode, string(raw))
	}
	return string(raw)
}
