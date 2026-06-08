package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	handler "github.com/jami1024/omnihub/internal/handler/admin"
	"github.com/jami1024/omnihub/internal/repository"
)

type fakeAnnouncementStore struct {
	list      []repository.Announcement
	createID  int64
	createErr error
	updateErr error
	deleteErr error
	lastID    int64
	last      repository.Announcement
}

func (f *fakeAnnouncementStore) List(context.Context) ([]repository.Announcement, error) {
	return f.list, nil
}

func (f *fakeAnnouncementStore) Create(_ context.Context, a repository.Announcement) (int64, error) {
	f.last = a
	return f.createID, f.createErr
}

func (f *fakeAnnouncementStore) Update(_ context.Context, id int64, a repository.Announcement) error {
	f.lastID, f.last = id, a
	return f.updateErr
}

func (f *fakeAnnouncementStore) Delete(_ context.Context, id int64) error {
	f.lastID = id
	return f.deleteErr
}

func newAnnouncementEngine(store *fakeAnnouncementStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/admin/api/announcements", handler.ListAnnouncementsHandler(store))
	r.POST("/admin/api/announcements", handler.CreateAnnouncementHandler(store))
	r.PATCH("/admin/api/announcements/:id", handler.UpdateAnnouncementHandler(store))
	r.DELETE("/admin/api/announcements/:id", handler.DeleteAnnouncementHandler(store))
	return r
}

func TestListAnnouncementsReturnsRows(t *testing.T) {
	store := &fakeAnnouncementStore{list: []repository.Announcement{{
		ID: 1, Title: "Maintenance", Body: "Tonight", Kind: "maintenance", Status: "published", Placement: "banner",
	}}}
	rec := do(newAnnouncementEngine(store), http.MethodGet, "/admin/api/announcements", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Announcements []repository.Announcement `json:"announcements"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Announcements) != 1 || resp.Announcements[0].Title != "Maintenance" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestCreateAnnouncementRejectsEmptyTitle(t *testing.T) {
	rec := do(newAnnouncementEngine(&fakeAnnouncementStore{}), http.MethodPost,
		"/admin/api/announcements", `{"title":"","body":"Body","kind":"info","status":"draft","placement":"portal_home"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestCreateAnnouncementForwardsBody(t *testing.T) {
	store := &fakeAnnouncementStore{createID: 9}
	rec := do(newAnnouncementEngine(store), http.MethodPost,
		"/admin/api/announcements", `{"title":"Price update","body":"New pricing","kind":"pricing","status":"published","placement":"portal_home","priority":3,"dismissible":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	if store.last.Title != "Price update" || store.last.Kind != "pricing" || store.last.Priority != 3 || !store.last.Dismissible {
		t.Fatalf("announcement not forwarded: %+v", store.last)
	}
}

func TestUpdateAnnouncementNotFound(t *testing.T) {
	store := &fakeAnnouncementStore{updateErr: repository.ErrAnnouncementNotFound}
	rec := do(newAnnouncementEngine(store), http.MethodPatch,
		"/admin/api/announcements/99", `{"title":"T","body":"B","kind":"info","status":"draft","placement":"portal_home"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestDeleteAnnouncement(t *testing.T) {
	store := &fakeAnnouncementStore{}
	rec := do(newAnnouncementEngine(store), http.MethodDelete, "/admin/api/announcements/5", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body %s)", rec.Code, rec.Body.String())
	}
	if store.lastID != 5 {
		t.Fatalf("deleted id = %d, want 5", store.lastID)
	}
}
