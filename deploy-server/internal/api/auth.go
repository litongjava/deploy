package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"deploy-server/internal/ids"
	"deploy-server/internal/store"
)

// Claims 是 JWT 载荷。
type Claims struct {
	UserID   uint   `json:"uid"`
	Username string `json:"usr"`
	Role     string `json:"rol"`
	jwt.RegisteredClaims
}

// AuthManager 负责签发/校验登录令牌，以及把请求上下文里的用户解析出来。
type AuthManager struct {
	st     *store.Store
	secret []byte
	ttl    time.Duration
}

// NewAuthManager 构造鉴权管理器。
func NewAuthManager(st *store.Store, secret string, ttl time.Duration) *AuthManager {
	return &AuthManager{st: st, secret: []byte(secret), ttl: ttl}
}

// HashPassword 用 bcrypt 生成口令哈希。
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(b), err
}

// CheckPassword 校验口令。
func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// Issue 签发登录令牌。
func (a *AuthManager) Issue(u *store.User) (string, time.Time, error) {
	exp := time.Now().Add(a.ttl)
	claims := Claims{
		UserID: u.ID, Username: u.Username, Role: u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "deploy-server",
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString(a.secret)
	return s, exp, err
}

// Parse 校验并解析登录令牌。
func (a *AuthManager) Parse(raw string) (*Claims, error) {
	var claims Claims
	_, err := jwt.ParseWithClaims(raw, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("签名算法不符")
		}
		return a.secret, nil
	})
	if err != nil {
		return nil, err
	}
	return &claims, nil
}

// ContextKeyUser 是 gin.Context 里当前用户的键。
const ContextKeyUser = "currentUser"

// tokenFromRequest 从 Authorization 头里取出令牌。
// 同时接受裸 token 与 "Bearer <token>"，因为前端模板发的是裸 token。
func tokenFromRequest(c *gin.Context) string {
	raw := strings.TrimSpace(c.GetHeader("Authorization"))
	if raw == "" {
		return ""
	}
	if len(raw) > 7 && strings.EqualFold(raw[:7], "bearer ") {
		return strings.TrimSpace(raw[7:])
	}
	return raw
}

// Middleware 是登录态校验中间件，支持 JWT 与 API Token 两种凭据。
func (a *AuthManager) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := tokenFromRequest(c)
		if raw == "" {
			Unauthorized(c, "未登录或登录已过期")
			return
		}

		// API Token 有固定前缀，先分流，避免拿 dk_ 去解 JWT。
		if strings.HasPrefix(raw, "dk_") {
			u, err := a.userByAPIToken(raw)
			if err != nil {
				Unauthorized(c, "接口令牌无效或已过期")
				return
			}
			c.Set(ContextKeyUser, u)
			c.Next()
			return
		}

		claims, err := a.Parse(raw)
		if err != nil {
			Unauthorized(c, "未登录或登录已过期")
			return
		}
		var u store.User
		if err := a.st.DB().First(&u, claims.UserID).Error; err != nil {
			Unauthorized(c, "账号不存在")
			return
		}
		if u.Status != "active" {
			Forbidden(c, "账号已被禁用")
			return
		}
		c.Set(ContextKeyUser, &u)
		c.Next()
	}
}

func (a *AuthManager) userByAPIToken(raw string) (*store.User, error) {
	hash := ids.HashToken(raw)
	var t store.APIToken
	if err := a.st.DB().Where("token_hash = ?", hash).First(&t).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	if t.ExpiresAt != nil && now.After(*t.ExpiresAt) {
		return nil, errors.New("令牌已过期")
	}
	var u store.User
	if err := a.st.DB().First(&u, t.UserID).Error; err != nil {
		return nil, err
	}
	if u.Status != "active" {
		return nil, errors.New("账号已禁用")
	}
	// 记录最后使用时间，失败不影响本次请求。
	go func() {
		_ = a.st.DB().Model(&store.APIToken{}).Where("id = ?", t.ID).
			Update("last_used_at", now).Error
	}()
	return &u, nil
}

// RequireAdmin 要求当前用户是管理员。
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		u := CurrentUser(c)
		if u == nil || u.Role != "admin" {
			Forbidden(c, "需要管理员权限")
			return
		}
		c.Next()
	}
}

// CurrentUser 取出当前登录用户；未登录返回 nil。
func CurrentUser(c *gin.Context) *store.User {
	v, ok := c.Get(ContextKeyUser)
	if !ok {
		return nil
	}
	u, ok := v.(*store.User)
	if !ok {
		return nil
	}
	return u
}

// ClientIP 取客户端 IP（gin 的 ClientIP 已处理 X-Forwarded-For）。
func ClientIP(c *gin.Context) string { return c.ClientIP() }

// findUserByID 是个小工具，多处用到。
func findUserByID(st *store.Store, id uint) (*store.User, error) {
	var u store.User
	if err := st.DB().First(&u, id).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

var errRecordNotFound = gorm.ErrRecordNotFound

// 保证 http 包被引用（Unauthorized 等用的是 gin，但 facade 常量来自 net/http）。
var _ = http.StatusOK
