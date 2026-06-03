package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	handler "github.com/jami1024/omnihub/internal/handler/admin"
	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/apikey"
)

// fakeKeyStore implements the (unexported) keyStore interface the key
// handlers depend on, driven by struct fields.
type fakeKeyStore struct {
	list        []*apikey.Key
	listErr     error
	getKey      *apikey.Key
	getErr      error
	insertID    int64
	insertErr   error
	updateErr   error
	deleteErr   error
	lastInsert  repository.ApiKeyInsertParams
	lastUpdate  repository.ApiKeyUpdateParams
	lastUpdated int64
	lastDeleted int64
}

func (f *fakeKeyStore) ListAll(context.Context) ([]*apikey.Key, error) {
	return f.list, f.listErr
}

func (f *fakeKeyStore) GetByID(_ context.Context, _ int64) (*apikey.Key, error) {
	return f.getKey, f.getErr
}

func (f *fakeKeyStore) Insert(_ context.Context, p repository.ApiKeyInsertParams) (int64, error) {
	f.lastInsert = p
	return f.insertID, f.insertErr
}

func (f *fakeKeyStore) UpdateMeta(_ context.Context, id int64, p repository.ApiKeyUpdateParams) error {
	f.lastUpdate = p
	f.lastUpdated = id
	return f.updateErr
}

func (f *fakeKeyStore) DeleteByID(_ context.Context, id int64) error {
	f.lastDeleted = id
	return f.deleteErr
}

func newKeyEngine(store *fakeKeyStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/admin/api/keys", handler.ListKeysHandler(store))
	r.POST("/admin/api/keys", handler.CreateKeyHandler(store))
	r.PATCH("/admin/api/keys/:id", handler.UpdateKeyHandler(store))
	r.DELETE("/admin/api/keys/:id", handler.DeleteKeyHandler(store))
	return r
}

func TestListKeysNeverLeaksHash(t *testing.T) {
	store := &fakeKeyStore{
		list: []*apikey.Key{
			{ID: 1, Name: "ci", Label: "ci-bot", Hash: "deadbeefdeadbeef", Enabled: true},
		},
	}
	rec := do(newKeyEngine(store), http.MethodGet, "/admin/api/keys", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "deadbeef") {
		t.Fatalf("key hash leaked into list response: %s", rec.Body.String())
	}
	var resp struct {
		Keys []struct {
			ID            int64    `json:"id"`
			AllowedModels []string `json:"allowed_models"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Keys) != 1 {
		t.Fatalf("keys = %d, want 1", len(resp.Keys))
	}
	// allowed_models must marshal as [] (not null) so the UI can map().
	if resp.Keys[0].AllowedModels == nil {
		t.Errorf("allowed_models should serialize as [], got null")
	}
}

func TestCreateKeyReturnsCleartextOnce(t *testing.T) {
	store := &fakeKeyStore{
		insertID: 7,
		getKey:   &apikey.Key{ID: 7, Name: "ci", Enabled: true},
	}
	body := `{"name":"ci","label":"ci-bot"}`
	rec := do(newKeyEngine(store), http.MethodPost, "/admin/api/keys", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID  int64  `json:"id"`
		Key string `json:"key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Key == "" {
		t.Fatal("create response must include the one-time cleartext key")
	}
	// The stored hash must be sha256(returned cleartext) — proving the
	// server persisted only the hash, not the value.
	if store.lastInsert.Hash != apikey.HashOf(resp.Key) {
		t.Errorf("stored hash %q != HashOf(returned key)", store.lastInsert.Hash)
	}
	if store.lastInsert.Hash == resp.Key {
		t.Error("stored the cleartext as the hash")
	}
}

func TestCreateKeyRequiresName(t *testing.T) {
	rec := do(newKeyEngine(&fakeKeyStore{}), http.MethodPost, "/admin/api/keys", `{"label":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestCreateKeyRejectsBadLimits(t *testing.T) {
	cases := map[string]string{
		"rpm zero":       `{"name":"a","rpm_limit":0}`,
		"rpm negative":   `{"name":"a","rpm_limit":-5}`,
		"daily negative": `{"name":"a","daily_usd_limit":-1.5}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := do(newKeyEngine(&fakeKeyStore{}), http.MethodPost, "/admin/api/keys", body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCreateKeyNameTaken(t *testing.T) {
	store := &fakeKeyStore{insertErr: repository.ErrApiKeyNameTaken}
	rec := do(newKeyEngine(store), http.MethodPost, "/admin/api/keys", `{"name":"dup"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "name_taken") {
		t.Errorf("expected name_taken code, got %s", rec.Body.String())
	}
}

func TestUpdateKeyForwardsFieldsAndCleansModels(t *testing.T) {
	store := &fakeKeyStore{getKey: &apikey.Key{ID: 5, Name: "ci", Enabled: false}}
	body := `{"name":"ci","label":"bot","enabled":false,"rpm_limit":60,"allowed_models":["gpt-4o"," "," claude-3 "]}`
	rec := do(newKeyEngine(store), http.MethodPatch, "/admin/api/keys/5", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if store.lastUpdated != 5 {
		t.Errorf("updated id = %d, want 5", store.lastUpdated)
	}
	if store.lastUpdate.Enabled {
		t.Error("enabled=false not forwarded")
	}
	if store.lastUpdate.RPMLimit == nil || *store.lastUpdate.RPMLimit != 60 {
		t.Errorf("rpm_limit not forwarded: %+v", store.lastUpdate.RPMLimit)
	}
	// Blank entries dropped, surviving entries trimmed.
	got := store.lastUpdate.AllowedModels
	if len(got) != 2 || got[0] != "gpt-4o" || got[1] != "claude-3" {
		t.Errorf("allowed_models = %v, want [gpt-4o claude-3]", got)
	}
}

func TestUpdateKeyNotFound(t *testing.T) {
	store := &fakeKeyStore{updateErr: repository.ErrApiKeyNotFound}
	rec := do(newKeyEngine(store), http.MethodPatch, "/admin/api/keys/99", `{"name":"a"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestUpdateKeyBadID(t *testing.T) {
	rec := do(newKeyEngine(&fakeKeyStore{}), http.MethodPatch, "/admin/api/keys/xyz", `{"name":"a"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestDeleteKey(t *testing.T) {
	store := &fakeKeyStore{}
	rec := do(newKeyEngine(store), http.MethodDelete, "/admin/api/keys/7", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}
	if store.lastDeleted != 7 {
		t.Errorf("deleted id = %d, want 7", store.lastDeleted)
	}
}

func TestDeleteKeyNotFound(t *testing.T) {
	store := &fakeKeyStore{deleteErr: repository.ErrApiKeyNotFound}
	rec := do(newKeyEngine(store), http.MethodDelete, "/admin/api/keys/7", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
