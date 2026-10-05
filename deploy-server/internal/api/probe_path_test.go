package api

import "testing"

// 探针路径要按执行环境展开：nvm 的官方安装脚本装在 $HOME/.nvm，
// 本平台默认装在 /usr/local/nvm，两条路径都得能认出来。
func TestExpandProbePath(t *testing.T) {
	env := []string{"HOME=/root", "NVM_DIR=/usr/local/nvm"}

	cases := []struct{ in, want string }{
		{"/usr/local/nvm/nvm.sh", "/usr/local/nvm/nvm.sh"},
		{"$NVM_DIR/nvm.sh", "/usr/local/nvm/nvm.sh"},
		{"${NVM_DIR}/nvm.sh", "/usr/local/nvm/nvm.sh"},
		{"$HOME/.nvm/nvm.sh", "/root/.nvm/nvm.sh"},
		{"~/.nvm/nvm.sh", "/root/.nvm/nvm.sh"},
		{"~", "/root"},
		// 没有的变量展开成空串，路径自然不存在（不会被当成命中）
		{"$NOT_SET/nvm.sh", "/nvm.sh"},
		{"", ""},
	}
	for _, c := range cases {
		if got := expandProbePath(c.in, env); got != c.want {
			t.Errorf("expandProbePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	// 没有 HOME 时不能把 ~ 展开成相对路径
	if got := expandProbePath("~/x", []string{"PATH=/usr/bin"}); got != "~/x" {
		t.Errorf("HOME 缺失时不该展开 ~，得到 %q", got)
	}
}
