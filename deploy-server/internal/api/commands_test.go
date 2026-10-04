package api

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// 布尔字段带 `default:true` 时 GORM 的 Create 会跳过 false 零值，库里落成 true。
// 这里钉住三处必须显式落库的地方：新建项目关 Hook / 关变量覆盖、新建禁用命令、
// 批量同步里新增禁用命令（否则「禁用」只是个假象，命令照样会执行）。
func TestBooleanDefaultsPersist(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	proj := createProject(t, e, map[string]any{
		"key": "flag-check", "name": "布尔默认值",
		"workdir":          "${WORKSPACE_ROOT}/${PROJECT_KEY}",
		"hookEnabled":      false,
		"allowEnvOverride": false,
		"commands": []map[string]any{
			{"name": "disabled", "script": "echo never-run", "enabled": false, "failStrategy": "abort"},
		},
	})
	if proj["hookEnabled"] != false || proj["allowEnvOverride"] != false {
		t.Fatalf("创建响应里的布尔值就不对: %v", proj)
	}

	// 重新读取（走数据库）必须还是 false
	_, out := e.call(t, "GET", "/api/v1/projects/flag-check", e.token, nil)
	d := dataMap(t, out)
	if d["hookEnabled"] != false {
		t.Errorf("hookEnabled 没落库（库默认值是 true）: %v", d["hookEnabled"])
	}
	if d["allowEnvOverride"] != false {
		t.Errorf("allowEnvOverride 没落库（库默认值是 true）: %v", d["allowEnvOverride"])
	}
	cmd := d["commands"].([]any)[0].(map[string]any)
	if cmd["enabled"] != false {
		t.Errorf("命令的 enabled 没落库（库默认值是 true）: %v", cmd["enabled"])
	}

	// 单独新增一条禁用命令：同样要落成 false
	_, out = e.call(t, "POST", "/api/v1/projects/flag-check/commands", e.token, map[string]any{
		"name": "off", "script": "echo off", "enabled": false,
	})
	if dataMap(t, out)["enabled"] != false {
		t.Errorf("新增禁用命令没落库: %v", dataMap(t, out)["enabled"])
	}

	// 禁用的命令不能被执行：两条都禁用 → 一次运行里没有任何命令步骤
	code, out := e.call(t, "POST", "/api/v1/projects/flag-check/run", e.token, nil)
	if code != 200 {
		t.Fatalf("触发失败: %d %v", code, out)
	}
	run := e.waitRun(t, int(dataMap(t, out)["runId"].(float64)), 60*time.Second)
	if run["status"] != "success" {
		t.Fatalf("运行应成功，实际 %v", run["status"])
	}
	if steps, _ := run["steps"].([]any); len(steps) != 0 {
		t.Fatalf("被禁用的命令不应产生步骤: %v", steps)
	}
	_, out = e.call(t, "GET", fmt.Sprintf("/api/v1/runs/%v/log", run["id"]), e.token, nil)
	if text, _ := dataMap(t, out)["data"].(string); !strings.Contains(text, "no enabled commands") {
		t.Errorf("日志应说明没有启用的命令:\n%s", text)
	}
}

