package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	handler "github.com/jami1024/omnihub/internal/handler/admin"
	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/admin"
	"github.com/jami1024/omnihub/internal/service/guard"
)

// stubRepo satisfies the (unexported) userLookup interface in the
// login handler — we feed it a fixed user and toggle the failure mode
// per test.
type stubRepo struct {
	user *admin.User
	err  error
}

func (s *stubRepo) GetByUsername(_ context.Context, _ string) (*admin.User, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.user, nil
}

func newLoginEngine(t *testing.T, repo *stubRepo) (*gin.Engine, *admin.Issuer) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	iss := admin.NewIssuer([]byte("test-secret"), time.Hour)
	r := gin.New()
	r.POST("/admin/api/login", handler.LoginHandler(repo, iss))
	return r, iss
}

func doJSON(r *gin.Engine, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/api/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	return rec
}

func TestLoginSuccess(t *testing.T) {
	hash, _ := admin.HashPassword("hunter2")
	r, iss := newLoginEngine(t, &stubRepo{
		user: &admin.User{ID: 7, Username: "root", PasswordHash: hash, Enabled: true},
	})

	rec := doJSON(r, `{"username":"root","password":"hunter2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
		Username  string `json:"username"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Username != "root" || resp.Token == "" {
		t.Errorf("login response = %+v", resp)
	}
	claims, err := iss.Verify(resp.Token)
	if err != nil {
		t.Fatalf("issued token does not verify: %v", err)
	}
	if claims.Sub != "root" || claims.UID != 7 {
		t.Errorf("claims = %+v, want sub=root uid=7", claims)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	hash, _ := admin.HashPassword("hunter2")
	r, _ := newLoginEngine(t, &stubRepo{
		user: &admin.User{ID: 7, Username: "root", PasswordHash: hash, Enabled: true},
	})
	rec := doJSON(r, `{"username":"root","password":"wrong"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_credentials") {
		t.Errorf("expected invalid_credentials envelope, got %s", rec.Body.String())
	}
}

func TestLoginUnknownUser(t *testing.T) {
	r, _ := newLoginEngine(t, &stubRepo{err: repository.ErrAdminUserNotFound})
	rec := doJSON(r, `{"username":"nobody","password":"x"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid_credentials") {
		t.Errorf("unknown user should look like a wrong password, got %s", rec.Body.String())
	}
}

func TestLoginDisabledUser(t *testing.T) {
	hash, _ := admin.HashPassword("hunter2")
	r, _ := newLoginEngine(t, &stubRepo{
		user: &admin.User{ID: 7, Username: "root", PasswordHash: hash, Enabled: false},
	})
	rec := doJSON(r, `{"username":"root","password":"hunter2"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid_credentials") {
		t.Errorf("disabled user should mask as wrong-password, got %s", rec.Body.String())
	}
}

func TestLoginBadJSON(t *testing.T) {
	r, _ := newLoginEngine(t, &stubRepo{})
	rec := doJSON(r, `{not json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestMeReturnsIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	iss := admin.NewIssuer([]byte("test-secret"), time.Hour)
	r := gin.New()
	authMW := guard.NewAdminAuthenticator(iss).Middleware()
	r.GET("/admin/api/me", authMW, handler.MeHandler())

	tok, _, _ := iss.Issue("root", 42)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.ID != 42 || resp.Username != "root" {
		t.Errorf("me = %+v", resp)
	}
}

func TestMeRejectsMissingAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	iss := admin.NewIssuer([]byte("test-secret"), time.Hour)
	r := gin.New()
	authMW := guard.NewAdminAuthenticator(iss).Middleware()
	r.GET("/admin/api/me", authMW, handler.MeHandler())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/api/me", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("missing auth: status = %d, want 401", rec.Code)
	}
}
