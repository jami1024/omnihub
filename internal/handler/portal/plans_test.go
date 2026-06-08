package portal_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/handler/portal"
	"github.com/jami1024/omnihub/internal/repository"
)

type fakePortalPlanStore struct {
	plans      []repository.Plan
	grant      *repository.UserPlanGrant
	claimID    int64
	lastUserID int64
	lastPlanID int64
}

func (f *fakePortalPlanStore) ListEnabledPlans(context.Context) ([]repository.Plan, error) {
	return f.plans, nil
}
func (f *fakePortalPlanStore) ActiveGrantForUser(_ context.Context, userID int64, _ time.Time) (*repository.UserPlanGrant, error) {
	f.lastUserID = userID
	return f.grant, nil
}
func (f *fakePortalPlanStore) GetPlan(_ context.Context, planID int64) (*repository.Plan, error) {
	for _, p := range f.plans {
		if p.ID == planID {
			return &p, nil
		}
	}
	return nil, repository.ErrPlanNotFound
}
func (f *fakePortalPlanStore) GrantPlanToUser(_ context.Context, userID, planID int64, _ time.Time) (int64, error) {
	f.lastUserID, f.lastPlanID = userID, planID
	return f.claimID, nil
}

func TestPortalPlansOnlyReturnsEnabledPlans(t *testing.T) {
	store := &fakePortalPlanStore{plans: []repository.Plan{{ID: 1, Name: "Free", PriceUSD: 0, IncludedCreditUSD: 1, Enabled: true}}}
	r := engineWithUser(func(r *gin.Engine) { r.GET("/plans", portal.PlansHandler(store)) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/plans", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Plans []repository.Plan `json:"plans"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Plans) != 1 || resp.Plans[0].Name != "Free" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestPortalCurrentPlanReturnsActiveGrant(t *testing.T) {
	store := &fakePortalPlanStore{grant: &repository.UserPlanGrant{ID: 9, UserID: 7, PlanNameSnapshot: "Starter", CreditRemainingUSD: 2, Status: "active"}}
	r := engineWithUser(func(r *gin.Engine) { r.GET("/me/plan", portal.CurrentPlanHandler(store)) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/me/plan", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Grant *repository.UserPlanGrant `json:"grant"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Grant == nil || resp.Grant.PlanNameSnapshot != "Starter" || store.lastUserID != 7 {
		t.Fatalf("unexpected current plan: %+v user=%d", resp.Grant, store.lastUserID)
	}
}
