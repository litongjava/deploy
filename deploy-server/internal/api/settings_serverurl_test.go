package api

import (
	"testing"
)

// 服务端外部地址是**运行时设置**：在设置页改完，同一个进程里的变量预览与后续运行
// 立刻用新地址，不需要重启服务（对外地址会随网络环境变化，锁在 unit 里就得为它重启）。
// 这里把「默认值 → 保存 → 立即生效 → 非法值拒绝 → 清空回退」整条链路钉住。
func TestServerURLRuntimeSetting(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	// 没设置过 → 用启动配置里的默认值（测试环境是 listen 推导出来的地址）
	_, out := e.call(t, "GET", "/api/v1/system/settings", e.token, nil)
	def, _ := dataMap(t, out)["serverUrl"].(string)
	if def == "" {
		t.Fatalf("默认 serverUrl 不该为空: %v", out)
	}
	if def != e.srv.cfg.ServerURL {
		t.Fatalf("默认 serverUrl = %q，期望启动配置里的 %q", def, e.srv.cfg.ServerURL)
	}

	createProject(t, e, map[string]any{
		"key": "addr-check", "name": "地址检查",
		"workdir":  "${WORKSPACE_ROOT}/${PROJECT_KEY}",
		"commands": []map[string]any{{"name": "noop", "script": "echo hi"}},
	})

	// 变量预览里读到的 DEPLOY_SERVER_URL 就是脚本真正会拿到的值。
	previewed := func() string {
		_, out := e.call(t, "POST", "/api/v1/projects/addr-check/preview", e.token, nil)
		envs, _ := dataMap(t, out)["builtinEnvs"].([]any)
		for _, raw := range envs {
			kv, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if kv["key"] == "DEPLOY_SERVER_URL" {
				v, _ := kv["value"].(string)
				return v
			}
		}
		t.Fatalf("变量预览里没有 DEPLOY_SERVER_URL: %v", out)
		return ""
	}
	if got := previewed(); got != def {
		t.Fatalf("预览应显示默认地址 %q，得到 %q", def, got)
	}

	// 保存新地址：结尾多余的 '/' 要去掉，免得脚本里拼出双斜杠。
	code, out := e.call(t, "PUT", "/api/v1/system/settings", e.token, map[string]any{
		"serverUrl":              "http://deploy.example.com:10055/",
		"maxConcurrentRuns":      5,
		"logRetentionDays":       30,
		"logMaxBytes":            104857600,
		"hookRateLimitPerMinute": 60,
		"visibility":             "allReadable",
		"notify":                 map[string]any{"enabled": false, "url": "", "on": []string{"failed"}},
	})
	if code != 200 {
		t.Fatalf("保存设置失败: %d %v", code, out)
	}
	if got, _ := dataMap(t, out)["serverUrl"].(string); got != "http://deploy.example.com:10055" {
		t.Fatalf("保存响应里的 serverUrl = %q", got)
	}

	// 重新读取（走库）与变量预览都要立刻是新地址，全程没有重启。
	_, out = e.call(t, "GET", "/api/v1/system/settings", e.token, nil)
	if got, _ := dataMap(t, out)["serverUrl"].(string); got != "http://deploy.example.com:10055" {
		t.Fatalf("重新读取的 serverUrl = %q", got)
	}
	if got := previewed(); got != "http://deploy.example.com:10055" {
		t.Fatalf("变量预览没跟上新地址: %q", got)
	}

	// 非法地址必须被拒，并且不能把已存的值改坏（校验发生在任何写库之前）。
	// 按契约，参数错误是 HTTP 200 + success=false + errorCode 40000（见 response.go）。
	for _, bad := range []string{"deploy.example.com:10055", "ftp://deploy.example.com", "http://", "http://a b"} {
		code, out = e.call(t, "PUT", "/api/v1/system/settings", e.token, map[string]any{"serverUrl": bad})
		if code != 200 || out["success"] != false {
			t.Fatalf("非法地址 %q 应被拒绝，得到 %d %v", bad, code, out)
		}
		if ec, _ := out["errorCode"].(float64); ec != 40000 {
			t.Fatalf("非法地址 %q 的错误码应为 40000，得到 %v", bad, out["errorCode"])
		}
	}
	_, out = e.call(t, "GET", "/api/v1/system/settings", e.token, nil)
	if got, _ := dataMap(t, out)["serverUrl"].(string); got != "http://deploy.example.com:10055" {
		t.Fatalf("非法请求不该改动已存的地址: %q", got)
	}

	// 清空 → 回退到启动配置里的默认值，而不是变成一个空地址。
	code, out = e.call(t, "PUT", "/api/v1/system/settings", e.token, map[string]any{"serverUrl": "   "})
	if code != 200 {
		t.Fatalf("清空地址失败: %d %v", code, out)
	}
	if got, _ := dataMap(t, out)["serverUrl"].(string); got != def {
		t.Fatalf("清空后应回退到默认值 %q，得到 %q", def, got)
	}
}
