package portal_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	portal "github.com/jami1024/omnihub/internal/handler/portal"
	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/admin"
	"github.com/jami1024/omnihub/internal/service/apikey"
	"github.com/jami1024/omnihub/internal/service/guard"
)

func do(r *gin.Engine, method, path, body, auth string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	r.ServeHTTP(rec, req)
	return rec
}

type fakeUserStore struct {
	insertID  int64
	insertErr error
	lastUser  repository.UserInsertParams
}

func (f *fakeUserStore) GetByUsername(context.Context, string) (*repository.User, error) {
	return nil, repository.ErrUserNotFound
}
func (f *fakeUserStore) GetByID(_ context.Context, id int64) (*repository.User, error) {
	return &repository.User{ID: id, Username: "alice"}, nil
}
func (f *fakeUserStore) Insert(_ context.Context, p repository.UserInsertParams) (int64, error) {
	f.lastUser = p
	return f.insertID, f.insertErr
}

func newIssuer() *admin.Issuer { return admin.NewIssuer([]byte("test-secret"), 0) }

func TestSignupValidatesAndHashes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &fakeUserStore{insertID: 1}
	r := gin.New()
	r.POST("/portal/api/signup", portal.SignupHandler(store, newIssuer()))

	// short password rejected
	if rec := do(r, http.MethodPost, "/portal/api/signup", `{"username":"alice","password":"short"}`, ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("short password: status %d want 400", rec.Code)
	}
	// happy path → token, password stored hashed (not cleartext)
	rec := do(r, http.MethodPost, "/portal/api/signup", `{"username":"alice","password":"alicepw12345"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("signup status %d want 200 (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "token") {
		t.Error("signup should return a token")
	}
	if store.lastUser.PasswordHash == "" || store.lastUser.PasswordHash == "alicepw12345" {
		t.Errorf("password must be hashed, got %q", store.lastUser.PasswordHash)
	}
	if admin.VerifyPassword(store.lastUser.PasswordHash, "alicepw12345") != nil {
		t.Error("stored hash should verify against the cleartext")
	}
}

func TestSignupUsernameTaken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &fakeUserStore{insertErr: repository.ErrUsernameTaken}
	r := gin.New()
	r.POST("/portal/api/signup", portal.SignupHandler(store, newIssuer()))
	rec := do(r, http.MethodPost, "/portal/api/signup", `{"username":"taken","password":"alicepw12345"}`, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d want 409", rec.Code)
	}
}

type fakeKeyStore struct {
	list       []*apikey.Key
	insertID   int64
	lastParams repository.ApiKeyInsertParams
	deletedID  int64
	deletedBy  int64
	deleteErr  error
}

func (f *fakeKeyStore) ListByUser(_ context.Context, _ int64) ([]*apikey.Key, error) {
	return f.list, nil
}
func (f *fakeKeyStore) Insert(_ context.Context, p repository.ApiKeyInsertParams) (int64, error) {
	f.lastParams = p
	return f.insertID, nil
}
func (f *fakeKeyStore) GetByID(_ context.Context, id int64) (*apikey.Key, error) {
	return &apikey.Key{ID: id, Name: "alice-key-1", Enabled: true}, nil
}
func (f *fakeKeyStore) DeleteByIDOwnedBy(_ context.Context, id, uid int64) error {
	f.deletedID, f.deletedBy = id, uid
	return f.deleteErr
}

// engineWithUser mounts a portal route behind a middleware that fakes an
// authenticated user (id=7), exercising the real handlers + scoping.
func engineWithUser(register func(r *gin.Engine)) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(guard.CtxKeyUserID, int64(7)); c.Next() })
	register(r)
	return r
}

func TestCreateKeyOwnedByUserAndCleartextOnce(t *testing.T) {
	store := &fakeKeyStore{insertID: 3}
	r := engineWithUser(func(r *gin.Engine) {
		r.POST("/portal/api/keys", portal.CreateKeyHandler(store))
	})
	rec := do(r, http.MethodPost, "/portal/api/keys", `{"name":"alice-key-1"}`, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d want 201 (%s)", rec.Code, rec.Body.String())
	}
	if store.lastParams.UserID == nil || *store.lastParams.UserID != 7 {
		t.Errorf("key must be owned by user 7, got %v", store.lastParams.UserID)
	}
	if store.lastParams.Hash == "" || !strings.Contains(rec.Body.String(), `"key":"omni-`) {
		t.Error("create must store a hash and return the cleartext once")
	}
}

func TestDeleteKeyScopedToOwner(t *testing.T) {
	store := &fakeKeyStore{}
	r := engineWithUser(func(r *gin.Engine) {
		r.DELETE("/portal/api/keys/:id", portal.DeleteKeyHandler(store))
	})
	rec := do(r, http.MethodDelete, "/portal/api/keys/9", "", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d want 204", rec.Code)
	}
	if store.deletedID != 9 || store.deletedBy != 7 {
		t.Errorf("delete must be scoped to (id=9, user=7), got (%d, %d)", store.deletedID, store.deletedBy)
	}
}
