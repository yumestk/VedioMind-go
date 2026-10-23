package app

import (
	"github.com/gin-gonic/gin"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func (s *Server) notFoundOrSPA(c *gin.Context) {
	requested := c.Request.URL.Path
	if requested == "/api" || strings.HasPrefix(requested, "/api/") || (c.Request.Method != "GET" && c.Request.Method != "HEAD") {
		notFound(c)
		return
	}
	root := envString("WEB_DIST", "web/dist")
	clean := strings.TrimPrefix(path.Clean("/"+requested), "/")
	for _, part := range strings.Split(clean, "/") {
		if strings.HasPrefix(part, ".") {
			notFound(c)
			return
		}
	}
	if clean != "" {
		if stat, err := fs.Stat(os.DirFS(root), clean); err == nil && !stat.IsDir() {
			c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: https:; frame-src https://player.bilibili.com https://www.youtube-nocookie.com; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'")
			http.ServeFile(c.Writer, c.Request, filepath.Join(root, filepath.FromSlash(clean)))
			return
		}
	}
	if strings.HasPrefix(clean, "assets/") || (clean != "" && strings.Contains(path.Base(clean), ".")) {
		notFound(c)
		return
	}
	if _, err := os.Stat(filepath.Join(root, "index.html")); err != nil {
		notFound(c)
		return
	}
	c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: https:; frame-src https://player.bilibili.com https://www.youtube-nocookie.com; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'")
	http.ServeFile(c.Writer, c.Request, filepath.Join(root, "index.html"))
}
