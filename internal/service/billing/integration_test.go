package billing_test

// DB-backed end-to-end verification of the plan-first → wallet billing
// path. Unlike billing_test.go (which uses an in-memory fake Store), this
// exercises the real repository SQL — ConsumeGrantCredit's row-locked
// transaction, ActiveGrantForUser, and the derived wallet balance — through
// the production RepositoryStore.
//
// Gated behind OMNIHUB_TEST_DB so the normal `go test ./...` run stays green
// without a database. Point it at a throwaway Postgres:
//
//	OMNIHUB_TEST_DB=postgres://omnihub:omnihub@127.0.0.1:5440/omnihub?sslmode=disable \
//	    go test ./internal/service/billing -run Integration -v

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jami1024/omnihub/internal/db"
	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/billing"
)

const fakeBcryptHash = "$2a$10$0123456789012345678901234567890123456789012345678901" // 60 chars

func testDSN() string { return os.Getenv("OMNIHUB_TEST_DB") }

var uniqCounter int64

// uniq returns a process-unique suffix so each scenario gets its own user
// even when scenarios share one database.
func uniq() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.FormatInt(atomic.AddInt64(&uniqCounter, 1), 10)
}

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := strings.TrimSpace(testDSN())
	if dsn == "" {
		t.Skip("OMNIHUB_TEST_DB not set; skipping DB integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("migrate test db: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// createUser inserts a portal user with a unique username and returns its id.
func createUser(t *testing.T, pool *pgxpool.Pool, name string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(),
		`INSERT INTO users (username, password_hash) VALUES ($1, $2) RETURNING id`,
		name, fakeBcryptHash).Scan(&id)
	if err != nil {
		t.Fatalf("create user %q: %v", name, err)
	}
	return id
}

// newStore builds the same RepositoryStore wired in cmd/omnihub/main.go.
func newStore(pool *pgxpool.Pool) billing.Store {
	return billing.NewRepositoryStore(
		repository.NewPlanRepo(pool),
		repository.NewWalletRepo(pool),
		repository.NewMessageRequestRepo(pool),
	)
}

func grantRemaining(t *testing.T, pool *pgxpool.Pool, grantID int64) (float64, string) {
	t.Helper()
	var rem float64
	var status string
	err := pool.QueryRow(context.Background(),
		`SELECT credit_remaining_usd::float8, status FROM user_plan_grants WHERE id = $1`,
		grantID).Scan(&rem, &status)
	if err != nil {
		t.Fatalf("read grant %d: %v", grantID, err)
	}
	return rem, status
}

func ledgerSum(t *testing.T, pool *pgxpool.Pool, userID int64) float64 {
	t.Helper()
	var sum float64
	err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(amount_usd),0)::float8 FROM plan_usage_ledger WHERE user_id = $1`,
		userID).Scan(&sum)
	if err != nil {
		t.Fatalf("sum plan ledger for user %d: %v", userID, err)
	}
	return sum
}

func approx(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }

// TestIntegrationPlanCoversFullAmount: a charge fully inside plan credit
// debits the plan only, never the wallet, and writes a ledger row.
func TestIntegrationPlanCoversFullAmount(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	store := newStore(pool)
	svc := billing.New(store)
	plans := repository.NewPlanRepo(pool)

	uid := createUser(t, pool, "billing-full-"+uniq())
	planID, err := plans.CreatePlan(ctx, repository.Plan{Name: "Starter", IncludedCreditUSD: 10, PriceRatio: 1, AllowPaygOverage: true, Enabled: true})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	grantID, err := plans.GrantPlanToUser(ctx, uid, planID, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("grant plan: %v", err)
	}

	res, err := svc.Charge(ctx, uid, 4)
	if err != nil {
		t.Fatalf("charge: %v", err)
	}
	if !approx(res.PlanUSD, 4) || !approx(res.WalletUSD, 0) {
		t.Fatalf("expected plan=4 wallet=0, got %+v", res)
	}
	if rem, status := grantRemaining(t, pool, grantID); !approx(rem, 6) || status != "active" {
		t.Fatalf("expected remaining=6 active, got %v %s", rem, status)
	}
	if s := ledgerSum(t, pool, uid); !approx(s, 4) {
		t.Fatalf("expected plan ledger sum=4, got %v", s)
	}
}

// TestIntegrationPlanThenWalletOverage: when the plan runs out mid-charge and
// overage is allowed, the remainder is covered by wallet credit and the grant
// flips to depleted.
func TestIntegrationPlanThenWalletOverage(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	store := newStore(pool)
	svc := billing.New(store)
	plans := repository.NewPlanRepo(pool)
	wallet := repository.NewWalletRepo(pool)

	uid := createUser(t, pool, "billing-overage-"+uniq())
	if err := wallet.AddEntry(ctx, uid, "topup", 10, "test topup", "test"); err != nil {
		t.Fatalf("topup: %v", err)
	}
	planID, err := plans.CreatePlan(ctx, repository.Plan{Name: "Small", IncludedCreditUSD: 3, PriceRatio: 1, AllowPaygOverage: true, Enabled: true})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	grantID, err := plans.GrantPlanToUser(ctx, uid, planID, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("grant plan: %v", err)
	}

	res, err := svc.Charge(ctx, uid, 5)
	if err != nil {
		t.Fatalf("charge: %v", err)
	}
	if !approx(res.PlanUSD, 3) || !approx(res.WalletUSD, 2) {
		t.Fatalf("expected plan=3 wallet=2, got %+v", res)
	}
	if rem, status := grantRemaining(t, pool, grantID); !approx(rem, 0) || status != "depleted" {
		t.Fatalf("expected remaining=0 depleted, got %v %s", rem, status)
	}
}

// TestIntegrationPlanExhaustedNoOverage: when overage is disabled and the
// charge exceeds remaining plan credit, the service rejects with
// ErrInsufficientBalance.
func TestIntegrationPlanExhaustedNoOverage(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	store := newStore(pool)
	svc := billing.New(store)
	plans := repository.NewPlanRepo(pool)

	uid := createUser(t, pool, "billing-nooverage-"+uniq())
	planID, err := plans.CreatePlan(ctx, repository.Plan{Name: "Capped", IncludedCreditUSD: 1, PriceRatio: 1, AllowPaygOverage: false, Enabled: true})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if _, err := plans.GrantPlanToUser(ctx, uid, planID, time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("grant plan: %v", err)
	}

	if _, err := svc.Charge(ctx, uid, 2); err != billing.ErrInsufficientBalance {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}
}

// TestIntegrationConcurrentConsumeIsAtomic: many concurrent charges against a
// single grant must never over-consume below zero — the FOR UPDATE lock in
// ConsumeGrantCredit must serialize them. Plan credit 10, twenty concurrent
// charges of 1 with overage off: exactly 10 succeed, 10 are rejected, and the
// ledger sums to exactly the granted credit.
func TestIntegrationConcurrentConsumeIsAtomic(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	store := newStore(pool)
	svc := billing.New(store)
	plans := repository.NewPlanRepo(pool)

	uid := createUser(t, pool, "billing-concurrent-"+uniq())
	planID, err := plans.CreatePlan(ctx, repository.Plan{Name: "Race", IncludedCreditUSD: 10, PriceRatio: 1, AllowPaygOverage: false, Enabled: true})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if _, err := plans.GrantPlanToUser(ctx, uid, planID, time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("grant plan: %v", err)
	}

	const n = 20
	results := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() { _, err := svc.Charge(ctx, uid, 1); results <- err }()
	}
	var ok, rejected int
	for i := 0; i < n; i++ {
		switch err := <-results; err {
		case nil:
			ok++
		case billing.ErrInsufficientBalance:
			rejected++
		default:
			t.Fatalf("unexpected charge error: %v", err)
		}
	}
	if ok != 10 || rejected != 10 {
		t.Fatalf("expected 10 ok / 10 rejected, got %d ok / %d rejected", ok, rejected)
	}
	if s := ledgerSum(t, pool, uid); !approx(s, 10) {
		t.Fatalf("expected plan ledger to sum to exactly 10, got %v", s)
	}
}
