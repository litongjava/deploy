// Package web 把构建好的前端静态资源嵌进二进制。
//
// 这样交付物就是一个可执行文件：解压、启动、访问 10055 就有完整 UI
// （需求 D4：SQLite + 单二进制 + 前端 embed）。
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

// all: 前缀让 dist 下的点文件（.gitkeep 之类）也被嵌进来，
// 否则空目录会导致 go:embed 编译失败。
//
//go:embed all:dist
var distFS embed.FS

// Available 报告内嵌资源是否是「真的构建过的前端」。
// 仓库里放的占位 index.html 会被识别出来，用于在自检页给出提示。
func Available() bool {
	raw, err := fs.ReadFile(distFS, "dist/index.html")
	if err != nil {
		return false
	}
	return !strings.Contains(string(raw), "__PLACEHOLDER__")
}

// Register 注册静态资源与 SPA 回退。
//
// 规则：命中真实文件就返回文件；否则一律返回 index.html，
// 让前端路由（/projects/xxx）在刷新时也能正常打开。
// /api/ 前缀下的 404 仍然返回 JSON，不能被前端吞掉。
func Register(r *gin.Engine) error {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return err
	}
	fileServer := http.FileServer(http.FS(sub))

	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/playwright/") {
			c.JSON(http.StatusNotFound, gin.H{
				"success": false, "data": nil,
				"errorCode": 40400, "errorMessage": "endpoint not found: " + p,
			})
			return
		}

		clean := strings.TrimPrefix(path.Clean(p), "/")
		if clean != "" && clean != "." {
			if st, err := fs.Stat(sub, clean); err == nil && !st.IsDir() {
				setCacheHeaders(c, clean)
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
		}

		// SPA 回退：统一交给 index.html，且绝不缓存，
		// 否则前端发版后用户会一直拿到旧壳子。
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Request.URL.Path = "/"
		fileServer.ServeHTTP(c.Writer, c.Request)
	})
	return nil
}

// setCacheHeaders 给带哈希的静态资源长缓存，HTML 一律不缓存。
func setCacheHeaders(c *gin.Context, clean string) {
	switch {
	case strings.HasSuffix(clean, ".html"):
		c.Header("Cache-Control", "no-cache")
	case hasExt(clean):
		c.Header("Cache-Control", "public, max-age=2592000")
	}
}

func hasExt(p string) bool {
	switch path.Ext(p) {
	case ".js", ".css", ".svg", ".png", ".jpg", ".jpeg", ".webp", ".ico", ".woff", ".woff2", ".map":
		return true
	}
	return false
}
