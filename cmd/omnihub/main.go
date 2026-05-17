// Package main is the entry point for the omnihub gateway binary.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
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
	"github.com/jami1024/omnihub/internal/service/health"
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
// degradation: no pool / buffer means log-only mode and the gateway
// will not mount /v1/messages.
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

	registry := buildDriverRegistry()
	if err := setupAccountPool(rootCtx); err != nil {
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
// The accounts table is the sole source of truth for upstream
// providers. If it is empty (or the gateway is running without a
// database at all), /v1/messages is not mounted — only the health
// endpoints stay live so a load balancer can drain the instance.
//
// The operator is expected to add at least one row to the accounts
// table before the gateway can serve traffic. See the README for the
// SQL snippets; a CLI / admin API will follow.
func mountGatewayRoutes(r *gin.Engine, registry *provider.Registry) {
	if accountPool == nil {
		slog.Error("/v1/messages disabled: no database configured; set OMNIHUB_DATABASE_URL and add accounts to the accounts table")
		return
	}
	if accountPool.Size() == 0 {
		slog.Error("/v1/messages disabled: accounts table is empty. " +
			"Add a row to enable routing, e.g.:\n\n" +
			"  INSERT INTO accounts (name, provider, credentials) VALUES (\n" +
			"      'my-anthropic', 'anthropic',\n" +
			"      '{\"api_key\":\"sk-ant-...\"}'::jsonb);\n\n" +
			"or for Claude Platform on AWS:\n\n" +
			"  INSERT INTO accounts (name, provider, credentials) VALUES (\n" +
			"      'my-cp', 'claude-platform',\n" +
			"      '{\"api_key\":\"sk-aws-...\",\"aws_region\":\"us-east-1\",\"workspace_id\":\"ws_...\"}'::jsonb);")
		return
	}

	auth := guard.NewAuthenticator(os.Getenv("OMNIHUB_API_KEYS"))
	if auth.Disabled() {
		slog.Warn("OMNIHUB_API_KEYS is empty; /v1/messages is OPEN to anyone reaching this port — do not expose publicly")
	} else {
		slog.Info("virtual key auth enabled", "key_count", auth.KeyCount())
	}

	healthCfg := loadHealthConfig()
	tracker := health.New(healthCfg)
	res := resolver.New(accountPool, registry, tracker)
	forwarder := forward.New(nil)
	prices := pricing.Default()

	gw := r.Group("/", auth.Middleware(), guard.RequestLog())
	gw.POST("/v1/messages", gateway.AnthropicMessagesHandler(forwarder, res, tracker, writeBuffer, prices))

	slog.Info("gateway mounted",
		"path", "/v1/messages",
		"account_count", accountPool.Size(),
		"circuit_failure_threshold", healthCfg.FailureThreshold,
		"circuit_open_duration", healthCfg.OpenDuration,
		"circuit_half_open_success", healthCfg.HalfOpenSuccessThreshold,
	)
}

// loadHealthConfig builds the circuit-breaker configuration, falling
// back to DefaultConfig for any env var that is unset or malformed.
//
// Supported overrides:
//
//   - OMNIHUB_CIRCUIT_FAILURE_THRESHOLD    integer, ≥ 0; 0 disables
//   - OMNIHUB_CIRCUIT_OPEN_DURATION        Go duration ("30s", "2m")
//   - OMNIHUB_CIRCUIT_HALF_OPEN_SUCCESS    integer, > 0
//
// Per-account thresholds will arrive as DB columns on the accounts
// table in a follow-up commit; the global values here serve as the
// default for accounts that do not override.
func loadHealthConfig() health.Config {
	cfg := health.DefaultConfig()

	if v := os.Getenv("OMNIHUB_CIRCUIT_FAILURE_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.FailureThreshold = n
		} else {
			slog.Warn("OMNIHUB_CIRCUIT_FAILURE_THRESHOLD invalid; using default",
				"value", v, "default", cfg.FailureThreshold)
		}
	}

	if v := os.Getenv("OMNIHUB_CIRCUIT_OPEN_DURATION"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.OpenDuration = d
		} else {
			slog.Warn("OMNIHUB_CIRCUIT_OPEN_DURATION invalid; using default",
				"value", v, "default", cfg.OpenDuration)
		}
	}

	if v := os.Getenv("OMNIHUB_CIRCUIT_HALF_OPEN_SUCCESS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.HalfOpenSuccessThreshold = n
		} else {
			slog.Warn("OMNIHUB_CIRCUIT_HALF_OPEN_SUCCESS invalid; using default",
				"value", v, "default", cfg.HalfOpenSuccessThreshold)
		}
	}

	return cfg
}

// buildDriverRegistry registers every built-in driver. Adding a new
// driver in code means appending one line here.
func buildDriverRegistry() *provider.Registry {
	reg := provider.NewRegistry()
	reg.MustRegister(anthropic.New())
	reg.MustRegister(claudeplatform.New())
	return reg
}

// setupAccountPool wires the in-memory account pool against the DB.
// When no DB is configured the pool stays nil and mountGatewayRoutes
// reports the missing-provider state to the operator.
func setupAccountPool(ctx context.Context) error {
	if pool == nil {
		// log-only mode without DB — no account source, no routing.
		return nil
	}

	repo := repository.NewAccountRepo(pool)
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
		slog.Warn("OMNIHUB_DATABASE_URL is empty; running in log-only mode (no usage persistence and no upstream accounts)")
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
