// Package ids 生成各类随机标识：API Token、Hook Token、密码学随机串。
package ids

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
)

// base62 去掉容易看混的字符（0/O、1/l/I），便于人工抄写与排错。
const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func randomString(n int) string {
	buf := make([]byte, n)
	max := big.NewInt(int64(len(alphabet)))
	for i := range buf {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			// crypto/rand 失败属于系统级故障，没有合理降级方案。
			panic("crypto/rand 不可用: " + err.Error())
		}
		buf[i] = alphabet[v.Int64()]
	}
	return string(buf)
}

// NewAPIToken 生成 CLI / 第三方用的接口令牌，返回明文与用于展示的前缀。
func NewAPIToken() (plain, prefix string) {
	plain = "dk_" + randomString(32)
	return plain, plain[:8]
}

// NewHookToken 生成免登录触发部署的 Hook 令牌。
func NewHookToken() (plain, prefix string) {
	plain = "hk_" + randomString(32)
	return plain, plain[:8]
}

// HashToken 是令牌的落库形式。令牌是高熵随机串，
// 用 SHA-256 足够（不需要 bcrypt 那种抗暴力破解的慢哈希）。
func HashToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}
