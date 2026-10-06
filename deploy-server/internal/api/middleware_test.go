package api

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"deploy-server/internal/middleware"
)

// 中间件（docker/postgresql/mysql/redis/elasticsearch）：清单、权限、命令拼装、脚本下载。
//
// 与 toolchain_test.go 同一条原则：一律用 dryRun 触发，保证测试**绝不会真的去装东西**
// （脚本的 dry-run 会跳过所有有副作用的动作）；真正的安装由人在页面上点。
func TestMiddlewareEndpoints(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	// 1) 清单：包含全部中间件、给出各自状态、方式与动作表
	code, out := e.call(t, "GET", "/api/v1/system/middleware", e.token, nil)
	if code != 200 {
		t.Fatalf("清单接口失败: %d %v", code, out)
	}
	data := dataMap(t, out)
	comps, _ := data["components"].([]any)
	if len(comps) != 5 {
		t.Fatalf("中间件数量应为 5，实际 %v", len(comps))
	}
	keys := map[string]bool{}
	byKey := map[string]map[string]any{}
	for _, item := range comps {
		m := item.(map[string]any)
		k, _ := m["key"].(string)
		keys[k] = true
		byKey[k] = m
		for _, f := range []string{"name", "note", "installed", "running", "modes", "defaultMode", "mode", "effectiveMode", "nativeSupported", "fields"} {
			if _, ok := m[f]; !ok {
				t.Errorf("中间件 %s 缺少字段 %s: %v", k, f, m)
			}
		}
	}
	for _, want := range []string{"docker", "postgresql", "mysql", "redis", "elasticsearch"} {
		if !keys[want] {
			t.Errorf("清单里缺少中间件 %s", want)
		}
	}
	// docker 必须排第一：其余四项默认都跑在它的容器里，「一键安装缺失」按这个顺序执行。
	if k, _ := comps[0].(map[string]any)["key"].(string); k != "docker" {
		t.Errorf("docker 应排在第一位，实际 %v", k)
	}
	// elasticsearch 只支持容器方式，必须显式标出来（页面据此置灰，而不是点了才失败）。
	if es := byKey["elasticsearch"]; es["nativeSupported"] != false {
		t.Errorf("elasticsearch 应标记为不支持 native: %v", es["nativeSupported"])
	} else if modes, _ := es["modes"].([]any); len(modes) != 1 || modes[0] != "docker" {
		t.Errorf("elasticsearch 只应有 docker 一种方式: %v", es["modes"])
	}
	// docker 本体只支持原生方式。
	if d := byKey["docker"]; d["defaultMode"] != "native" {
		t.Errorf("docker 默认方式应为 native: %v", d["defaultMode"])
	}
	if _, ok := data["scriptUrl"].(string); !ok {
		t.Errorf("清单应给出脚本下载地址: %v", data)
	}
	if actions, _ := data["actions"].([]any); len(actions) == 0 {
		t.Errorf("清单应给出可用动作: %v", data["actions"])
	}

	// 1b) 探测项不能「一荣俱荣」：容器探测与原生命令探测是两条互斥的事实，
	// 全标成命中会让人看到「容器 ✔ + psql ✔」这种自相矛盾的面板。
	for _, item := range comps {
		m := item.(map[string]any)
		installed, _ := m["installed"].(bool)
		mode, _ := m["effectiveMode"].(string)
		probes, _ := m["probes"].([]any)
		for _, p := range probes {
			pm := p.(map[string]any)
			name, _ := pm["name"].(string)
			found, _ := pm["found"].(bool)
			if !found || !installed {
				continue
			}
			if strings.HasPrefix(name, "容器 ") && mode != "docker" {
				t.Errorf("%s 的容器探测标记为命中，但实际方式是 %s", m["key"], mode)
			}
			if !strings.HasPrefix(name, "容器 ") && mode != "native" {
				t.Errorf("%s 的原生探测 %q 标记为命中，但实际方式是 %s", m["key"], name, mode)
			}
		}
	}

	// 2) 未知中间件必须被拒绝（名字直接进命令行，不能放行）
	_, out = e.call(t, "POST", "/api/v1/system/middleware/install", e.token, map[string]any{
		"components": []string{"redis; rm -rf /"},
	})
	assertBadRequest(t, out, "unknown middleware")
	_, out = e.call(t, "POST", "/api/v1/system/middleware/install", e.token, map[string]any{
		"components": []string{"postgresql"}, "action": "drop-tables",
	})
	assertBadRequest(t, out, "unknown action")
	_, out = e.call(t, "POST", "/api/v1/system/middleware/install", e.token, map[string]any{
		"components": []string{"redis"}, "mode": "podman",
	})
	assertBadRequest(t, out, "mode must be")

	// 3) 启停类动作必须点名组件：空列表只对 install 有意义，
	//    否则「清空勾选后点了停止」会意外把所有中间件停掉。
	_, out = e.call(t, "POST", "/api/v1/system/middleware/install", e.token, map[string]any{
		"components": []string{}, "action": "stop", "dryRun": true,
	})
	assertBadRequest(t, out, "must not be empty")

	// 4) 不支持的组合要提前拦掉，而不是等脚本在日志里说「这一项被跳过了」。
	//    注意：这里必须带上 mode=native，否则不会命中「native 不支持」这条校验，
	//    在非 Linux 平台上会先被 dry-run 放行、再被平台校验拦下，测不到想测的分支。
	_, out = e.call(t, "POST", "/api/v1/system/middleware/install", e.token, map[string]any{
		"components": []string{"elasticsearch"}, "mode": "native", "dryRun": true,
	})
	assertBadRequest(t, out, "does not support the native mode (available: docker)")

	// 5) dry-run 触发：应创建内置系统项目与一条命令，并产生一个 Run
	code, out = e.call(t, "POST", "/api/v1/system/middleware/install", e.token, map[string]any{
		"components": []string{"redis", "postgresql"}, "mode": "docker", "dryRun": true,
	})
	if code != 200 || out["success"] != true {
		t.Fatalf("触发中间件安装失败: %d %v", code, out)
	}
	install := dataMap(t, out)
	runID := int(install["runId"].(float64))
	if runID == 0 {
		t.Fatalf("应返回 runId: %v", install)
	}
	if got, _ := install["projectKey"].(string); got != "system-middleware" {
		t.Fatalf("应使用内置中间件项目，实际 %v", got)
	}
	cmdText, _ := install["command"].(string)
	for _, want := range []string{"install.sh", "--only redis,postgresql", "--action install", "--dry-run",
		"--mode-of redis=docker", "--mode-of postgresql=docker"} {
		if !strings.Contains(cmdText, want) {
			t.Errorf("安装命令 %q 缺少 %q", cmdText, want)
		}
	}
	// 逐组件的方式要与注入的运行参数一致（脚本按 --mode-of 取值，参数用于回看）
	_, out = e.call(t, "GET", fmt.Sprintf("/api/v1/runs/%d", runID), e.token, nil)
	params, _ := dataMap(t, out)["params"].(map[string]any)
	if params["mode.redis"] != "docker" || params["mode.postgresql"] != "docker" {
		t.Errorf("运行参数里应记录每个组件的方式，实际 %v", params)
	}
	if params["components"] != "redis,postgresql" {
		t.Errorf("运行参数里应记录本次处理的组件，实际 %v", params["components"])
	}
	if params["action"] != "install" {
		t.Errorf("运行参数里应记录动作，实际 %v", params["action"])
	}
	e.waitRun(t, runID, 60*time.Second)

	// 6) 内置项目与命令要真的落库，且命令脚本可复用
	_, out = e.call(t, "GET", "/api/v1/projects/system-middleware", e.token, nil)
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
	// 再触发一次必须复用同一个命令（不新增行），且脚本被更新成最新一次选择
	_, out = e.call(t, "POST", "/api/v1/system/middleware/install", e.token, map[string]any{
		"components": []string{"mysql"}, "dryRun": true,
	})
	if out["success"] != true {
		t.Fatalf("第二次触发失败: %v", out)
	}
	e.waitRun(t, int(dataMap(t, out)["runId"].(float64)), 60*time.Second)
	_, out = e.call(t, "GET", "/api/v1/projects/system-middleware", e.token, nil)
	cmds, _ = dataMap(t, out)["commands"].([]any)
	if len(cmds) != 1 {
		t.Fatalf("重复触发不应新增命令行: %v", cmds)
	}
	if s, _ := cmds[0].(map[string]any)["script"].(string); !strings.Contains(s, "--only mysql") {
		t.Fatalf("命令脚本应被更新成最新一次选择: %q", s)
	}

	// 7) 非 Linux 平台：真安装要被挡下，只留 dry-run
	if runtime.GOOS != "linux" {
		_, out = e.call(t, "POST", "/api/v1/system/middleware/install", e.token, map[string]any{
			"components": []string{"redis"},
		})
		if out["success"] != false {
			t.Fatalf("%s 上真安装应被拒绝: %v", runtime.GOOS, out)
		}
	}

	// 8) 脚本下载接口要返回真正的脚本内容
	body := e.getText(t, "/api/v1/system/middleware/script", e.token)
	for _, want := range []string{"ALL_COMPONENTS=", "--dry-run", "install_postgresql", "install_mysql",
		"install_redis", "install_elasticsearch", "install_docker_native"} {
		if !strings.Contains(body, want) {
			t.Errorf("下载到的脚本缺少 %q", want)
		}
	}
	// 页面清单（Go 里的 Components）与脚本的 ALL_COMPONENTS 必须**逐项同序**一致。
	// 对不上就会出现「面板里有一项、脚本却不认」这种自相矛盾的提示。
	if got := scriptAllComponents(t, body); !reflect.DeepEqual(got, middleware.AllKeys()) {
		t.Errorf("脚本 ALL_COMPONENTS 与页面组件清单不一致：\n脚本：%v\n页面：%v", got, middleware.AllKeys())
	}

	// 9) 非管理员不能触发（装软件是管理员动作），但清单可以看
	e.call(t, "POST", "/api/v1/users", e.token, map[string]any{
		"username": "dave", "password": "dave12345678", "role": "user",
	})
	_, out = e.call(t, "POST", "/api/v1/auth/login", "", map[string]any{
		"username": "dave", "password": "dave12345678",
	})
	daveToken, _ := dataMap(t, out)["token"].(string)
	if daveToken == "" {
		t.Fatalf("普通用户登录失败: %v", out)
	}
	if code, _ := e.call(t, "GET", "/api/v1/system/middleware", daveToken, nil); code != 200 {
		t.Fatalf("普通用户应能查看清单，得到 %d", code)
	}
	_, out = e.call(t, "POST", "/api/v1/system/middleware/install", daveToken, map[string]any{
		"components": []string{"redis"}, "dryRun": true,
	})
	if out["success"] != false || !strings.Contains(out["errorMessage"].(string), "admin") {
		t.Fatalf("普通用户不应能触发安装: %v", out)
	}
}

