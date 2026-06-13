package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	handler "github.com/jami1024/omnihub/internal/handler/admin"
	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/provider"
)

// fakeGroupLister implements the groupLister interface the import
// handler needs.
type fakeGroupLister struct {
	groups []repository.ProviderGroup
	err    error
}

func (f *fakeGroupLister) List(context.Context) ([]repository.ProviderGroup, error) {
	return f.groups, f.err
}

func newDataEngine(store *fakeAccountStore, groups *fakeGroupLister) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/admin/api/accounts/export", handler.ExportAccountsHandler(store))
	r.POST("/admin/api/accounts/import", handler.ImportAccountsHandler(store, groups))
	return r
}

func TestExportIncludesCleartextCredentials(t *testing.T) {
	store := &fakeAccountStore{
		list: []*provider.Account{
			{ID: 1, Name: "anthropic-1", Provider: "anthropic",
				Credentials: map[string]string{"api_key": "sk-secret"}, GroupName: "prod"},
			{ID: 2, Name: "codex-1", Provider: "openai-codex", AuthType: "imported_oauth",
				Credentials: map[string]string{"access_token": "at"}},
		},
		enabled: []bool{true, false},
	}
	r := newDataEngine(store, &fakeGroupLister{})

	rec := do(r, http.MethodGet, "/admin/api/accounts/export", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd == "" {
		t.Fatal("export should be an attachment")
	}
	var bundle struct {
		Type     string `json:"type"`
		Version  int    `json:"version"`
		Accounts []struct {
			Name        string            `json:"name"`
			Enabled     bool              `json:"enabled"`
			GroupName   string            `json:"group_name"`
			Credentials map[string]string `json:"credentials"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &bundle); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if bundle.Type != "omnihub-accounts" || bundle.Version != 1 || len(bundle.Accounts) != 2 {
		t.Fatalf("bundle shape: %+v", bundle)
	}
	if bundle.Accounts[0].Credentials["api_key"] != "sk-secret" {
		t.Fatal("export must carry cleartext credentials")
	}
	if bundle.Accounts[0].Enabled != true || bundle.Accounts[1].Enabled != false {
		t.Fatal("enabled flag not carried per account")
	}
	if bundle.Accounts[0].GroupName != "prod" {
		t.Fatal("group exported by name")
	}
}

func TestExportIDFilter(t *testing.T) {
	store := &fakeAccountStore{
		list: []*provider.Account{
			{ID: 1, Name: "a", Provider: "anthropic"},
			{ID: 2, Name: "b", Provider: "anthropic"},
			{ID: 3, Name: "c", Provider: "anthropic"},
		},
		enabled: []bool{true, true, true},
	}
	r := newDataEngine(store, &fakeGroupLister{})
	rec := do(r, http.MethodGet, "/admin/api/accounts/export?ids=1,3", "")
	var bundle struct {
		Accounts []struct {
			Name string `json:"name"`
		} `json:"accounts"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &bundle)
	if len(bundle.Accounts) != 2 || bundle.Accounts[0].Name != "a" || bundle.Accounts[1].Name != "c" {
		t.Fatalf("id filter wrong: %+v", bundle.Accounts)
	}
}

func TestImportCreatesAndResolvesGroup(t *testing.T) {
	store := &fakeAccountStore{insertID: 10}
	groups := &fakeGroupLister{groups: []repository.ProviderGroup{{ID: 7, Name: "prod"}}}
	r := newDataEngine(store, groups)

	body := `{"accounts":[
		{"name":"a","provider":"anthropic","credentials":{"api_key":"k"},"group_name":"prod","auth_type":"api_key","circuit_open_duration_ms":5000}
	]}`
	rec := do(r, http.MethodPost, "/admin/api/accounts/import", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Created int `json:"created"`
		Skipped int `json:"skipped"`
		Failed  int `json:"failed"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Created != 1 || res.Failed != 0 {
		t.Fatalf("result: %+v", res)
	}
	if store.lastInsert.GroupID == nil || *store.lastInsert.GroupID != 7 {
		t.Fatalf("group not resolved by name: %+v", store.lastInsert.GroupID)
	}
	if store.lastInsert.CircuitOpenDuration == nil || store.lastInsert.CircuitOpenDuration.Milliseconds() != 5000 {
		t.Fatalf("circuit duration not restored from ms: %+v", store.lastInsert.CircuitOpenDuration)
	}
}

func TestImportConflictPolicy(t *testing.T) {
	body := `{"accounts":[{"name":"dup","provider":"anthropic","credentials":{"api_key":"k"},"auth_type":"api_key"}],"on_conflict":%q}`

	// skip (default): a name clash is counted as skipped, not failed.
	skipStore := &fakeAccountStore{insertErr: repository.ErrAccountNameTaken}
	r := newDataEngine(skipStore, &fakeGroupLister{})
	rec := do(r, http.MethodPost, "/admin/api/accounts/import", `{"accounts":[{"name":"dup","provider":"anthropic","credentials":{"api_key":"k"},"auth_type":"api_key"}]}`)
	var res struct{ Created, Skipped, Failed int }
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Skipped != 1 || res.Failed != 0 || res.Created != 0 {
		t.Fatalf("skip policy: %+v", res)
	}

	// fail: a name clash is counted as failed.
	failStore := &fakeAccountStore{insertErr: repository.ErrAccountNameTaken}
	r2 := newDataEngine(failStore, &fakeGroupLister{})
	rec2 := do(r2, http.MethodPost, "/admin/api/accounts/import", `{"accounts":[{"name":"dup","provider":"anthropic","credentials":{"api_key":"k"},"auth_type":"api_key"}],"on_conflict":"fail"}`)
	var res2 struct {
		Created, Skipped, Failed int
		Errors                   []struct{ Message string }
	}
	_ = json.Unmarshal(rec2.Body.Bytes(), &res2)
	if res2.Failed != 1 || res2.Skipped != 0 {
		t.Fatalf("fail policy: %+v", res2)
	}
	_ = body
}

func TestImportMissingGroupWarns(t *testing.T) {
	store := &fakeAccountStore{insertID: 1}
	r := newDataEngine(store, &fakeGroupLister{}) // no groups
	body := `{"accounts":[{"name":"a","provider":"anthropic","credentials":{"api_key":"k"},"group_name":"ghost","auth_type":"api_key"}]}`
	rec := do(r, http.MethodPost, "/admin/api/accounts/import", body)
	var res struct {
		Created int `json:"created"`
		Errors  []struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Created != 1 {
		t.Fatalf("missing group must not fail the account: %+v", res)
	}
	if store.lastInsert.GroupID != nil {
		t.Fatal("unknown group must yield ungrouped account")
	}
	if len(res.Errors) != 1 || res.Errors[0].Kind != "warning" {
		t.Fatalf("expected a warning, got %+v", res.Errors)
	}
}

func TestImportValidationAndEmpty(t *testing.T) {
	r := newDataEngine(&fakeAccountStore{}, &fakeGroupLister{})

	// Empty bundle.
	if rec := do(r, http.MethodPost, "/admin/api/accounts/import", `{"accounts":[]}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty bundle should be 400, got %d", rec.Code)
	}

	// Missing provider + bad auth_type → both failed, batch continues.
	store := &fakeAccountStore{insertID: 1}
	r2 := newDataEngine(store, &fakeGroupLister{})
	body := `{"accounts":[
		{"name":"noprov","provider":"","credentials":{},"auth_type":"api_key"},
		{"name":"badauth","provider":"anthropic","credentials":{},"auth_type":"bogus"},
		{"name":"ok","provider":"anthropic","credentials":{"api_key":"k"},"auth_type":"api_key"}
	]}`
	rec := do(r2, http.MethodPost, "/admin/api/accounts/import", body)
	var res struct{ Created, Failed int }
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Created != 1 || res.Failed != 2 {
		t.Fatalf("expected 1 created 2 failed, got %+v", res)
	}
}
