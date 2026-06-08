package public_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	publichandler "github.com/jami1024/omnihub/internal/handler/public"
	"github.com/jami1024/omnihub/internal/repository"
)

type fakePlanStore struct {
	plans []repository.Plan
	err   error
}

func (f *fakePlanStore) ListEnabledPlans(context.Context) ([]repository.Plan, error) {
	return f.plans, f.err
}

type fakePriceStore struct {
	prices []repository.ModelPrice
	err    error
}

func (f *fakePriceStore) ListAll(context.Context) ([]repository.ModelPrice, error) {
	return f.prices, f.err
}

func newPricingEngine(plans *fakePlanStore, prices *fakePriceStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/public/api/pricing", publichandler.PricingHandler(plans, prices))
	return r
}

func TestPricingHandlerReturnsEnabledPlansAndCommonOfficialPrices(t *testing.T) {
	now := time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)
	plans := &fakePlanStore{plans: []repository.Plan{
		{ID: 1, Name: "Starter", PriceUSD: 9, IncludedCreditUSD: 10, PriceRatio: 0.95, Enabled: true, SortOrder: 1, CreatedAt: now, UpdatedAt: now},
		{ID: 2, Name: "Team", PriceUSD: 49, IncludedCreditUSD: 60, PriceRatio: 0.9, Enabled: true, SortOrder: 2, CreatedAt: now, UpdatedAt: now},
	}}
	prices := &fakePriceStore{prices: []repository.ModelPrice{
		{ID: 10, Model: "zz-custom", InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, Source: repository.PriceSourceManual, UpdatedAt: now},
		{ID: 11, Model: "gpt-4o-mini", InputCostPerToken: 0.15e-6, OutputCostPerToken: 0.6e-6, Source: repository.PriceSourceLiteLLM, UpdatedAt: now},
		{ID: 12, Model: "claude-sonnet-4-5", InputCostPerToken: 3e-6, OutputCostPerToken: 15e-6, Source: repository.PriceSourceLiteLLM, UpdatedAt: now},
		{ID: 13, Model: "gpt-4o", Source: repository.PriceSourceLiteLLM, UpdatedAt: now},
	}}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/public/api/pricing", nil)
	newPricingEngine(plans, prices).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"plans":[`) || !strings.Contains(body, `"prices":[`) {
		t.Fatalf("response missing plans/prices: %s", body)
	}
	if !strings.Contains(body, `"name":"Team"`) || !strings.Contains(body, `"price_ratio":0.9`) {
		t.Fatalf("response missing plan fields: %s", body)
	}
	if !strings.Contains(body, `"model":"claude-sonnet-4-5"`) || !strings.Contains(body, `"model":"gpt-4o-mini"`) {
		t.Fatalf("response missing common model prices: %s", body)
	}
	if strings.Contains(body, "zz-custom") {
		t.Fatalf("non-common model should not be exposed when common prices exist: %s", body)
	}
	if strings.Contains(body, `"model":"gpt-4o"`) {
		t.Fatalf("zero-cost price row should not be exposed: %s", body)
	}
}

func TestPricingHandlerUsesFallbackPricesWhenNoCommonModelsExist(t *testing.T) {
	now := time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)
	prices := &fakePriceStore{prices: []repository.ModelPrice{
		{ID: 1, Model: "b-model", InputCostPerToken: 2e-6, OutputCostPerToken: 3e-6, Source: repository.PriceSourceLiteLLM, UpdatedAt: now},
		{ID: 2, Model: "a-model", InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, Source: repository.PriceSourceLiteLLM, UpdatedAt: now},
	}}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/public/api/pricing", nil)
	newPricingEngine(&fakePlanStore{}, prices).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"model":"a-model"`) || !strings.Contains(body, `"model":"b-model"`) {
		t.Fatalf("fallback prices missing: %s", body)
	}
	if strings.Index(body, `"model":"a-model"`) > strings.Index(body, `"model":"b-model"`) {
		t.Fatalf("fallback prices should be sorted by model: %s", body)
	}
}

func TestPricingHandlerReturnsInternalErrorWhenStoreFails(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/public/api/pricing", nil)
	newPricingEngine(&fakePlanStore{err: errors.New("database down")}, &fakePriceStore{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "could not load pricing") {
		t.Fatalf("unexpected error body: %s", rec.Body.String())
	}
}
