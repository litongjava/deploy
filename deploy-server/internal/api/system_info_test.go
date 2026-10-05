package api

import (
	"testing"
)

// 服务器信息接口的最小契约测试。
//
// 抓的重点是「字段名和类型不能悄悄变」：这个页面的数据结构是给
// 前端 services/types.ts 里的 ServerInfo 对齐的，一旦改名前端会静默变成空白，
// 而不是报错——所以用测试把契约钉住。
func TestSystemInfoEndpoint(t *testing.T) {
	e := newTestEnv(t)
	e.mustBootstrap(t)

	// 未登录必须 401：这个接口暴露的是宿主机资源情况，不能匿名可读。
	if code, _ := e.call(t, "GET", "/api/v1/system/info", "", nil); code != 401 {
		t.Fatalf("未登录访问 /system/info 应返回 401，得到 %d", code)
	}

	code, out := e.call(t, "GET", "/api/v1/system/info", e.token, nil)
	if code != 200 {
		t.Fatalf("system/info 失败: %d %v", code, out)
	}
	if out["success"] != true {
		t.Fatalf("响应 success 应为 true: %v", out)
	}
	data := dataMap(t, out)

	// ---- 基本信息 ----
	if data["hostname"] == nil || data["os"] == nil || data["arch"] == nil {
		t.Fatalf("缺少基本信息字段: %v", data)
	}
	if _, ok := data["uptimeSec"].(float64); !ok {
		t.Fatalf("uptimeSec 应为数字: %v", data["uptimeSec"])
	}

	// ---- CPU ----
	cpu, ok := data["cpu"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 cpu 对象: %v", data["cpu"])
	}
	cores, _ := cpu["cores"].(float64)
	if cores < 1 {
		t.Fatalf("cpu.cores 至少应为 1，得到 %v", cpu["cores"])
	}
	usage, ok := cpu["usagePercent"].(float64)
	if !ok {
		t.Fatalf("cpu.usagePercent 应为数字: %v", cpu["usagePercent"])
	}
	if usage < 0 || usage > 100 {
		t.Fatalf("cpu.usagePercent 超出 0~100: %v", usage)
	}

	// ---- 每个逻辑核心的使用率（页面上那排小进度条的数据源）----
	// 这条断言的意义在于「字段真的在响应里」：契约里 cpu.coreUsage 是数组，
	// 前端按它渲染，一旦改名或变成 null，页面会静默少掉一整块。
	// 注意变量别叫 cores：上面已经用 cores 接过 cpu["cores"]（float64）。
	coreUsage, ok := cpu["coreUsage"].([]any)
	if !ok {
		t.Fatalf("cpu.coreUsage 应为数组（空数组也不能是 null）: %v", cpu["coreUsage"])
	}
	if len(coreUsage) > 0 {
		if len(coreUsage) != int(cpu["cores"].(float64)) {
			t.Fatalf("cpu.coreUsage 有 %d 项，与 cpu.cores 不一致", len(coreUsage))
		}
		var first map[string]any
		for i, raw := range coreUsage {
			c, ok := raw.(map[string]any)
			if !ok {
				t.Fatalf("cpu.coreUsage[%d] 不是对象: %v", i, raw)
			}
			p, _ := c["usagePercent"].(float64)
			if p < 0 || p > 100 {
				t.Fatalf("cpu.coreUsage[%d].usagePercent 超出 0~100: %v", i, p)
			}
			if i == 0 {
				first = c
			}
		}
		if first["index"] != float64(0) {
			t.Fatalf("cpu.coreUsage[0].index 应为 0: %v", first["index"])
		}
		if first["indexLabel"] != "CPU0" {
			t.Fatalf("cpu.coreUsage[0].indexLabel 应为 CPU0: %v", first["indexLabel"])
		}
	}

	// ---- 内存 ----
	mem, ok := data["memory"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 memory 对象: %v", data["memory"])
	}
	total, _ := mem["totalBytes"].(float64)
	used, _ := mem["usedBytes"].(float64)
	avail, _ := mem["availableBytes"].(float64)
	if total <= 0 {
		t.Fatalf("memory.totalBytes 应大于 0，得到 %v", mem["totalBytes"])
	}
	if used < 0 || used > total {
		t.Fatalf("memory.usedBytes 不合理: used=%v total=%v", used, total)
	}
	// available + used 应当约等于 total；留 1% 余量吸收取样瞬间的抖动。
	if diff := total - (used + avail); diff > total*0.01 || diff < -total*0.01 {
		t.Fatalf("memory 各项不自洽: total=%v used=%v available=%v", total, used, avail)
	}
	memUsage, _ := mem["usagePercent"].(float64)
	if memUsage < 0 || memUsage > 100 {
		t.Fatalf("memory.usagePercent 超出 0~100: %v", memUsage)
	}

	// ---- 磁盘 ----
	disks, ok := data["disks"].([]any)
	if !ok {
		t.Fatalf("disks 应为数组（空数组也不行是 null）: %v", data["disks"])
	}
	for i, raw := range disks {
		d, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("disks[%d] 不是对象: %v", i, raw)
		}
		dt, _ := d["totalBytes"].(float64)
		du, _ := d["usedBytes"].(float64)
		dp, _ := d["usagePercent"].(float64)
		if d["path"] == nil || d["path"] == "" {
			t.Fatalf("disks[%d] 缺少 path: %v", i, d)
		}
		if dt <= 0 {
			t.Fatalf("disks[%d].totalBytes 应大于 0: %v", i, d)
		}
		if du < 0 || du > dt {
			t.Fatalf("disks[%d].usedBytes 不合理: %v", i, d)
		}
		if dp < 0 || dp > 100 {
			t.Fatalf("disks[%d].usagePercent 超出 0~100: %v", i, dp)
		}
		if s, _ := d["status"].(string); s != "ok" && s != "warn" {
			t.Fatalf("disks[%d].status 取值非法: %v", i, d["status"])
		}
	}

	// ---- 不泄漏配置细节 ----
	// doctor 接口会返回 webRoot/dataDir，这个面向所有登录用户的页面不能带上它们。
	for _, key := range []string{"webRoot", "dataDir", "serverUrl"} {
		if _, exists := data[key]; exists {
			t.Fatalf("system/info 不应暴露 %s", key)
		}
	}
}
