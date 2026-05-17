// Package main is the entry point for the omnihub gateway binary.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jami1024/omnihub/internal/db"
	"github.com/jami1024/omnihub/internal/handler/gateway"
	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/account"
	"github.com/jami1024/omnihub/internal/service/forward"
	"github.com/jami1024/omnihub/internal/service/guard"
	"github.com/jami1024/omnihub/internal/service/pricing"
	"github.com/jami1024/omnihub/internal/service/provider"
	"github.com/jami1024/omnihub/internal/service/provider/drivers/anthropic"
	"github.com/jami1024/omnihub/internal/service/provider/drivers/claudeplatform"
	"github.com/jami1024/omnihub/internal/service/resolver"
)

// Build info populated by the linker via -ldflags (see Makefile).
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// Process-wide singletons. nil values are valid for graceful
// degradation: no pool / buffer means log-only mode.
var (
	pool        *pgxpool.Pool
	writeBuffer *repository.WriteBuffer
	accountPool *account.Pool
)

const accountPoolRefreshInterval = 30 * time.Second

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	addr := os.Getenv("OMNIHUB_LISTEN")
	if addr == "" {
		addr = ":8080"
	}

	rootCtx, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()

	bootCtx, cancelBoot := context.WithTimeout(rootCtx, 30*time.Second)
	if err := initDatabase(bootCtx); err != nil {
		cancelBoot()
		slog.Error("database init failed", "err", err)
		os.Exit(1)
	}
	cancelBoot()

	// Build the driver registry (one entry per built-in driver) and
	// the account pool. The pool starts empty; we seed from env vars
	// when the table is empty for backward compatibility, then start
	// the periodic refresher.
	registry := buildDriverRegistry()
	if err := setupAccountPool(rootCtx, registry); err != nil {
		slog.Error("account pool init failed", "err", err)
		os.Exit(1)
	}

	defer func() {
		if writeBuffer != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			writeBuffer.Stop(shutdownCtx)
			cancel()
		}
		if pool != nil {
			pool.Close()
		}
	}()

	gin.SetMode(gin.ReleaseMode)
	r := newRouter(registry)

	server := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		// WriteTimeout intentionally left at 0; streaming responses may run
		// for tens of seconds and must not be killed by the server clock.
		IdleTimeout: 120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(rootCtx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("omnihub listening",
			"addr", addr,
			"version", version,
			"commit", commit,
		)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
		os.Exit(1)
	}

	slog.Info("shutdown complete")
}

func newRouter(registry *provider.Registry) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/healthz", handleHealth)
	r.GET("/readyz", handleReady)
	r.GET("/version", handleVersion)

	mountGatewayRoutes(r, registry)

	return r
}

// mountGatewayRoutes wires the LLM forwarding endpoints onto r.
//
// MVP behaviour: a Resolver picks one upstream account per request
// from the in-memory account pool. The pool is populated from the
// `accounts` table; first-run deployments are auto-seeded from env
// vars (OMNIHUB_ANTHROPIC_API_KEY or OMNIHUB_CLAUDE_PLATFORM_*).
//
// If no upstream credentials exist anywhere — empty table AND empty
// env — /v1/messages is not mounted and only the health endpoints
// remain live.
func mountGatewayRoutes(r *gin.Engine, registry *provider.Registry) {
	if accountPool == nil || accountPool.Size() == 0 {
		slog.Warn("no upstream accounts available; /v1/messages disabled. " +
			"Either insert rows into the accounts table or set " +
			"OMNIHUB_ANTHROPIC_API_KEY / OMNIHUB_CLAUDE_PLATFORM_* env vars.")
		return
	}

	auth := guard.NewAuthenticator(os.Getenv("OMNIHUB_API_KEYS"))
	if auth.Disabled() {
		slog.Warn("OMNIHUB_API_KEYS is empty; /v1/messages is OPEN to anyone reaching this port — do not expose publicly")
	} else {
		slog.Info("virtual key auth enabled", "key_count", auth.KeyCount())
	}

	res := resolver.New(accountPool, registry)
	forwarder := forward.New(nil)
	prices := pricing.Default()

	gw := r.Group("/", auth.Middleware(), guard.RequestLog())
	gw.POST("/v1/messages", gateway.AnthropicMessagesHandler(forwarder, res, writeBuffer, prices))

	slog.Info("gateway mounted",
		"path", "/v1/messages",
		"account_count", accountPool.Size(),
	)
}

// buildDriverRegistry registers every built-in driver. Adding a new
// driver in code means appending one line here.
func buildDriverRegistry() *provider.Registry {
	reg := provider.NewRegistry()
	reg.MustRegister(anthropic.New())
	reg.MustRegister(claudeplatform.New())
	return reg
}

// setupAccountPool wires the in-memory account pool against the DB
// (when configured) and bootstraps from env vars on first run.
//
// When the DB is not configured (log-only mode), the function builds
// the pool from env vars in memory only — accounts disappear on
// process exit, which is fine for smoke tests.
func setupAccountPool(ctx context.Context, registry *provider.Registry) error {
	if pool == nil {
		// No DB — build an ephemeral, env-only pool so smoke tests
		// can still exercise the gateway without Postgres.
		accountPool = account.NewPool(envOnlySource{})
		return accountPool.Refresh(ctx)
	}

	repo := repository.NewAccountRepo(pool)

	count, err := repo.CountAll(ctx)
	if err != nil {
		return err
	}
	if count == 0 {
		seeded, err := seedFromEnv(ctx, repo, registry)
		if err != nil {
			return err
		}
		if seeded > 0 {
			slog.Info("auto-seeded accounts from env vars", "count", seeded)
		}
	}

	accountPool = account.NewPool(repo)
	if err := accountPool.Start(ctx, accountPoolRefreshInterval); err != nil {
		return err
	}
	slog.Info("account pool ready",
		"size", accountPool.Size(),
		"refresh_interval", accountPoolRefreshInterval,
	)
	return nil
}

