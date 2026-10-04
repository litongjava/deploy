package store

import (
	"path/filepath"
	"strings"
	"testing"
)

// newTestStore 起一个临时的 SQLite（顺带验证 AutoMigrate 能加上新列）。
func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func mkProject(t *testing.T, st *Store, key, kind string, deps ...string) *Project {
	t.Helper()
	p := Project{
		Key: key, Name: key, Kind: kind, Workdir: "/tmp/" + key,
		Shell: "auto", Concurrency: ConcurrencyQueue,
		Dependencies: EncodeDependencies(deps),
	}
	if err := st.DB().Create(&p).Error; err != nil {
		t.Fatalf("create project %s: %v", key, err)
	}
	return &p
}

func keysOf(list []Project) []string {
	out := make([]string, 0, len(list))
	for _, p := range list {
		out = append(out, p.Key)
	}
	return out
}

// 依赖在前、深度优先：d 依赖 b、c，b 依赖 a，c 依赖 a → a, b, c（a 只构建一次）。
func TestResolveDependenciesOrderAndDedup(t *testing.T) {
	st := newTestStore(t)
	mkProject(t, st, "a", KindLib)
	mkProject(t, st, "b", KindLib, "a")
	mkProject(t, st, "c", KindLib, "a")
	d := mkProject(t, st, "d", KindApp, "b", "c")

	got, err := st.ResolveDependencies(d)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := []string{"a", "b", "c"}
	if strings.Join(keysOf(got), ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", keysOf(got), want)
	}
}

// 多层依赖也是先叶子后上层。
func TestResolveDependenciesDeepChain(t *testing.T) {
	st := newTestStore(t)
	mkProject(t, st, "tio-boot", KindLib)
	mkProject(t, st, "java-openai", KindLib)
	mkProject(t, st, "tio-boot-admin", KindLib, "tio-boot", "java-openai")
	app := mkProject(t, st, "myprofessor-backend", KindApp, "tio-boot-admin")

	got, err := st.ResolveDependencies(app)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := "tio-boot,java-openai,tio-boot-admin"
	if strings.Join(keysOf(got), ",") != want {
		t.Fatalf("order = %v, want %s", keysOf(got), want)
	}
}

func TestResolveDependenciesRejects(t *testing.T) {
	st := newTestStore(t)
	mkProject(t, st, "lib1", KindLib)
	app := mkProject(t, st, "app1", KindApp, "lib1")
	archived := mkProject(t, st, "old-lib", KindLib)
	st.DB().Model(&Project{}).Where("id = ?", archived.ID).Update("archived", true)

	cases := []struct {
		name   string
		deps   []string
		errHas string
	}{
		{"missing", []string{"nope"}, "does not exist"},
		{"not a lib", []string{"app1"}, "not a build project"},
		{"archived", []string{"old-lib"}, "archived"},
		{"self", []string{"app1"}, "not a build project"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &Project{Key: "tmp-" + c.name, Name: "tmp", Kind: KindApp, Dependencies: EncodeDependencies(c.deps)}
			_, err := st.ResolveDependencies(p)
			if err == nil || !strings.Contains(err.Error(), c.errHas) {
				t.Fatalf("err = %v, want contains %q", err, c.errHas)
			}
		})
	}
	// 依赖自己（同一个 key）也要被拦住。
	app.Dependencies = EncodeDependencies([]string{"app1"})
	if _, err := st.ResolveDependencies(app); err == nil {
		t.Fatal("self dependency should fail")
	}
}

func TestResolveDependenciesCycle(t *testing.T) {
	st := newTestStore(t)
	a := mkProject(t, st, "a", KindLib, "b")
	mkProject(t, st, "b", KindLib, "a")

	if _, err := st.ResolveDependencies(a); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("err = %v, want a cycle error", err)
	}
}

func TestValidateDependencyTargets(t *testing.T) {
	st := newTestStore(t)
	mkProject(t, st, "lib1", KindLib)
	mkProject(t, st, "lib2", KindLib, "lib1")
	graph, err := st.DependencyGraph()
	if err != nil {
		t.Fatalf("graph: %v", err)
	}

	if msg := ValidateDependencyTargets(graph, "lib2", []string{"lib1"}); msg != "" {
		t.Fatalf("valid deps rejected: %s", msg)
	}
	if msg := ValidateDependencyTargets(graph, "lib2", []string{"lib2"}); msg == "" {
		t.Fatal("self dependency accepted")
	}
	if msg := ValidateDependencyTargets(graph, "lib2", []string{"ghost"}); !strings.Contains(msg, "does not exist") {
		t.Fatalf("missing dep: %s", msg)
	}
	// lib1 -> lib2 会与已有的 lib2 -> lib1 成环。
	if msg := ValidateDependencyTargets(graph, "lib1", []string{"lib2"}); !strings.Contains(msg, "cycle") {
		t.Fatalf("cycle not detected: %s", msg)
	}
	// 新建项目（图里还没有它）可以依赖已知项目，但要拦住自环。
	if msg := ValidateDependencyTargets(graph, "brand-new", []string{"lib2"}); msg != "" {
		t.Fatalf("new project deps rejected: %s", msg)
	}
	if msg := ValidateDependencyTargets(graph, "brand-new", []string{"brand-new"}); msg == "" {
		t.Fatal("new project depending on itself accepted")
	}
}

func TestNormalizeAndEncodeDependencies(t *testing.T) {
	keys := NormalizeKeys([]string{" a ", "", "a", "b", "b", " c"})
	if strings.Join(keys, ",") != "a,b,c" {
		t.Fatalf("normalize = %v", keys)
	}
	if got := EncodeDependencies(nil); got != "" {
		t.Fatalf("empty encode = %q", got)
	}
	raw := EncodeDependencies([]string{"a", "b"})
	if got := ParseDependencies(raw); strings.Join(got, ",") != "a,b" {
		t.Fatalf("round trip = %v", got)
	}
	if got := ParseDependencies("not json"); got != nil {
		t.Fatalf("bad json should be empty, got %v", got)
	}
}
