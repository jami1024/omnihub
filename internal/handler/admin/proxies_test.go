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

// fakeProxyStore implements the (unexported) proxyStore interface.
type fakeProxyStore struct {
	inserted []repository.ProxyParams
	taken    map[string]bool
}

func (f *fakeProxyStore) ListAll(context.Context) ([]*provider.Proxy, error) { return nil, nil }
func (f *fakeProxyStore) GetByID(context.Context, int64) (*provider.Proxy, error) {
	return nil, repository.ErrProxyNotFound
}
func (f *fakeProxyStore) Insert(_ context.Context, p repository.ProxyParams) (int64, error) {
	if f.taken != nil && f.taken[p.Name] {
		return 0, repository.ErrProxyNameTaken
	}
	f.inserted = append(f.inserted, p)
	return int64(len(f.inserted)), nil
}
func (f *fakeProxyStore) Update(context.Context, int64, repository.ProxyParams) error { return nil }
func (f *fakeProxyStore) Delete(context.Context, int64) error                         { return nil }

func newProxyImportEngine(store *fakeProxyStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/admin/api/proxies/import", handler.ImportProxiesHandler(store))
	return r
}

func TestImportProxiesParsesAllForms(t *testing.T) {
	store := &fakeProxyStore{}
	r := newProxyImportEngine(store)
	body := `{"proxies":[
		"socks5://user:pass@1.2.3.4:1080",
		"5.6.7.8:8080:u2:p2",
		"9.9.9.9:3128"
	],"default_protocol":"http"}`
	rec := do(r, http.MethodPost, "/admin/api/proxies/import", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body %s", rec.Code, rec.Body.String())
	}
	var res struct{ Created, Skipped, Failed int }
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Created != 3 || res.Failed != 0 {
		t.Fatalf("result: %+v", res)
	}
	if len(store.inserted) != 3 {
		t.Fatalf("inserted %d", len(store.inserted))
	}
	// URL form: socks5 with auth.
	p0 := store.inserted[0]
	if p0.Protocol != "socks5" || p0.Host != "1.2.3.4" || p0.Port != 1080 ||
		p0.Username != "user" || p0.Password != "pass" || p0.Name != "1.2.3.4:1080" {
		t.Fatalf("url form parsed wrong: %+v", p0)
	}
	// host:port:user:pass with default protocol http.
	p1 := store.inserted[1]
	if p1.Protocol != "http" || p1.Host != "5.6.7.8" || p1.Port != 8080 ||
		p1.Username != "u2" || p1.Password != "p2" {
		t.Fatalf("host:port:user:pass parsed wrong: %+v", p1)
	}
	// bare host:port, no auth.
	p2 := store.inserted[2]
	if p2.Protocol != "http" || p2.Host != "9.9.9.9" || p2.Port != 3128 || p2.Username != "" {
		t.Fatalf("host:port parsed wrong: %+v", p2)
	}
}

func TestImportProxiesSkipAndFail(t *testing.T) {
	store := &fakeProxyStore{taken: map[string]bool{"1.1.1.1:8080": true}}
	r := newProxyImportEngine(store)
	body := `{"proxies":[
		"1.1.1.1:8080",
		"2.2.2.2:notaport",
		"hostonly",
		"3.3.3.3:9090"
	]}`
	rec := do(r, http.MethodPost, "/admin/api/proxies/import", body)
	var res struct {
		Created, Skipped, Failed int
		Errors                   []struct{ Line, Message string }
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	// taken → skipped; bad port + hostonly → failed; last → created.
	if res.Created != 1 || res.Skipped != 1 || res.Failed != 2 {
		t.Fatalf("result: %+v", res)
	}
	if len(res.Errors) != 2 {
		t.Fatalf("expected 2 per-line errors, got %+v", res.Errors)
	}
}

func TestImportProxiesEmpty(t *testing.T) {
	r := newProxyImportEngine(&fakeProxyStore{})
	rec := do(r, http.MethodPost, "/admin/api/proxies/import", `{"proxies":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty import should be 400, got %d", rec.Code)
	}
}
