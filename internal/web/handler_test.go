package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/web"
)

func TestSPAHandlerServesIndex(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	spa := web.SPAHandler("/admin")
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/admin") {
			spa(c)
			return
		}
		c.AbortWithStatus(http.StatusNotFound)
	})

	cases := []struct {
		path string
		want string
	}{
		{"/admin", "OmniHub admin"},
		{"/admin/", "OmniHub admin"},
		{"/admin/keys", "OmniHub admin"},       // SPA fallback
		{"/admin/index.html", "OmniHub admin"}, // direct asset
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Errorf("body missing %q: %s", tc.want, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Errorf("content-type = %q, want text/html", ct)
			}
		})
	}
}

func TestSPAHandlerLeavesNonAdminAlone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	spa := web.SPAHandler("/admin")
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/admin") {
			spa(c)
			return
		}
		c.AbortWithStatus(http.StatusNotFound)
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/messages", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("non-admin path should 404, got %d", rec.Code)
	}
}
