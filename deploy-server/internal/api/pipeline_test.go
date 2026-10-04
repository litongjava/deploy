package api

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

// 依赖构建流水线的端到端用例：先建底层「构建项目」，再建依赖它的应用项目，
// 触发时带上 withDependencies，验证构建顺序、状态透传与失败中止。

func createProject(t *testing.T, e *testEnv, body map[string]any) map[string]any {
	t.Helper()
	code, out := e.call(t, "POST", "/api/v1/projects", e.token, body)
	if code != 200 || out["success"] != true {
		t.Fatalf("创建项目失败: %d %v", code, out)
	}
	return dataMap(t, out)
}

func TestDependencyPipelineBuildsDependenciesFirst(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	createProject(t, e, map[string]any{
		"key": "dep-lib", "name": "底层库", "kind": "lib",
		"workdir":  "${WORKSPACE_ROOT}/${PROJECT_KEY}",
		"commands": []map[string]any{{"name": "build-lib", "script": "echo built-dep-lib"}},
	})
	app := createProject(t, e, map[string]any{
		"key": "app-main", "name": "业务项目", "kind": "app",
		"workdir":      "${WORKSPACE_ROOT}/${PROJECT_KEY}",
		"dependencies": []string{"dep-lib"},
		"commands":     []map[string]any{{"name": "build-app", "script": "echo built-app-main"}},
	})
	if deps, _ := app["dependencies"].([]any); len(deps) != 1 || deps[0] != "dep-lib" {
		t.Fatalf("依赖没有保存: %v", app["dependencies"])
	}
	// 详情接口要给出构建计划：ok + 顺序
	_, out := e.call(t, "GET", "/api/v1/projects/app-main", e.token, nil)
	plan, _ := dataMap(t, out)["dependencyPlan"].(map[string]any)
	if plan == nil || plan["ok"] != true {
		t.Fatalf("依赖计划不可用: %v", plan)
	}
	if order, _ := plan["order"].([]any); len(order) != 1 {
		t.Fatalf("依赖计划顺序不对: %v", plan)
	}

	// 含依赖触发
	code, out := e.call(t, "POST", "/api/v1/projects/app-main/run", e.token, map[string]any{
		"withDependencies": true,
	})
	if code != 200 {
		t.Fatalf("触发失败: %d %v", code, out)
	}
	data := dataMap(t, out)
	mainRunID := int(data["runId"].(float64))
	deps, _ := data["dependencies"].([]any)
	if len(deps) != 1 {
		t.Fatalf("应返回 1 个依赖 Run: %v", data)
	}
	depInfo := deps[0].(map[string]any)
	depRunID := int(depInfo["runId"].(float64))
	if depInfo["key"] != "dep-lib" {
		t.Fatalf("依赖 Run 的项目不对: %v", depInfo)
	}
	if pid, _ := data["pipelineId"].(string); pid == "" {
		t.Fatalf("应返回 pipelineId: %v", data)
	}

	dep := e.waitRun(t, depRunID, 90*time.Second)
	if dep["status"] != "success" {
		t.Fatalf("依赖构建应成功，实际 %v（%v）", dep["status"], dep["errorMsg"])
	}
	main := e.waitRun(t, mainRunID, 120*time.Second)
	if main["status"] != "success" {
		t.Fatalf("主项目应成功，实际 %v（%v）", main["status"], main["errorMsg"])
	}

	// 顺序：主项目开始时间不早于依赖结束时间
	depEnd, _ := time.Parse(time.RFC3339Nano, dep["finishedAt"].(string))
	mainStart, _ := time.Parse(time.RFC3339Nano, main["startedAt"].(string))
	if depEnd.IsZero() || mainStart.IsZero() {
		t.Fatalf("时间字段缺失: dep=%v main=%v", dep["finishedAt"], main["startedAt"])
	}
	if mainStart.Before(depEnd) {
		t.Fatalf("主项目在依赖完成前就开始了: dep 结束 %v, main 开始 %v", depEnd, mainStart)
	}

	// 运行详情里应带上整条流水线
	_, out = e.call(t, "GET", fmt.Sprintf("/api/v1/runs/%d", mainRunID), e.token, nil)
	pl, _ := dataMap(t, out)["pipeline"].([]any)
	if len(pl) != 2 {
		t.Fatalf("流水线应有 2 条 Run: %v", pl)
	}
	roles := map[string]bool{}
	for _, item := range pl {
		m := item.(map[string]any)
		roles[m["pipelineRole"].(string)] = true
	}
	if !roles["dependency"] || !roles["main"] {
		t.Fatalf("流水线角色不对: %v", pl)
	}

	// 依赖构建项目的构建号与主项目各自独立
	if dep["buildNumber"].(float64) != 1 || main["buildNumber"].(float64) != 1 {
		t.Fatalf("构建号不对: dep=%v main=%v", dep["buildNumber"], main["buildNumber"])
	}
}

