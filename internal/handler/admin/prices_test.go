package admin_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	handler "github.com/jami1024/omnihub/internal/handler/admin"
	"github.com/jami1024/omnihub/internal/repository"
)

type fakePriceStore struct {
	list       []repository.ModelPrice
	get        repository.ModelPrice
	insertID   int64
	insertErr  error
	updateErr  error
	deleteErr  error
	lastModel  string
	lastParams repository.ModelPriceParams
	lastID     int64
}

func (f *fakePriceStore) ListAll(context.Context) ([]repository.ModelPrice, error) {
	return f.list, nil
}
func (f *fakePriceStore) GetByID(_ context.Context, id int64) (repository.ModelPrice, error) {
	f.get.ID = id
	return f.get, nil
}
func (f *fakePriceStore) InsertManual(_ context.Context, model string, p repository.ModelPriceParams) (int64, error) {
	f.lastModel, f.lastParams = model, p
	return f.insertID, f.insertErr
}
func (f *fakePriceStore) UpdateManual(_ context.Context, id int64, p repository.ModelPriceParams) error {
	f.lastID, f.lastParams = id, p
	return f.updateErr
}
func (f *fakePriceStore) DeleteByID(_ context.Context, id int64) error {
	f.lastID = id
	return f.deleteErr
}

type fakeSyncer struct {
	res    repository.UpsertResult
	err    error
	called bool
}

func (f *fakeSyncer) SyncFromLiteLLM(context.Context, string) (repository.UpsertResult, error) {
	f.called = true
	return f.res, f.err
}

func TestListPricesIncludesSource(t *testing.T) {
	store := &fakePriceStore{list: []repository.ModelPrice{
		{ID: 1, Model: "gpt-5.2", InputCostPerToken: 1.25e-6, Source: "litellm"},
		{ID: 2, Model: "custom", Source: "manual"},
	}}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/admin/api/prices", handler.ListPricesHandler(store))
	rec := do(r, http.MethodGet, "/admin/api/prices", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Prices []struct {
			Model  string `json:"model"`
			Source string `json:"source"`
		} `json:"prices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Prices) != 2 || resp.Prices[0].Source != "litellm" || resp.Prices[1].Source != "manual" {
		t.Errorf("unexpected: %+v", resp.Prices)
	}
}

func TestCreatePriceRequiresModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/admin/api/prices", handler.CreatePriceHandler(&fakePriceStore{}))
	rec := do(r, http.MethodPost, "/admin/api/prices", `{"input_cost_per_token":1e-6}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400 (%s)", rec.Code, rec.Body.String())
	}
}

func TestCreatePriceRejectsNegative(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/admin/api/prices", handler.CreatePriceHandler(&fakePriceStore{}))
	rec := do(r, http.MethodPost, "/admin/api/prices", `{"model":"x","output_cost_per_token":-1}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400 (%s)", rec.Code, rec.Body.String())
	}
}

func TestCreatePriceConflict(t *testing.T) {
	store := &fakePriceStore{insertErr: repository.ErrModelPriceExists}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/admin/api/prices", handler.CreatePriceHandler(store))
	rec := do(r, http.MethodPost, "/admin/api/prices", `{"model":"gpt-5.2","input_cost_per_token":1e-6}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d want 409 (%s)", rec.Code, rec.Body.String())
	}
}

func TestCreatePriceForwardsCosts(t *testing.T) {
	store := &fakePriceStore{insertID: 7, get: repository.ModelPrice{Model: "gpt-5.2", Source: "manual"}}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/admin/api/prices", handler.CreatePriceHandler(store))
	rec := do(r, http.MethodPost, "/admin/api/prices",
		`{"model":"gpt-5.2","input_cost_per_token":1.25e-6,"output_cost_per_token":1e-5}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d want 201 (%s)", rec.Code, rec.Body.String())
	}
	if store.lastModel != "gpt-5.2" || store.lastParams.InputCostPerToken != 1.25e-6 || store.lastParams.OutputCostPerToken != 1e-5 {
		t.Errorf("costs not forwarded: model=%q %+v", store.lastModel, store.lastParams)
	}
}

func TestUpdatePriceNotFound(t *testing.T) {
	store := &fakePriceStore{updateErr: repository.ErrModelPriceNotFound}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PATCH("/admin/api/prices/:id", handler.UpdatePriceHandler(store))
	rec := do(r, http.MethodPatch, "/admin/api/prices/99", `{"input_cost_per_token":1e-6}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d want 404 (%s)", rec.Code, rec.Body.String())
	}
}

func TestDeletePrice(t *testing.T) {
	store := &fakePriceStore{}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.DELETE("/admin/api/prices/:id", handler.DeletePriceHandler(store))
	rec := do(r, http.MethodDelete, "/admin/api/prices/5", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d want 204 (%s)", rec.Code, rec.Body.String())
	}
	if store.lastID != 5 {
		t.Errorf("deleted id=%d want 5", store.lastID)
	}
}

func TestSyncPricesSuccess(t *testing.T) {
	syncer := &fakeSyncer{res: repository.UpsertResult{Added: 10, Updated: 3, Skipped: 1}}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/admin/api/prices/sync", handler.SyncPricesHandler(syncer))
	rec := do(r, http.MethodPost, "/admin/api/prices/sync", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200 (%s)", rec.Code, rec.Body.String())
	}
	if !syncer.called {
		t.Error("syncer not called")
	}
	var resp struct{ Added, Updated, Skipped int }
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Added != 10 || resp.Updated != 3 || resp.Skipped != 1 {
		t.Errorf("counts not forwarded: %+v", resp)
	}
}

func TestSyncPricesFailure(t *testing.T) {
	syncer := &fakeSyncer{err: errors.New("network down")}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/admin/api/prices/sync", handler.SyncPricesHandler(syncer))
	rec := do(r, http.MethodPost, "/admin/api/prices/sync", "")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d want 502 (%s)", rec.Code, rec.Body.String())
	}
}

func TestSyncPricesUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/admin/api/prices/sync", handler.SyncPricesHandler(nil))
	rec := do(r, http.MethodPost, "/admin/api/prices/sync", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want 503 (%s)", rec.Code, rec.Body.String())
	}
}
