package toolchain

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Run installer functions with all system mutations stubbed, in a private directory.
func TestInstallerSources(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash unavailable")
	}
	raw, _ := Script()
	script := string(raw)
	setup := script[:strings.Index(script, "# ---------- 平台识别")]
	funcs := script[strings.Index(script, "profile_write() {"):strings.Index(script, "# ---------- 选择组件")]
	for _, mode := range []string{"off", "cn"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			cargo := filepath.Join(dir, "cargo")
			m2 := filepath.Join(dir, ".m2")
			os.MkdirAll(cargo, 0700)
			os.MkdirAll(m2, 0700)
			for _, f := range []string{filepath.Join(cargo, "config.toml"), filepath.Join(m2, "settings.xml")} {
				os.WriteFile(f, []byte("custom configuration"), 0600)
			}
			harness := setup + funcs + `
DRY_RUN=1; FORCE=1; GOARCH=amd64; PKG=apt
HOME="$TEST_DIR"; CARGO_HOME="$TEST_DIR/cargo"
ensure_prereqs() { :; }
pkg_install() { :; }
detect_nvm() { return 0; }
detect_node() { return 0; }
install_go
install_node
install_pnpm
install_maven
install_rust
node_shell 'printf "resolved-node=%s resolved-npm=%s resolved-corepack=%s\n" "$NVM_NODEJS_ORG_MIRROR" "$npm_config_registry" "$COREPACK_NPM_REGISTRY"'
`
			cmd := exec.Command(bash, "-s", "--", "--mirror", mode)
			cmd.Stdin = strings.NewReader(harness)
			cmd.Env = append(os.Environ(), "TEST_DIR="+filepath.ToSlash(dir), "NVM_NODEJS_ORG_MIRROR=https://old.example/node", "RUSTUP_DIST_SERVER=https://old.example/rust")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			wants := []string{"settings.xml", "config.toml"}
			if mode == "off" {
				wants = append(wants, "https://go.dev/dl/", "GOPROXY=https://proxy.golang.org,direct", "https://static.rust-lang.org", "resolved-node=https://nodejs.org/dist", "resolved-npm=https://registry.npmjs.org", "resolved-corepack=https://registry.npmjs.org")
				if strings.Contains(string(out), "old.example") {
					t.Fatalf("old mirror leaked: %s", out)
				}
			} else {
				wants = append(wants, "https://mirrors.aliyun.com/golang/", "GOPROXY=https://goproxy.cn,direct", "https://old.example/rust", "resolved-node=https://old.example/node", "resolved-npm=https://registry.npmmirror.com")
			}
			for _, want := range wants {
				if !strings.Contains(string(out), want) {
					t.Errorf("missing %q: %s", want, out)
				}
			}
			for _, f := range []string{filepath.Join(cargo, "config.toml"), filepath.Join(m2, "settings.xml")} {
				data, _ := os.ReadFile(f)
				if string(data) != "custom configuration" {
					t.Fatalf("config overwritten: %s", f)
				}
			}
		})
	}
}
