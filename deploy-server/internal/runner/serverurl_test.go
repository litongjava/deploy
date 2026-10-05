package runner

import (
	"testing"

	"deploy-server/internal/config"
	"deploy-server/internal/secret"
	"deploy-server/internal/store"
)

// DEPLOY_SERVER_URL 每次运行都重新取：系统设置里的值优先，其次启动配置里的默认值。
// 「重新取」正是「改了地址不用重启」的实现方式，所以这里连读两次来钉住它。
func TestServerURLPrefersRuntimeSetting(t *testing.T) {
	t.Setenv("DEPLOY_SERVER_URL", "")
	t.Setenv("DEPLOY_DATA_DIR", "")

	dir := t.TempDir()
	cfg, err := config.Load([]string{
		"-data-dir", dir + "/data",
		"-web-root", dir + "/workspaces",
		"-listen", "127.0.0.1:0",
		"-server-url", "http://startup-host:10055",
	})
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	box, err := secret.New(cfg.SecretKey)
	if err != nil {
		t.Fatalf("初始化加密失败: %v", err)
	}
	e := New(cfg, st, box)

	// 没设置过 → 启动默认值
	if got := e.serverURL(); got != "http://startup-host:10055" {
		t.Fatalf("没设置过时应使用启动默认值，得到 %q", got)
	}

	// 模拟管理员在设置页保存新地址：同一进程内立即生效
	if err := st.SetSetting("serverUrl", "http://runtime-host:10055"); err != nil {
		t.Fatalf("写入设置失败: %v", err)
	}
	if got := e.serverURL(); got != "http://runtime-host:10055" {
		t.Fatalf("设置里的值应优先于启动默认值，得到 %q", got)
	}

	// 清空 → 回退到启动默认值
	if err := st.SetSetting("serverUrl", ""); err != nil {
		t.Fatalf("清空设置失败: %v", err)
	}
	if got := e.serverURL(); got != "http://startup-host:10055" {
		t.Fatalf("清空后应回退到启动默认值，得到 %q", got)
	}
}
