package portal_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	portal "github.com/jami1024/omnihub/internal/handler/portal"
	"github.com/jami1024/omnihub/internal/repository"
)

type fakePortalAnnouncementStore struct {
	rows          []repository.Announcement
	lastPlacement string
}

func (f *fakePortalAnnouncementStore) ListActive(_ context.Context, placement string, _ time.Time) ([]repository.Announcement, error) {
	f.lastPlacement = placement
	return f.rows, nil
}

func TestPortalAnnouncementsReturnsActiveRows(t *testing.T) {
	store := &fakePortalAnnouncementStore{rows: []repository.Announcement{{
		ID: 1, Title: "Welcome", Body: "Hello", Kind: "info", Status: "published", Placement: "portal_home",
	}}}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/announcements", portal.AnnouncementsHandler(store))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/announcements?placement=portal_home", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if store.lastPlacement != "portal_home" {
		t.Fatalf("placement = %q, want portal_home", store.lastPlacement)
	}
	var resp struct {
		Announcements []repository.Announcement `json:"announcements"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Announcements) != 1 || resp.Announcements[0].Title != "Welcome" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestPortalAnnouncementsRejectsInvalidPlacement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/announcements", portal.AnnouncementsHandler(&fakePortalAnnouncementStore{}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/announcements?placement=admin", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
	}
}