// seedFromEnv inserts one account row per recognised env-var block on
// an empty `accounts` table. Returns the number of rows inserted.
//
// This preserves the env-var workflow that previous MVP revisions
// used, so an upgrade with the same configuration just works.
func seedFromEnv(ctx context.Context, repo *repository.AccountRepo, registry *provider.Registry) (int, error) {
	inserted := 0

	if key := os.Getenv("OMNIHUB_ANTHROPIC_API_KEY"); key != "" {
		if _, ok := registry.Get(anthropic.DriverName); ok {
			id, err := repo.Insert(ctx, repository.InsertParams{
				Name:           "anthropic-env",
				Provider:       anthropic.DriverName,
				Enabled:        true,
				Weight:         100,
				Priority:       0,
				CostMultiplier: 1.0,
				Credentials:    map[string]string{"api_key": key},
			})
			if err != nil {
				return inserted, err
			}
			slog.Info("seeded anthropic account from env", "id", id)
			inserted++
		}
	}

	cpKey := os.Getenv("OMNIHUB_CLAUDE_PLATFORM_API_KEY")
	cpRegion := os.Getenv("OMNIHUB_CLAUDE_PLATFORM_REGION")
	cpWorkspace := os.Getenv("OMNIHUB_CLAUDE_PLATFORM_WORKSPACE_ID")
	if cpKey != "" && cpRegion != "" && cpWorkspace != "" {
		if _, ok := registry.Get(claudeplatform.DriverName); ok {
			id, err := repo.Insert(ctx, repository.InsertParams{
				Name:           "claude-platform-env",
				Provider:       claudeplatform.DriverName,
				Enabled:        true,
				Weight:         100,
				Priority:       0,
				CostMultiplier: 1.0,
				Credentials: map[string]string{
					"api_key":      cpKey,
					"aws_region":   cpRegion,
					"workspace_id": cpWorkspace,
				},
			})
			if err != nil {
				return inserted, err
			}
			slog.Info("seeded claude-platform account from env", "id", id)
			inserted++
		}
	}

	return inserted, nil
}

// envOnlySource builds accounts from env vars purely in memory.
// Used when no DB is configured so the gateway still works for
// smoke tests.
type envOnlySource struct{}

func (envOnlySource) ListEnabled(_ context.Context) ([]*provider.Account, error) {
	var out []*provider.Account
	if key := os.Getenv("OMNIHUB_ANTHROPIC_API_KEY"); key != "" {
		out = append(out, &provider.Account{
			Name:           "anthropic-env",
			Provider:       anthropic.DriverName,
			Weight:         100,
			Priority:       0,
			CostMultiplier: 1.0,
			Credentials:    map[string]string{"api_key": key},
		})
	}
	cpKey := os.Getenv("OMNIHUB_CLAUDE_PLATFORM_API_KEY")
	cpRegion := os.Getenv("OMNIHUB_CLAUDE_PLATFORM_REGION")
	cpWorkspace := os.Getenv("OMNIHUB_CLAUDE_PLATFORM_WORKSPACE_ID")
	if cpKey != "" && cpRegion != "" && cpWorkspace != "" {
		out = append(out, &provider.Account{
			Name:           "claude-platform-env",
			Provider:       claudeplatform.DriverName,
			Weight:         100,
			Priority:       0,
			CostMultiplier: 1.0,
			Credentials: map[string]string{
				"api_key":      cpKey,
				"aws_region":   cpRegion,
				"workspace_id": cpWorkspace,
			},
		})
	}
	return out, nil
}

func handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// handleReady reports readiness: DB reachable when configured.
// /healthz is a liveness probe (always 200 if the process is up),
// /readyz is a readiness probe (200 only when dependencies are usable).
func handleReady(c *gin.Context) {
	if pool != nil {
		pingCtx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(pingCtx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "not_ready",
				"detail": "database unreachable: " + err.Error(),
			})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}

// initDatabase opens a pgx connection pool and runs migrations when
// OMNIHUB_DATABASE_URL is set. With an empty DSN the gateway runs in
// log-only mode (no persisted usage history) and the function is a
// no-op so local smoke tests do not require a database.
func initDatabase(ctx context.Context) error {
	dsn := os.Getenv("OMNIHUB_DATABASE_URL")
	if dsn == "" {
		slog.Warn("OMNIHUB_DATABASE_URL is empty; running in log-only mode (no usage persistence)")
		return nil
	}

	p, err := db.Open(ctx, db.Config{
		DSN:             dsn,
		MaxConns:        20,
		MinConns:        2,
		MaxConnLifetime: time.Hour,
		MaxConnIdleTime: 15 * time.Minute,
	})
	if err != nil {
		return err
	}
	pool = p

	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}

	writeBuffer = repository.NewWriteBuffer(
		repository.NewMessageRequestRepo(pool),
		repository.WriteBufferConfig{}, // production defaults: 250ms / 200 rows / 5000 cap
	)
	slog.Info("database ready", "max_conns", 20)
	return nil
}

func handleVersion(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"version": version,
		"commit":  commit,
		"date":    date,
	})
}
