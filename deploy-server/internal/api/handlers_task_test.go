package api

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"deploy-server/internal/store"
)

// 任务的端到端链路：建任务 → 执行 → 看日志 → Hook 触发 → 归档拦截 → 删除。
//
// 它同时验证任务与项目共用的那部分语义（Run / 日志 / Hook）在任务这一侧也成立，
// 以及任务独有的约束：没有依赖、没有 Git、步骤是内联的 JSON。

func createDemoTask(t *testing.T, e *testEnv) map[string]any {
	t.Helper()
	code, out := e.call(t, "POST", "/api/v1/tasks", e.token, map[string]any{
		"key": "bench-demo", "name": "压测任务",
		"workdir":     "${WORKSPACE_ROOT}/${TASK_KEY}",
		"description": "演示任务",
		"concurrency": "queue",
		"steps": []map[string]any{
			{"name": "打印环境", "script": "echo task-key=" + envRef("TASK_KEY"), "enabled": true},
			{"name": "第二步", "script": "echo step-two", "enabled": true},
		},
		"envs": []map[string]any{
			{"key": "TARGET", "value": "http://127.0.0.1:8080", "isSecret": false},
			{"key": "TOKEN", "value": "s3cr3t-value", "isSecret": true},
		},
	})
	if code != 200 {
		t.Fatalf("创建任务失败: %d %v", code, out)
	}
	return dataMap(t, out)
}

func TestTaskLifecycle(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	task := createDemoTask(t, e)
	if task["key"] != "bench-demo" {
		t.Fatalf("任务 key 不对: %v", task)
	}
	steps, _ := task["steps"].([]any)
	if len(steps) != 2 {
		t.Fatalf("步骤没有保存: %v", task["steps"])
	}
	// Secret 的值永远不出现在出参里。
	for _, raw := range envsOfTask(task) {
		if raw["key"] != "TOKEN" {
			continue
		}
		if raw["value"] != "" {
			t.Fatalf("Secret 不应回明文: %v", raw["value"])
		}
		if raw["isSecret"] != true {
			t.Fatalf("Secret 标记丢了: %v", raw)
		}
	}

	// key 重复必须冲突
	code, out := e.call(t, "POST", "/api/v1/tasks", e.token, map[string]any{
		"key": "bench-demo", "name": "重复", "workdir": "/tmp/x",
		"steps": []map[string]any{{"name": "a", "script": "echo a"}},
	})
	if code != 200 || out["success"] != false {
		t.Fatalf("重复 key 应失败: %d %v", code, out)
	}

	// 执行
	code, out = e.call(t, "POST", "/api/v1/tasks/bench-demo/run", e.token, map[string]any{})
	if code != 200 {
		t.Fatalf("触发任务失败: %d %v", code, out)
	}
	trigger := dataMap(t, out)
	if trigger["targetKind"] != "task" {
		t.Fatalf("触发结果应标出执行对象是任务: %v", trigger)
	}
	runID := int(trigger["runId"].(float64))

	final := e.waitRun(t, runID, 20*time.Second)
	if final["status"] != "success" {
		t.Fatalf("任务应执行成功: %v", final)
	}
	if final["targetKind"] != "task" || final["taskId"] == float64(0) {
		t.Fatalf("Run 没有记录任务身份: %v", final)
	}
	if final["targetKey"] != "bench-demo" {
		t.Fatalf("targetKey 应是任务 key: %v", final["targetKey"])
	}

	// 日志：写在 tasks/<key>/ 下，TASK_KEY 内置变量已注入，Secret 已打码
	code, out = e.call(t, "GET", fmt.Sprintf("/api/v1/runs/%d/log", runID), e.token, nil)
	if code != 200 {
		t.Fatalf("读日志失败: %d", code)
	}
	logText, _ := dataMap(t, out)["data"].(string)
	if !strings.Contains(logText, "=== Task Run #1 ===") {
		t.Fatalf("日志头不对:\n%s", logText)
	}
	if !strings.Contains(logText, "task-key=bench-demo") {
		t.Fatalf("TASK_KEY 没有注入:\n%s", logText)
	}
	if strings.Contains(logText, "s3cr3t-value") {
		t.Fatalf("Secret 明文漏进日志:\n%s", logText)
	}

	// Hook 触发：任务也可以
	code, out = e.call(t, "POST", "/api/v1/tasks/bench-demo/hooks", e.token, map[string]string{"name": "ci"})
	if code != 200 {
		t.Fatalf("建 Hook 失败: %d %v", code, out)
	}
	plainToken, _ := dataMap(t, out)["token"].(string)
	if plainToken == "" {
		t.Fatal("Hook 没有返回明文令牌")
	}
	code, out = e.callWithHeaders(t, "POST", "/api/v1/hooks/"+plainToken, map[string]string{
		"X-Deploy-Client": "test",
	}, map[string]any{"env": map[string]string{"EXTRA": "1"}})
	if code != 202 {
		t.Fatalf("Hook 触发任务应返回 202: %d %v", code, out)
	}
	hookRunID := int(dataMap(t, out)["runId"].(float64))
	hookFinal := e.waitRun(t, hookRunID, 20*time.Second)
	if hookFinal["status"] != "success" {
		t.Fatalf("Hook 触发的任务应成功: %v", hookFinal)
	}
	if hookFinal["triggerType"] != "hook" || hookFinal["targetKey"] != "bench-demo" {
		t.Fatalf("Hook 运行的触发方式/目标不对: %v", hookFinal)
	}

	// 归档后不能再执行
	code, out = e.call(t, "POST", "/api/v1/tasks/bench-demo/archive", e.token, map[string]any{"archived": true})
	if code != 200 || out["success"] != true {
		t.Fatalf("归档失败: %d %v", code, out)
	}
	code, out = e.call(t, "POST", "/api/v1/tasks/bench-demo/run", e.token, map[string]any{})
	if out["success"] != false {
		t.Fatalf("归档任务不应能执行: %d %v", code, out)
	}

	// 取消归档 → 删除（连带运行历史）
	e.call(t, "POST", "/api/v1/tasks/bench-demo/archive", e.token, map[string]any{"archived": false})
	code, out = e.call(t, "DELETE", "/api/v1/tasks/bench-demo", e.token, nil)
	if code != 200 || out["success"] != true {
		t.Fatalf("删除任务失败: %d %v", code, out)
	}
	var cnt int64
	e.st.DB().Model(&store.Run{}).Where("task_id <> 0").Count(&cnt)
	if cnt != 0 {
		t.Fatalf("删除任务应连带删除它的运行记录，剩余 %d", cnt)
	}
}

