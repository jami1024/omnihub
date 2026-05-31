package web

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

// SPAHandler returns a gin handler that serves the embedded React app
// rooted at the given URL prefix (typically "/admin"). Resolved files in
// dist/ are served with the correct Content-Type; anything that doesn't
// resolve falls back to index.html so the React router can take over
// (deep links like /admin/keys still load the SPA shell instead of
// 404-ing).
//
// Intended for use as gin's NoRoute fallback rather than as a wildcard
// route — gin disallows catch-all routes that share a prefix with
// concrete routes (which /admin/api/* would conflict with).
func SPAHandler(prefix string) gin.HandlerFunc {
	root := AssetsFS()
	indexBytes, indexErr := fs.ReadFile(root, "index.html")
	prefix = strings.TrimSuffix(prefix, "/")
	return func(c *gin.Context) {
		reqPath := strings.TrimPrefix(c.Request.URL.Path, prefix)
		reqPath = strings.TrimPrefix(reqPath, "/")
		if reqPath == "" {
			reqPath = "index.html"
		}

		data, err := fs.ReadFile(root, reqPath)
		if err == nil {
			c.Data(http.StatusOK, contentTypeFor(reqPath), data)
			return
		}
		if !errors.Is(err, fs.ErrNotExist) {
			c.Data(http.StatusInternalServerError, "text/plain", []byte(err.Error()))
			return
		}
		// SPA fallback: any non-asset path renders the index so client
		// routing handles it.
		if indexErr != nil {
			c.Data(http.StatusNotFound, "text/plain",
				[]byte("admin UI bundle missing; rebuild with the frontend stage"))
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexBytes)
	}
}

// contentTypeFor maps a file extension to a Content-Type. Limited to
// the handful of types Vite emits — anything else falls back to
// application/octet-stream and lets the browser cope.
func contentTypeFor(name string) string {
	switch path.Ext(name) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".ico":
		return "image/x-icon"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".map":
		return "application/json; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}
