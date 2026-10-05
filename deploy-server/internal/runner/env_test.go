package runner

import (
	"strings"
	"testing"

	"deploy-server/internal/secret"
	"deploy-server/internal/store"
)

// TestBuildEnvPriority 验证环境变量覆盖顺序：
// 普通 < Secret < 内置 < 触发参数。
func TestBuildEnvPriority(t *testing.T) {
	box, err := secret.New("test-master-key")
	if err != nil {
		t.Fatal(err)
	}
	encShared, err := box.Encrypt("from-secret")
	if err != nil {
		t.Fatal(err)
	}
	encHidden, err := box.Encrypt("hidden-token")

	envs := []store.ProjectEnv{
		{Key: "SHARED", Value: "from-plain"},
		{Key: "SHARED_SECRET", Value: "", IsSecret: true, SecretEnc: encShared},
		{Key: "HIDDEN", Value: "", IsSecret: true, SecretEnc: encHidden},
		{Key: "BUILTIN", Value: "from-project"},
	}

	env, secrets, err := BuildEnv(EnvPlan{
		EnvVars:     EnvVarsFromProject(envs),
		Builtins:    map[string]string{"BUILTIN": "from-builtin", "BUILD_NUMBER": "42"},
		Params:      map[string]string{"BUILTIN": "from-param", "EXTRA": "1"},
		AllowParams: true,
		Box:         box,
	})
	if err != nil {
		t.Fatal(err)
	}

	got := indexEnv(env)
	if got["SHARED"] != "from-plain" {
		t.Errorf("普通变量错: %q", got["SHARED"])
	}
	if got["SHARED_SECRET"] != "from-secret" {
		t.Errorf("Secret 变量应解密注入: %q", got["SHARED_SECRET"])
	}
	if got["BUILD_NUMBER"] != "42" {
		t.Errorf("内置变量错: %q", got["BUILD_NUMBER"])
	}
	// 触发参数优先级最高。
	if got["BUILTIN"] != "from-param" {
		t.Errorf("触发参数应覆盖内置: %q", got["BUILTIN"])
	}
	if got["EXTRA"] != "1" {
		t.Errorf("额外参数没注入: %q", got["EXTRA"])
	}

	// 打码清单必须包含两个 Secret 的明文。
	joined := strings.Join(secrets, "|")
	if !strings.Contains(joined, "from-secret") || !strings.Contains(joined, "hidden-token") {
		t.Errorf("打码清单不完整: %v", secrets)
	}
}

// TestBuildEnvRespectsAllowOverride 不允许覆盖时，触发参数必须被忽略。
func TestBuildEnvRespectsAllowOverride(t *testing.T) {
	env, _, err := BuildEnv(EnvPlan{
		Builtins:    map[string]string{"PROJECT_KEY": "demo"},
		Params:      map[string]string{"PROJECT_KEY": "hacked"},
		AllowParams: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := indexEnv(env)["PROJECT_KEY"]; got != "demo" {
		t.Fatalf("关闭覆盖后参数仍生效: %q", got)
	}
}

// TestExpandWorkdir 占位符替换。任务与项目共用同一套占位符，所以 TASK_* 也要认。
func TestExpandWorkdir(t *testing.T) {
	cases := map[string]string{
		"${WORKSPACE_ROOT}/${PROJECT_KEY}": "/srv/ws/my-app",
		"/opt/$PROJECT_KEY":                "/opt/my-app",
		"":                                 "/srv/ws/my-app",
		"/data/${PROJECT_NAME}":            "/data/我的应用",
		"/data/${TASK_KEY}":                "/data/my-app",
		"/data/$TASK_NAME":                 "/data/我的应用",
	}
	for in, want := range cases {
		if got := ExpandWorkdir(in, "/srv/ws", "my-app", "我的应用"); got != want {
			t.Errorf("ExpandWorkdir(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

// TestGateLimitsConcurrency 验证并发闸门真的限制住并发，且能动态放大。
func TestGateLimitsConcurrency(t *testing.T) {
	g := newGate(2)
	g.acquire()
	g.acquire()
	if g.inFlight() != 2 {
		t.Fatalf("inFlight=%d", g.inFlight())
	}

	done := make(chan struct{})
	go func() {
		g.acquire() // 应阻塞，直到我们放一个或调大上限
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("超过上限的 acquire 不应立即成功")
	default:
	}

	g.setLimit(3) // 调大后应立刻唤醒排队者
	<-done
	if g.inFlight() != 3 {
		t.Fatalf("调大上限后 inFlight=%d", g.inFlight())
	}
}

func indexEnv(env []string) map[string]string {
	out := map[string]string{}
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i > 0 {
			out[kv[:i]] = kv[i+1:]
		}
	}
	return out
}