// 命令编辑必须真的写进库。
//
// 回归背景：更新命令用的是 hand-written 的列名 map，其中 workdirOverride 写成了
// `workdir_override`，而模型 WorkdirOver 的实际列名是 `workdir_over`。
// GORM 的 UPDATE 因此整条失败，但错误被丢弃 —— 前端看到「保存成功」，
// 脚本却一个字没变（运行仍然执行旧脚本）。这里把「编辑 → 落库 → 运行」整条链路钉住。
func TestUpdateProjectCommandTakesEffect(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	proj := createProject(t, e, map[string]any{
		"key": "cmd-edit", "name": "命令编辑",
		"workdir":  "${WORKSPACE_ROOT}/${PROJECT_KEY}",
		"commands": []map[string]any{{"name": "build", "script": "echo v1", "enabled": true, "failStrategy": "abort"}},
	})
	cmdID := int(proj["commands"].([]any)[0].(map[string]any)["id"].(float64))

	putProject := func(cmds []map[string]any) map[string]any {
		t.Helper()
		code, out := e.call(t, "PUT", "/api/v1/projects/cmd-edit", e.token, map[string]any{
			"key": "cmd-edit", "name": "命令编辑",
			"workdir":  "${WORKSPACE_ROOT}/${PROJECT_KEY}",
			"commands": cmds,
		})
		if code != 200 || out["success"] != true {
			t.Fatalf("更新项目失败: %d %v", code, out)
		}
		return dataMap(t, out)
	}
	firstCommand := func(d map[string]any) map[string]any {
		t.Helper()
		list, _ := d["commands"].([]any)
		if len(list) != 1 {
			t.Fatalf("命令数量应为 1，实际 %d: %v", len(list), d["commands"])
		}
		return list[0].(map[string]any)
	}

	// 1) 改脚本并把三个覆盖项都填上
	d := putProject([]map[string]any{{
		"id": cmdID, "name": "build-v2", "script": "echo v2", "sortOrder": 1,
		"enabled": true, "failStrategy": "continue",
		"workdirOverride": "/tmp/override", "timeoutSec": 7, "shellOverride": "bash",
	}})
	got := firstCommand(d)
	if got["script"] != "echo v2" {
		t.Errorf("脚本没更新: %v", got["script"])
	}
	if got["name"] != "build-v2" {
		t.Errorf("名称没更新: %v", got["name"])
	}
	if got["failStrategy"] != "continue" {
		t.Errorf("失败策略没更新: %v", got["failStrategy"])
	}
	if got["workdirOverride"] != "/tmp/override" {
		t.Errorf("工作目录覆盖没更新: %v (列名 workdir_over 写错时会是这样)", got["workdirOverride"])
	}
	if got["timeoutSec"] != float64(7) {
		t.Errorf("超时没更新: %v", got["timeoutSec"])
	}
	if got["shellOverride"] != "bash" {
		t.Errorf("shell 覆盖没更新: %v", got["shellOverride"])
	}

	// 2) 覆盖项要能被清空（这正是用 map 而不是结构体更新的理由）
	d = putProject([]map[string]any{{
		"id": cmdID, "name": "build-v2", "script": "echo v2", "sortOrder": 1,
		"enabled": true, "failStrategy": "continue",
		"workdirOverride": "", "timeoutSec": 0, "shellOverride": "",
	}})
	got = firstCommand(d)
	if got["workdirOverride"] != "" || got["timeoutSec"] != float64(0) || got["shellOverride"] != "" {
		t.Errorf("覆盖项没被清空: %v", got)
	}

	// 3) 重新执行必须跑新脚本（旧 bug 下这里跑的还是 echo v1）
	code, out := e.call(t, "POST", "/api/v1/projects/cmd-edit/run", e.token, nil)
	if code != 200 {
		t.Fatalf("触发失败: %d %v", code, out)
	}
	runID := int(dataMap(t, out)["runId"].(float64))
	run := e.waitRun(t, runID, 60*time.Second)
	if run["status"] != "success" {
		t.Fatalf("运行应成功，实际 %v（%v）", run["status"], run["errorMsg"])
	}
	steps, _ := run["steps"].([]any)
	if len(steps) == 0 {
		t.Fatalf("运行没有步骤记录: %v", run)
	}
	if script, _ := steps[0].(map[string]any)["script"].(string); script != "echo v2" {
		t.Errorf("运行执行的脚本 = %q, want %q（命令更新没生效）", script, "echo v2")
	}

	// 4) 单条命令接口 PUT /commands/{id} 走的是另一段代码，也要生效
	code, out = e.call(t, "PUT", fmt.Sprintf("/api/v1/commands/%d", cmdID), e.token, map[string]any{
		"name": "build-v3", "script": "echo v3", "failStrategy": "abort",
		"workdirOverride": "/srv/tmp",
	})
	if code != 200 || out["success"] != true {
		t.Fatalf("更新单条命令失败: %d %v", code, out)
	}
	_, out = e.call(t, "GET", "/api/v1/projects/cmd-edit", e.token, nil)
	got = firstCommand(dataMap(t, out))
	if got["script"] != "echo v3" {
		t.Errorf("单条命令接口没生效: script=%v", got["script"])
	}
	if got["workdirOverride"] != "/srv/tmp" {
		t.Errorf("单条命令接口的工作目录覆盖没生效: %v", got["workdirOverride"])
	}
}

// 命令的增删也要真的落库：删掉一条、新增一条、调换顺序。
func TestUpdateProjectCommandAddRemoveReorder(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	proj := createProject(t, e, map[string]any{
		"key": "cmd-list", "name": "命令增删",
		"workdir": "${WORKSPACE_ROOT}/${PROJECT_KEY}",
		"commands": []map[string]any{
			{"name": "one", "script": "echo one", "enabled": true, "failStrategy": "abort"},
			{"name": "two", "script": "echo two", "enabled": true, "failStrategy": "abort"},
		},
	})
	list := proj["commands"].([]any)
	id1 := int(list[0].(map[string]any)["id"].(float64))
	id2 := int(list[1].(map[string]any)["id"].(float64))

	// 删掉第二条、把第一条改名并跟上一条新的，且顺序反过来
	code, out := e.call(t, "PUT", "/api/v1/projects/cmd-list", e.token, map[string]any{
		"key": "cmd-list", "name": "命令增删",
		"workdir": "${WORKSPACE_ROOT}/${PROJECT_KEY}",
		"commands": []map[string]any{
			{"name": "new-first", "script": "echo new", "sortOrder": 1, "enabled": true, "failStrategy": "abort"},
			{"id": id1, "name": "one-renamed", "script": "echo one", "sortOrder": 2, "enabled": true, "failStrategy": "abort"},
		},
	})
	if code != 200 || out["success"] != true {
		t.Fatalf("更新失败: %d %v", code, out)
	}
	d := dataMap(t, out)
	cmds, _ := d["commands"].([]any)
	if len(cmds) != 2 {
		t.Fatalf("应剩 2 条命令: %v", d["commands"])
	}
	first := cmds[0].(map[string]any)
	second := cmds[1].(map[string]any)
	if first["name"] != "new-first" || first["sortOrder"] != float64(1) {
		t.Errorf("新命令没有排在最前: %v", first)
	}
	if second["name"] != "one-renamed" || int(second["id"].(float64)) != id1 {
		t.Errorf("原有命令没被保留/更新: %v", second)
	}
	// 已经删掉的那条不应再存在
	for _, c := range cmds {
		if int(c.(map[string]any)["id"].(float64)) == id2 {
			t.Errorf("被删掉的命令仍然存在: %v", c)
		}
	}
}
