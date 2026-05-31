package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	handler "github.com/jami1024/omnihub/internal/handler/admin"
	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/provider"
)

// fakeAccountStore implements the (unexported) accountStore interface
// the account handlers depend on. Each method's behaviour is driven by
// the struct fields so a test can inject a specific outcome.
type fakeAccountStore struct {
	list        []*provider.Account
	enabled     []bool
	listErr     error
	getAcct     *provider.Account
	getEnabled  bool
	getErr      error
	insertID    int64
	insertErr   error
	updateErr   error
	deleteErr   error
	lastInsert  repository.InsertParams
	lastUpdate  repository.UpdateParams
	lastUpdated int64
	lastDeleted int64
}

func (f *fakeAccountStore) ListAll(context.Context) ([]*provider.Account, []bool, error) {
	return f.list, f.enabled, f.listErr
}

func (f *fakeAccountStore) GetByID(_ context.Context, _ int64) (*provider.Account, bool, error) {
	return f.getAcct, f.getEnabled, f.getErr
}

func (f *fakeAccountStore) Insert(_ context.Context, p repository.InsertParams) (int64, error) {
	f.lastInsert = p
	return f.insertID, f.insertErr
}

func (f *fakeAccountStore) Update(_ context.Context, id int64, p repository.UpdateParams) error {
	f.lastUpdate = p
	f.lastUpdated = id
	return f.updateErr
}

func (f *fakeAccountStore) DeleteByID(_ context.Context, id int64) error {
	f.lastDeleted = id
	return f.deleteErr
}

func newAccountEngine(store *fakeAccountStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/admin/api/accounts", handler.ListAccountsHandler(store))
	r.POST("/admin/api/accounts", handler.CreateAccountHandler(store))
	r.PATCH("/admin/api/accounts/:id", handler.UpdateAccountHandler(store))
	r.DELETE("/admin/api/accounts/:id", handler.DeleteAccountHandler(store))
	return r
}

func do(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	var rdr *strings.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	} else {
		rdr = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	return rec
}

func TestListAccountsRedactsCredentials(t *testing.T) {
	store := &fakeAccountStore{
		list: []*provider.Account{
			{ID: 1, Name: "anthropic-1", Provider: "anthropic",
				Credentials: map[string]string{"api_key": "sk-super-secret", "workspace_id": "ws-1"}},
		},
		enabled: []bool{true},
	}
	rec := do(newAccountEngine(store), http.MethodGet, "/admin/api/accounts", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	// The secret VALUE must never appear in the response.
	if strings.Contains(rec.Body.String(), "sk-super-secret") {
		t.Fatalf("credential value leaked into list response: %s", rec.Body.String())
	}
	var resp struct {
		Accounts []struct {
			ID             int64    `json:"id"`
			CredentialKeys []string `json:"credential_keys"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Accounts) != 1 {
		t.Fatalf("accounts = %d, want 1", len(resp.Accounts))
	}
	got := resp.Accounts[0].CredentialKeys
	if len(got) != 2 || got[0] != "api_key" || got[1] != "workspace_id" {
		t.Errorf("credential_keys = %v, want sorted [api_key workspace_id]", got)
	}
}

func TestCreateAccountAppliesDefaults(t *testing.T) {
	store := &fakeAccountStore{
		insertID:   42,
		getAcct:    &provider.Account{ID: 42, Name: "openai-1", Provider: "openai", Weight: 100, CostMultiplier: 1.0},
		getEnabled: true,
	}
	body := `{"name":"openai-1","provider":"openai","credentials":{"api_key":"sk-x"}}`
	rec := do(newAccountEngine(store), http.MethodPost, "/admin/api/accounts", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if store.lastInsert.Weight != 100 || store.lastInsert.CostMultiplier != 1.0 || !store.lastInsert.Enabled {
		t.Errorf("defaults not applied: %+v", store.lastInsert)
	}
	if store.lastInsert.Credentials["api_key"] != "sk-x" {
		t.Errorf("credentials not forwarded: %+v", store.lastInsert.Credentials)
	}
}

func TestCreateAccountRequiresFields(t *testing.T) {
	cases := map[string]string{
		"missing name":        `{"provider":"openai","credentials":{"api_key":"x"}}`,
		"missing provider":    `{"name":"a","credentials":{"api_key":"x"}}`,
		"missing credentials": `{"name":"a","provider":"openai"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := do(newAccountEngine(&fakeAccountStore{}), http.MethodPost, "/admin/api/accounts", body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCreateAccountNameTaken(t *testing.T) {
	store := &fakeAccountStore{insertErr: repository.ErrAccountNameTaken}
	body := `{"name":"dup","provider":"openai","credentials":{"api_key":"x"}}`
	rec := do(newAccountEngine(store), http.MethodPost, "/admin/api/accounts", body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "name_taken") {
		t.Errorf("expected name_taken code, got %s", rec.Body.String())
	}
}

func TestUpdateAccountKeepsCredentialsWhenOmitted(t *testing.T) {
	store := &fakeAccountStore{
		getAcct:    &provider.Account{ID: 5, Name: "a", Provider: "openai"},
		getEnabled: true,
	}
	body := `{"name":"a","provider":"openai","weight":50,"priority":1,"cost_multiplier":2.0,"enabled":false}`
	rec := do(newAccountEngine(store), http.MethodPatch, "/admin/api/accounts/5", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if store.lastUpdated != 5 {
		t.Errorf("updated id = %d, want 5", store.lastUpdated)
	}
	if store.lastUpdate.Credentials != nil {
		t.Errorf("omitted credentials should be nil (keep), got %+v", store.lastUpdate.Credentials)
	}
	if store.lastUpdate.Weight != 50 || store.lastUpdate.Priority != 1 || store.lastUpdate.Enabled {
		t.Errorf("fields not forwarded: %+v", store.lastUpdate)
	}
}

func TestUpdateAccountRejectsEmptyCredentials(t *testing.T) {
	body := `{"name":"a","provider":"openai","credentials":{}}`
	rec := do(newAccountEngine(&fakeAccountStore{}), http.MethodPatch, "/admin/api/accounts/5", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestUpdateAccountNotFound(t *testing.T) {
	store := &fakeAccountStore{updateErr: repository.ErrAccountNotFound}
	body := `{"name":"a","provider":"openai"}`
	rec := do(newAccountEngine(store), http.MethodPatch, "/admin/api/accounts/99", body)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestUpdateAccountBadID(t *testing.T) {
	rec := do(newAccountEngine(&fakeAccountStore{}), http.MethodPatch, "/admin/api/accounts/abc",
		`{"name":"a","provider":"openai"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestDeleteAccount(t *testing.T) {
	store := &fakeAccountStore{}
	rec := do(newAccountEngine(store), http.MethodDelete, "/admin/api/accounts/7", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}
	if store.lastDeleted != 7 {
		t.Errorf("deleted id = %d, want 7", store.lastDeleted)
	}
}

func TestDeleteAccountNotFound(t *testing.T) {
	store := &fakeAccountStore{deleteErr: repository.ErrAccountNotFound}
	rec := do(newAccountEngine(store), http.MethodDelete, "/admin/api/accounts/7", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