// 中间件的安装参数：能改、能落库、密码加密且不回明文、方式可选，并且只有管理员能改。
func TestMiddlewareParams(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	// 1) 清单里密码类参数必须标成 secret，且初始「未设置」
	_, out := e.call(t, "GET", "/api/v1/system/middleware", e.token, nil)
	comps, _ := dataMap(t, out)["components"].([]any)
	var pg map[string]any
	for _, item := range comps {
		m := item.(map[string]any)
		if m["key"] == "postgresql" {
			pg = m
		}
	}
	if pg == nil {
		t.Fatal("清单里没有 postgresql")
	}
	byField := map[string]map[string]any{}
	for _, f := range pg["fields"].([]any) {
		fm := f.(map[string]any)
		byField[fm["key"].(string)] = fm
	}
	for _, want := range []string{"PG_VERSION", "PG_PORT", "PG_USER", "PG_PASSWORD"} {
		if _, ok := byField[want]; !ok {
			t.Fatalf("postgresql 缺少参数 %s（现有 %v）", want, byField)
		}
	}
	if byField["PG_VERSION"]["default"] != "16" {
		t.Errorf("PG_VERSION 默认值应为 16，实际 %v", byField["PG_VERSION"]["default"])
	}
	if byField["PG_PASSWORD"]["secret"] != true {
		t.Errorf("PG_PASSWORD 应标记为 secret: %v", byField["PG_PASSWORD"])
	}
	if byField["PG_PASSWORD"]["set"] != false {
		t.Errorf("初始不应有已保存的密码: %v", byField["PG_PASSWORD"])
	}

	// 2) 未知参数名必须拒绝（参数名会作为环境变量注入脚本）
	_, out = e.call(t, "PUT", "/api/v1/system/middleware/postgresql", e.token, map[string]any{
		"fields": map[string]string{"NOT_A_FIELD": "x"},
	})
	assertBadRequest(t, out, "has no parameter")
	_, out = e.call(t, "PUT", "/api/v1/system/middleware/ghost", e.token, map[string]any{
		"fields": map[string]string{"A": "b"},
	})
	assertBadRequest(t, out, "unknown middleware")
	// 不支持的方式也要拒绝
	_, out = e.call(t, "PUT", "/api/v1/system/middleware/elasticsearch", e.token, map[string]any{
		"fields": map[string]string{}, "mode": "native",
	})
	assertBadRequest(t, out, "does not support mode")

	// 3) 保存端口与密码，再读回来：普通参数回值，**密码只回 set，不回明文**
	_, out = e.call(t, "PUT", "/api/v1/system/middleware/postgresql", e.token, map[string]any{
		"fields": map[string]string{"PG_PORT": "15432", "PG_PASSWORD": "s3cr3t-pw"},
		"mode":   "native",
	})
	if out["success"] != true {
		t.Fatalf("保存参数失败: %v", out)
	}
	respFields := map[string]map[string]any{}
	for _, f := range dataMap(t, out)["fields"].([]any) {
		fm := f.(map[string]any)
		respFields[fm["key"].(string)] = fm
	}
	if respFields["PG_PORT"]["value"] != "15432" {
		t.Fatalf("保存后的端口不对: %v", respFields["PG_PORT"])
	}
	if respFields["PG_PASSWORD"]["set"] != true {
		t.Fatalf("密码保存后应标记为已设置: %v", respFields["PG_PASSWORD"])
	}
	if v, _ := respFields["PG_PASSWORD"]["value"].(string); v != "" {
		t.Fatalf("密码明文绝不能出参，实际返回了 %q", v)
	}
	if got, _ := dataMap(t, out)["mode"].(string); got != "native" {
		t.Fatalf("方式应保存为 native，实际 %v", got)
	}
	// 密文确实落库了，但**不是**明文（否则备份库文件即泄露数据库口令）
	raw, _ := e.st.GetSetting("middleware.postgresql")
	if strings.Contains(raw, "s3cr3t-pw") {
		t.Fatalf("settings 表里出现了明文密码: %s", raw)
	}
	if !strings.Contains(raw, "secrets") {
		t.Fatalf("settings 表里应有加密后的 secrets 段: %s", raw)
	}

	// 4) 只改端口（不回传密码）时，原密码必须保留
	_, out = e.call(t, "PUT", "/api/v1/system/middleware/postgresql", e.token, map[string]any{
		"fields": map[string]string{"PG_PORT": "15433"},
	})
	if out["success"] != true {
		t.Fatalf("只改端口失败: %v", out)
	}
	for _, f := range dataMap(t, out)["fields"].([]any) {
		fm := f.(map[string]any)
		if fm["key"] == "PG_PASSWORD" && fm["set"] != true {
			t.Fatalf("只改端口不应清掉密码: %v", fm)
		}
	}

	// 5) 安装时把保存的参数（含解密后的密码）注入运行参数，
	//    这样「这次按什么参数装的」在运行详情页可回看，随机生成的密码也能找回
	_, out = e.call(t, "POST", "/api/v1/system/middleware/install", e.token, map[string]any{
		"components": []string{"postgresql"}, "dryRun": true,
	})
	if out["success"] != true {
		t.Fatalf("触发安装失败: %v", out)
	}
	runID := int(dataMap(t, out)["runId"].(float64))
	_, out = e.call(t, "GET", fmt.Sprintf("/api/v1/runs/%d", runID), e.token, nil)
	params, _ := dataMap(t, out)["params"].(map[string]any)
	if params["PG_PORT"] != "15433" {
		t.Errorf("运行参数里应带上 PG_PORT，实际 %v", params)
	}
	if params["PG_PASSWORD"] != "s3cr3t-pw" {
		t.Errorf("运行参数里应带上解密后的密码（脚本据此初始化数据库），实际 %v", params["PG_PASSWORD"])
	}
	// 保存的是 native，所以脚本也应按 native 跑（配置生效，而不是被忽略）
	if params["mode.postgresql"] != "native" {
		t.Errorf("保存的安装方式应生效，实际 %v", params["mode.postgresql"])
	}
	e.waitRun(t, runID, 60*time.Second)

	// 6) 显式传空串 = 清空密码（回到「随机生成」）
	_, out = e.call(t, "PUT", "/api/v1/system/middleware/postgresql", e.token, map[string]any{
		"fields": map[string]string{"PG_PASSWORD": ""},
	})
	if out["success"] != true {
		t.Fatalf("清空密码失败: %v", out)
	}
	for _, f := range dataMap(t, out)["fields"].([]any) {
		fm := f.(map[string]any)
		if fm["key"] == "PG_PASSWORD" && fm["set"] != false {
			t.Fatalf("传空串应清空密码: %v", fm)
		}
	}

	// 7) 只有管理员能改参数
	e.call(t, "POST", "/api/v1/users", e.token, map[string]any{
		"username": "erin", "password": "erin12345678", "role": "user",
	})
	_, out = e.call(t, "POST", "/api/v1/auth/login", "", map[string]any{
		"username": "erin", "password": "erin12345678",
	})
	erinToken, _ := dataMap(t, out)["token"].(string)
	_, out = e.call(t, "PUT", "/api/v1/system/middleware/postgresql", erinToken, map[string]any{
		"fields": map[string]string{"PG_PORT": "5433"},
	})
	if out["success"] != false {
		t.Fatalf("普通用户不应能改安装参数: %v", out)
	}
}

// 组件名 → 容器名 / 卷名的前缀必须与内嵌脚本里的默认值一致：
// 平台报告的容器名和脚本实际操作的不是同一个，是很隐蔽的一类 bug。
func TestMiddlewareContainerNamingMatchesScript(t *testing.T) {
	body, err := middleware.Script()
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	for _, want := range []string{
		`CONTAINER_PREFIX="${CONTAINER_PREFIX:-` + middleware.ContainerPrefix + `}"`,
		`VOLUME_PREFIX="${VOLUME_PREFIX:-` + middleware.VolumePrefix + `}"`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("脚本里缺少与 Go 侧一致的默认值：%s", want)
		}
	}
	for _, comp := range middleware.Components {
		if comp.Key == "docker" {
			continue
		}
		// 脚本的 component_name 覆盖了每一个组件，避免出现「Go 里有、脚本里没有」的哑项。
		if !strings.Contains(script, comp.Key+")") {
			t.Errorf("脚本里没有处理组件 %s", comp.Key)
		}
	}
}