func TestDependencyFailureAbortsMainProject(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	createProject(t, e, map[string]any{
		"key": "broken-lib", "name": "会失败的底层库", "kind": "lib",
		"workdir":  "${WORKSPACE_ROOT}/${PROJECT_KEY}",
		"commands": []map[string]any{{"name": "fail", "script": "echo will-fail && exit 3"}},
	})
	createProject(t, e, map[string]any{
		"key": "app-2", "name": "业务项目 2", "kind": "app",
		"workdir":      "${WORKSPACE_ROOT}/${PROJECT_KEY}",
		"dependencies": []string{"broken-lib"},
		"commands":     []map[string]any{{"name": "build", "script": "echo should-not-run"}},
	})

	code, out := e.call(t, "POST", "/api/v1/projects/app-2/run", e.token, map[string]any{
		"withDependencies": true,
	})
	if code != 200 {
		t.Fatalf("触发失败: %d %v", code, out)
	}
	data := dataMap(t, out)
	mainRunID := int(data["runId"].(float64))
	depRunID := int(data["dependencies"].([]any)[0].(map[string]any)["runId"].(float64))

	dep := e.waitRun(t, depRunID, 90*time.Second)
	if dep["status"] != "failed" {
		t.Fatalf("依赖应失败，实际 %v", dep["status"])
	}
	main := e.waitRun(t, mainRunID, 120*time.Second)
	if main["status"] != "failed" {
		t.Fatalf("主项目应记 failed，实际 %v", main["status"])
	}
	if msg, _ := main["errorMsg"].(string); !strings.Contains(msg, "dependency build broken-lib failed") {
		t.Fatalf("错误信息没说清是哪个依赖失败: %q", msg)
	}
	// 主项目的日志要写明「没有执行」，否则用户以为它跑过了
	_, out = e.call(t, "GET", fmt.Sprintf("/api/v1/runs/%d/log", mainRunID), e.token, nil)
	logText, _ := dataMap(t, out)["data"].(string)
	if !strings.Contains(logText, "was not executed") {
		t.Fatalf("日志没有说明未执行:\n%s", logText)
	}
	if strings.Contains(logText, "should-not-run") {
		t.Fatalf("依赖失败后主项目仍然执行了:\n%s", logText)
	}
}

// assertBadRequest 断言「业务失败」信封。
// 注意：契约里参数错误是 HTTP 200 + success=false + errorCode 40000（见 response.go），
// 不能按 HTTP 状态码判断。
func assertBadRequest(t *testing.T, out map[string]any, errHas string) {
	t.Helper()
	if out["success"] != false {
		t.Fatalf("应为业务失败: %v", out)
	}
	if code, _ := out["errorCode"].(float64); code != 40000 {
		t.Fatalf("错误码应为 40000（参数错误），得到 %v", out["errorCode"])
	}
	if msg, _ := out["errorMessage"].(string); !strings.Contains(msg, errHas) {
		t.Fatalf("错误信息 %q 不含 %q", msg, errHas)
	}
}

// 项目列表的 archived 三态过滤：不传/false=未归档，true=已归档，all=全部。
func TestProjectListArchivedFilter(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	createProject(t, e, map[string]any{"key": "keep-me", "name": "keep", "workdir": "/tmp/keep-me"})
	createProject(t, e, map[string]any{"key": "gone-me", "name": "gone", "workdir": "/tmp/gone-me"})
	e.call(t, "POST", "/api/v1/projects/gone-me/archive", e.token, map[string]any{"archived": true})

	keys := func(query string) []string {
		t.Helper()
		_, out := e.call(t, "GET", "/api/v1/projects?page=1&pageSize=50"+query, e.token, nil)
		list, _ := dataMap(t, out)["list"].([]any)
		got := make([]string, 0, len(list))
		for _, item := range list {
			got = append(got, item.(map[string]any)["key"].(string))
		}
		sort.Strings(got)
		return got
	}

	if got := keys(""); strings.Join(got, ",") != "keep-me" {
		t.Fatalf("默认应只返回未归档: %v", got)
	}
	if got := keys("&archived=true"); strings.Join(got, ",") != "gone-me" {
		t.Fatalf("archived=true 应只返回已归档: %v", got)
	}
	if got := keys("&archived=all"); strings.Join(got, ",") != "gone-me,keep-me" {
		t.Fatalf("archived=all 应返回全部: %v", got)
	}
}

