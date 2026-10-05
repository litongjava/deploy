package runner

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 登录 shell 的 stdout 里可能混着欢迎语之类的杂输出，只有合法的 KEY=VALUE 才算环境变量。
func TestParseEnvironBlob(t *testing.T) {
	blob := strings.Join([]string{
		"Welcome to MiWiFi-R4A-srv",
		"HOME=/root",
		"PATH=/usr/java/jdk1.8.0_411/bin:/usr/maven/apache-maven-3.8.8/bin",
		"JAVA_HOME=/usr/java/jdk1.8.0_411",
		"=no-key",
		"1BAD=digit-first",
		"MULTI=a=b",
		"bad key=has-space",
	}, "\x00")

	env := parseEnvironBlob(blob)

	if got := envLookup(env, "PATH"); got != "/usr/java/jdk1.8.0_411/bin:/usr/maven/apache-maven-3.8.8/bin" {
		t.Fatalf("PATH = %q, want the java/maven dirs", got)
	}
	if got := envLookup(env, "JAVA_HOME"); got != "/usr/java/jdk1.8.0_411" {
		t.Fatalf("JAVA_HOME = %q", got)
	}
	// 值里带 = 时只按第一个 = 切分
	if got := envLookup(env, "MULTI"); got != "a=b" {
		t.Fatalf("MULTI = %q, want \"a=b\"", got)
	}
	// 非法键名与杂输出都必须被丢掉
	if len(env) != 4 {
		t.Fatalf("env = %v, want exactly 4 entries", env)
	}
	for _, bad := range []string{"Welcome to MiWiFi-R4A-srv", "1BAD", "bad key"} {
		if envLookup(env, bad) != "" {
			t.Fatalf("%q 不该被解析成环境变量", bad)
		}
	}
}

func TestWithEnvValue(t *testing.T) {
	env := withEnvValue([]string{"HOME=/root", "PATH=/usr/bin", "PATH=/usr/local/bin"}, "PATH", "/opt/bin")
	if got := envLookup(env, "PATH"); got != "/opt/bin" {
		t.Fatalf("PATH = %q, want /opt/bin", got)
	}
	if len(env) != 2 {
		t.Fatalf("env = %v, want exactly 2 entries（重复键要去重）", env)
	}
	if got := envLookup(env, "HOME"); got != "/root" {
		t.Fatalf("HOME 不该被动过: %q", got)
	}

	// 键不存在时追加
	env = withEnvValue(env, "LANG", "en_US.UTF-8")
	if got := envLookup(env, "LANG"); got != "en_US.UTF-8" {
		t.Fatalf("LANG = %q, want appended", got)
	}
}

// lookPathInDirs 只看给定的 PATH，不看进程自己的 PATH —— 这正是「装了却找不到」的关键。
func TestLookPathInDirs(t *testing.T) {
	if runtime.GOOS == "windows" {
		// 执行位是 Unix 语义；Windows 上 LookPathIn 直接走 exec.LookPath（认 PATHEXT 的 .exe/.bat），
		// 不会走到这里，所以这条用例只在 Unix 上跑。
		t.Skip("Windows 没有执行位语义")
	}
	dir := t.TempDir()
	java := filepath.Join(dir, "java")
	if err := os.WriteFile(java, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 没有执行位的同名文件不算命中（避免把数据文件当命令）
	notExec := filepath.Join(dir, "mvn")
	if err := os.WriteFile(notExec, []byte("not a program"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 目录也不算命中
	if err := os.Mkdir(filepath.Join(dir, "gradle"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got, err := lookPathInDirs("java", dir); err != nil || got != java {
		t.Fatalf("lookPathInDirs(java) = %q, %v; want %q", got, err, java)
	}
	if _, err := lookPathInDirs("mvn", dir); err == nil {
		t.Fatal("没有执行位的文件不该被当成命令")
	}
	if _, err := lookPathInDirs("gradle", dir); err == nil {
		t.Fatal("目录不该被当成命令")
	}
	if _, err := lookPathInDirs("node", dir+string(filepath.ListSeparator)); err == nil {
		t.Fatal("PATH 里没有的工具应当报错")
	}
}

func TestValidEnvKey(t *testing.T) {
	for _, ok := range []string{"PATH", "JAVA_HOME", "_x", "a1"} {
		if !validEnvKey(ok) {
			t.Fatalf("%q 应当是合法键名", ok)
		}
	}
	for _, bad := range []string{"", "1PATH", "A B", "A-B", "A\nB"} {
		if validEnvKey(bad) {
			t.Fatalf("%q 不该是合法键名", bad)
		}
	}
}

// LoginEnv 在 Unix 上要能拿到登录 shell 的 PATH（这正是 java/maven 能被找到的原因）；
// Windows 或拿不到时退回 os.Environ()，也必须带上 PATH。
func TestLoginEnvAlwaysHasPath(t *testing.T) {
	env := LoginEnv("auto")
	if envLookup(env, "PATH") == "" {
		t.Fatalf("LoginEnv 必须带上 PATH，实际拿到 %d 个变量", len(env))
	}
	if runtime.GOOS == "windows" && len(env) != len(os.Environ()) {
		t.Fatalf("Windows 上应当直接沿用服务进程环境")
	}
}
