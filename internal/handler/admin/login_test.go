package admin_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	handler "github.com/jami1024/omnihub/internal/handler/admin"
	"github.com/jami1024/omnihub/internal/service/admin"
	"github.com/jami1024/omnihub/internal/service/guard"
)

func newLoginEngine(t *testing.T, email, password string) (*gin.Engine, *admin.Issuer) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	iss := admin.NewIssuer([]byte("test-secret"), time.Hour)
	hash, err := admin.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.POST("/admin/api/login", handler.LoginHandler(handler.EnvAdminCredentials{
		Email:        email,
		PasswordHash: hash,
	}, iss))
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
	r, iss := newLoginEngine(t, "root@example.com", "hunter2")

	rec := doJSON(r, `{"email":"ROOT@example.com","password":"hunter2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
		Username  string `json:"username"`
		Email     string `json:"email"`
		Role      string `json:"role"`
		Redirect  string `json:"redirect_to"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Username != "root@example.com" || resp.Email != "root@example.com" || resp.Role != "admin" || resp.Redirect != "/admin" || resp.Token == "" {
		t.Errorf("login response = %+v", resp)
	}
	claims, err := iss.Verify(resp.Token)
	if err != nil {
		t.Fatalf("issued token does not verify: %v", err)
	}
	if claims.Sub != "root@example.com" || claims.Kind != admin.KindAdmin {
		t.Errorf("claims = %+v, want admin email subject", claims)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	r, _ := newLoginEngine(t, "root@example.com", "hunter2")
	rec := doJSON(r, `{"email":"root@example.com","password":"wrong"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_credentials") {
		t.Errorf("expected invalid_credentials envelope, got %s", rec.Body.String())
	}
}

func TestLoginUnknownEmail(t *testing.T) {
	r, _ := newLoginEngine(t, "root@example.com", "hunter2")
	rec := doJSON(r, `{"email":"nobody@example.com","password":"hunter2"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid_credentials") {
		t.Errorf("unknown email should look like a wrong password, got %s", rec.Body.String())
	}
}

func TestLoginBadJSON(t *testing.T) {
	r, _ := newLoginEngine(t, "root@example.com", "hunter2")
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
