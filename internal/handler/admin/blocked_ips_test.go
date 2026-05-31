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
)

type fakeBlockedIPStore struct {
	list       []repository.BlockedIPRecord
	listErr    error
	insertErr  error
	updateErr  error
	deleteErr  error
	lastIP     string
	lastParams repository.BlockedIPParams
	lastBy     string
}

func (f *fakeBlockedIPStore) ListRecords(context.Context) ([]repository.BlockedIPRecord, error) {
	return f.list, f.listErr
}
func (f *fakeBlockedIPStore) Insert(_ context.Context, ip string, p repository.BlockedIPParams, by string) error {
	f.lastIP, f.lastParams, f.lastBy = ip, p, by
	return f.insertErr
}
func (f *fakeBlockedIPStore) Update(_ context.Context, ip string, p repository.BlockedIPParams) error {
	f.lastIP, f.lastParams = ip, p
	return f.updateErr
}
func (f *fakeBlockedIPStore) Delete(_ context.Context, ip string) error {
	f.lastIP = ip
	return f.deleteErr
}

func newBlockedIPEngine(store *fakeBlockedIPStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/admin/api/blocked-ips", handler.ListBlockedIPsHandler(store))
	r.POST("/admin/api/blocked-ips", handler.CreateBlockedIPHandler(store))
	r.PATCH("/admin/api/blocked-ips/:ip", handler.UpdateBlockedIPHandler(store))
	r.DELETE("/admin/api/blocked-ips/:ip", handler.DeleteBlockedIPHandler(store))
	return r
}

func TestListBlockedIPsDerivesBlockedFlag(t *testing.T) {
	rpm := 60
	store := &fakeBlockedIPStore{
		list: []repository.BlockedIPRecord{
			{IP: "1.2.3.4"},                 // all limits nil → hard block
			{IP: "5.6.7.8", RPMLimit: &rpm}, // capped, not blocked
		},
	}
	rec := do(newBlockedIPEngine(store), http.MethodGet, "/admin/api/blocked-ips", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		BlockedIPs []struct {
			IP      string `json:"ip"`
			Blocked bool   `json:"blocked"`
		} `json:"blocked_ips"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.BlockedIPs) != 2 {
		t.Fatalf("got %d rows, want 2", len(resp.BlockedIPs))
	}
	if !resp.BlockedIPs[0].Blocked {
		t.Error("1.2.3.4 with no limits should be a hard block")
	}
	if resp.BlockedIPs[1].Blocked {
		t.Error("5.6.7.8 with an rpm cap should not be a hard block")
	}
}

func TestCreateBlockedIPValidatesIP(t *testing.T) {
	rec := do(newBlockedIPEngine(&fakeBlockedIPStore{}), http.MethodPost,
		"/admin/api/blocked-ips", `{"ip":"not-an-ip"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestCreateBlockedIPHardBlock(t *testing.T) {
	store := &fakeBlockedIPStore{}
	rec := do(newBlockedIPEngine(store), http.MethodPost,
		"/admin/api/blocked-ips", `{"ip":"2001:db8::1","reason":"abuse"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if store.lastIP != "2001:db8::1" {
		t.Errorf("stored ip = %q, want 2001:db8::1", store.lastIP)
	}
	if store.lastParams.RPMLimit != nil || store.lastParams.TPMLimit != nil || store.lastParams.ConcurrentLimit != nil {
		t.Errorf("hard block should have nil limits, got %+v", store.lastParams)
	}
	if store.lastBy == "" {
		t.Error("created_by (admin actor) should be forwarded")
	}
}

func TestCreateBlockedIPRejectsNonPositiveLimit(t *testing.T) {
	rec := do(newBlockedIPEngine(&fakeBlockedIPStore{}), http.MethodPost,
		"/admin/api/blocked-ips", `{"ip":"1.2.3.4","rpm_limit":0}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestCreateBlockedIPConflict(t *testing.T) {
	store := &fakeBlockedIPStore{insertErr: repository.ErrBlockedIPExists}
	rec := do(newBlockedIPEngine(store), http.MethodPost,
		"/admin/api/blocked-ips", `{"ip":"1.2.3.4"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "already_exists") {
		t.Errorf("expected already_exists code, got %s", rec.Body.String())
	}
}

func TestUpdateBlockedIPNotFound(t *testing.T) {
	store := &fakeBlockedIPStore{updateErr: repository.ErrBlockedIPNotFound}
	rec := do(newBlockedIPEngine(store), http.MethodPatch,
		"/admin/api/blocked-ips/1.2.3.4", `{"reason":"x"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestUpdateBlockedIPBadIP(t *testing.T) {
	rec := do(newBlockedIPEngine(&fakeBlockedIPStore{}), http.MethodPatch,
		"/admin/api/blocked-ips/not-an-ip", `{"reason":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestDeleteBlockedIP(t *testing.T) {
	store := &fakeBlockedIPStore{}
	rec := do(newBlockedIPEngine(store), http.MethodDelete,
		"/admin/api/blocked-ips/1.2.3.4", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}
	if store.lastIP != "1.2.3.4" {
		t.Errorf("deleted ip = %q, want 1.2.3.4", store.lastIP)
	}
}

func TestDeleteBlockedIPNotFound(t *testing.T) {
	store := &fakeBlockedIPStore{deleteErr: repository.ErrBlockedIPNotFound}
	rec := do(newBlockedIPEngine(store), http.MethodDelete,
		"/admin/api/blocked-ips/1.2.3.4", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
