package guard_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/service/guard"
)

func init() { gin.SetMode(gin.TestMode) }

// run wraps the Authenticator middleware with a no-op handler that
// records the resolved key label in the response body so tests can
// assert on it.
func run(t *testing.T, a *guard.Authenticator, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	r := gin.New()
	r.Use(a.Middleware())
	r.POST("/v1/messages", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"key_name": guard.KeyName(c)})
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestAuthenticatorParsesSpec(t *testing.T) {
	cases := []struct {
		spec      string
		wantKeys  int
		wantLabel map[string]string // key value → label
	}{
		{"", 0, nil},
		{"omni-abc", 1, map[string]string{"omni-abc": "default"}},
		{"alice:omni-abc", 1, map[string]string{"omni-abc": "alice"}},
		{"alice:omni-abc, bob:omni-def", 2, map[string]string{"omni-abc": "alice", "omni-def": "bob"}},
		{"  ,, alice:omni-abc ,, ", 1, map[string]string{"omni-abc": "alice"}},
	}
	for _, tc := range cases {
		t.Run(tc.spec, func(t *testing.T) {
			a := guard.NewAuthenticator(tc.spec)
			if got := a.KeyCount(); got != tc.wantKeys {
				t.Errorf("KeyCount: want %d, got %d", tc.wantKeys, got)
			}
			if tc.wantKeys == 0 && !a.Disabled() {
				t.Errorf("expected Disabled() for empty spec")
			}
		})
	}
}

func TestAuthAcceptsXAPIKey(t *testing.T) {
	a := guard.NewAuthenticator("alice:omni-abc")
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader("{}"))
	req.Header.Set("x-api-key", "omni-abc")

	rec := run(t, a, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var got struct{ KeyName string `json:"key_name"` }
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.KeyName != "alice" {
		t.Errorf("key_name: want alice, got %q", got.KeyName)
	}
}

func TestAuthAcceptsAuthorizationBearer(t *testing.T) {
	a := guard.NewAuthenticator("omni-abc")
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer omni-abc")

	rec := run(t, a, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
}

func TestAuthRejectsMissingKey(t *testing.T) {
	a := guard.NewAuthenticator("omni-abc")
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader("{}"))
	// no headers
	rec := run(t, a, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: want 401, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "authentication_error") {
		t.Errorf("body should mention authentication_error, got %s", rec.Body.String())
	}
}

func TestAuthRejectsInvalidKey(t *testing.T) {
	a := guard.NewAuthenticator("omni-abc")
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader("{}"))
	req.Header.Set("x-api-key", "wrong-key")

	rec := run(t, a, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: want 401, got %d", rec.Code)
	}
}

func TestAuthDisabledPassesThrough(t *testing.T) {
	a := guard.NewAuthenticator("")
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader("{}"))
	// no headers
	rec := run(t, a, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status: want 200 (auth disabled), got %d", rec.Code)
	}
	var got struct{ KeyName string `json:"key_name"` }
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.KeyName != "unauthenticated" {
		t.Errorf("key_name: want unauthenticated, got %q", got.KeyName)
	}
}