// TestTaskRejectsUnsupportedOptions 项目独有的参数用在任务上必须被明确拒绝，
// 而不是被静默忽略 —— 静默忽略会让人以为「含依赖执行」真的生效了。
func TestTaskRejectsUnsupportedOptions(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)
	createDemoTask(t, e)

	code, out := e.call(t, "POST", "/api/v1/tasks/bench-demo/run", e.token, map[string]any{
		"withDependencies": true,
	})
	if code != 200 || out["success"] != false {
		t.Fatalf("任务不支持 withDependencies，应报错: %d %v", code, out)
	}

	// 指定不存在的步骤同样要报错
	code, out = e.call(t, "POST", "/api/v1/tasks/bench-demo/run", e.token, map[string]any{
		"commandId": 99,
	})
	if code != 200 || out["success"] != false {
		t.Fatalf("不存在的步骤应报错: %d %v", code, out)
	}

	// 只跑某一步：1 基的步骤序号
	code, out = e.call(t, "POST", "/api/v1/tasks/bench-demo/run", e.token, map[string]any{
		"commandId": 2,
	})
	if code != 200 {
		t.Fatalf("按步骤执行失败: %d %v", code, out)
	}
	runID := int(dataMap(t, out)["runId"].(float64))
	final := e.waitRun(t, runID, 20*time.Second)
	if final["status"] != "success" {
		t.Fatalf("按步骤执行应成功: %v", final)
	}
	steps, _ := final["steps"].([]any)
	if len(steps) != 1 {
		t.Fatalf("只应执行一步，实际 %d 步", len(steps))
	}
	if name, _ := steps[0].(map[string]any)["name"].(string); name != "第二步" {
		t.Fatalf("执行的不是指定步骤: %v", name)
	}
}

// envsOfTask 把任务出参里的 envs 数组转成 map 列表。
func envsOfTask(task map[string]any) []map[string]any {
	raw, _ := task["envs"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