func TestDependencyValidation(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	createProject(t, e, map[string]any{
		"key": "app-only", "name": "应用项目", "kind": "app",
		"workdir": "${WORKSPACE_ROOT}/${PROJECT_KEY}",
	})
	createProject(t, e, map[string]any{
		"key": "lib-a", "name": "构建项目 A", "kind": "lib",
		"workdir": "${WORKSPACE_ROOT}/${PROJECT_KEY}",
	})
	createProject(t, e, map[string]any{
		"key": "lib-b", "name": "构建项目 B", "kind": "lib",
		"workdir":      "${WORKSPACE_ROOT}/${PROJECT_KEY}",
		"dependencies": []string{"lib-a"},
	})

	cases := []struct {
		name   string
		body   map[string]any
		errHas string
	}{
		{
			name: "依赖必须是构建项目",
			body: map[string]any{
				"key": "bad-1", "name": "bad", "kind": "app", "workdir": "/tmp/bad-1",
				"dependencies": []string{"app-only"},
			},
			errHas: "only build projects",
		},
		{
			name: "依赖必须存在",
			body: map[string]any{
				"key": "bad-2", "name": "bad", "kind": "app", "workdir": "/tmp/bad-2",
				"dependencies": []string{"ghost"},
			},
			errHas: "does not exist",
		},
		{
			name: "不能依赖自己",
			body: map[string]any{
				"key": "bad-3", "name": "bad", "kind": "app", "workdir": "/tmp/bad-3",
				"dependencies": []string{"bad-3"},
			},
			errHas: "cannot depend on itself",
		},
		{
			name: "类型取值必须是 app / lib",
			body: map[string]any{
				"key": "bad-4", "name": "bad", "kind": "library", "workdir": "/tmp/bad-4",
			},
			errHas: "project kind must be",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, out := e.call(t, "POST", "/api/v1/projects", e.token, c.body)
			assertBadRequest(t, out, c.errHas)
		})
	}

	// 保存时报环：让 lib-a 依赖 lib-b（已有 lib-b → lib-a）
	_, out := e.call(t, "PUT", "/api/v1/projects/lib-a", e.token, map[string]any{
		"key": "lib-a", "name": "构建项目 A", "kind": "lib",
		"workdir": "${WORKSPACE_ROOT}/lib-a", "dependencies": []string{"lib-b"},
	})
	assertBadRequest(t, out, "cycle")

	// 归档依赖后触发要报错，而不是静默跳过
	e.call(t, "POST", "/api/v1/projects/lib-a/archive", e.token, map[string]any{"archived": true})
	_, out = e.call(t, "POST", "/api/v1/projects/lib-b/run", e.token, map[string]any{
		"withDependencies": true,
	})
	assertBadRequest(t, out, "archived")
}

// 不带依赖触发时行为必须与以前完全一致（单 Run、无 pipeline）。
func TestRunWithoutDependenciesKeepsSingleRun(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	createProject(t, e, map[string]any{
		"key": "lib-x", "name": "构建项目 X", "kind": "lib",
		"workdir":  "${WORKSPACE_ROOT}/${PROJECT_KEY}",
		"commands": []map[string]any{{"name": "build", "script": "echo lib-x"}},
	})
	createProject(t, e, map[string]any{
		"key": "app-x", "name": "应用项目 X", "kind": "app",
		"workdir":      "${WORKSPACE_ROOT}/${PROJECT_KEY}",
		"dependencies": []string{"lib-x"},
		"commands":     []map[string]any{{"name": "build", "script": "echo app-x"}},
	})

	code, out := e.call(t, "POST", "/api/v1/projects/app-x/run", e.token, nil)
	if code != 200 {
		t.Fatalf("触发失败: %d %v", code, out)
	}
	data := dataMap(t, out)
	if deps, ok := data["dependencies"].([]any); ok && len(deps) > 0 {
		t.Fatalf("不带依赖触发不应创建依赖 Run: %v", data)
	}
	if pid, _ := data["pipelineId"].(string); pid != "" {
		t.Fatalf("不带依赖触发不应有 pipelineId: %v", data)
	}
	run := e.waitRun(t, int(data["runId"].(float64)), 90*time.Second)
	if run["status"] != "success" {
		t.Fatalf("应成功: %v", run["status"])
	}
}
