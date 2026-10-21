package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectSshKeys(t *testing.T) {
	if keys := collectSshKeys(""); len(keys) != 0 {
		t.Fatalf("empty home should return no keys, got %d", len(keys))
	}

	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}

	edPub := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey deploy@test\n"
	if err := os.WriteFile(filepath.Join(sshDir, "id_ed25519.pub"), []byte(edPub), 0o644); err != nil {
		t.Fatal(err)
	}
	// 私钥存在但没有对应公钥时不应被列出（接口只暴露公钥）。
	if err := os.WriteFile(filepath.Join(sshDir, "id_rsa"), []byte("PRIVATE"), 0o600); err != nil {
		t.Fatal(err)
	}

	keys := collectSshKeys(home)
	if len(keys) != 1 {
		t.Fatalf("expected 1 public key, got %d", len(keys))
	}
	if keys[0].Type != "ed25519" {
		t.Errorf("type = %q, want ed25519", keys[0].Type)
	}
	if keys[0].PublicKey != "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey deploy@test" {
		t.Errorf("unexpected public key content: %q", keys[0].PublicKey)
	}
	if keys[0].KeyPath != filepath.Join(sshDir, "id_ed25519.pub") {
		t.Errorf("keyPath = %q", keys[0].KeyPath)
	}

	// ed25519 排在 rsa 之前，与 sshKeyTypes 的展示优先级一致。
	rsaPub := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQTestKey deploy@test\n"
	if err := os.WriteFile(filepath.Join(sshDir, "id_rsa.pub"), []byte(rsaPub), 0o644); err != nil {
		t.Fatal(err)
	}
	keys = collectSshKeys(home)
	if len(keys) != 2 {
		t.Fatalf("expected 2 public keys, got %d", len(keys))
	}
	if keys[0].Type != "ed25519" || keys[1].Type != "rsa" {
		t.Errorf("unexpected order: %q, %q", keys[0].Type, keys[1].Type)
	}
}
