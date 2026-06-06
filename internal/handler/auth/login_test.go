package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	adminhandler "github.com/jami1024/omnihub/internal/handler/admin"
	"github.com/jami1024/omnihub/internal/handler/auth"
	"github.com/jami1024/omnihub/internal/repository"
	adminsvc "github.com/jami1024/omnihub/internal/service/admin"
)

type users struct{ user *repository.User }

func (u users) GetByEmail(context.Context, string) (*repository.User, error) {
	if u.user == nil {
		return nil, repository.ErrUserNotFound
	}
	return u.user, nil
}

func engine(t *testing.T, user *repository.User) (*gin.Engine, *adminsvc.Issuer) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	adminHash, err := adminsvc.HashPassword("adminpw123")
	if err != nil {
		t.Fatal(err)
	}
	iss := adminsvc.NewIssuer([]byte("test-secret"), 0)
	r := gin.New()
	r.POST("/auth/api/login", auth.LoginHandler(adminhandler.EnvAdminCredentials{
		Email: "admin@example.com", PasswordHash: adminHash,
	}, users{user: user}, iss))
	return r, iss
}

func post(r *gin.Engine, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/api/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	return rec
}

func TestUnifiedLoginReturnsAdminRoleForEnvEmail(t *testing.T) {
	r, iss := engine(t, nil)
	rec := post(r, `{"email":"ADMIN@example.com","password":"adminpw123"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d want 200 (%s)", rec.Code, rec.Body.String())
	}
	var out struct {
		Token      string `json:"token"`
		Role       string `json:"role"`
		RedirectTo string `json:"redirect_to"`
		Email      string `json:"email"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Role != "admin" || out.RedirectTo != "/admin" || out.Email != "admin@example.com" {
		t.Fatalf("unexpected response: %+v", out)
	}
	claims, err := iss.Verify(out.Token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Kind != adminsvc.KindAdmin {
		t.Fatalf("admin login should issue admin token, got %+v", claims)
	}
}

func TestUnifiedLoginReturnsUserRoleForPortalEmail(t *testing.T) {
	hash, err := adminsvc.HashPassword("userpw123")
	if err != nil {
		t.Fatal(err)
	}
	r, iss := engine(t, &repository.User{
		ID: 9, Username: "alice@example.com", Email: "alice@example.com", PasswordHash: hash, Enabled: true,
	})
	rec := post(r, `{"email":"alice@example.com","password":"userpw123"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d want 200 (%s)", rec.Code, rec.Body.String())
	}
	var out struct {
		Token      string `json:"token"`
		Role       string `json:"role"`
		RedirectTo string `json:"redirect_to"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Role != "user" || out.RedirectTo != "/portal" {
		t.Fatalf("unexpected response: %+v", out)
	}
	claims, err := iss.Verify(out.Token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Kind != adminsvc.KindUser || claims.UID != 9 {
		t.Fatalf("user login should issue user token, got %+v", claims)
	}
}
