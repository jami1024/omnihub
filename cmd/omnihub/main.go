// Package main is the entry point for the omnihub gateway binary.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jami1024/omnihub/internal/db"
	"github.com/jami1024/omnihub/internal/handler/gateway"
	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/account"
	"github.com/jami1024/omnihub/internal/service/apikey"
	"github.com/jami1024/omnihub/internal/service/forward"
	"github.com/jami1024/omnihub/internal/service/guard"
	"github.com/jami1024/omnihub/internal/service/health"
	"github.com/jami1024/omnihub/internal/service/pricing"
	"github.com/jami1024/omnihub/internal/service/provider"
	"github.com/jami1024/omnihub/internal/service/provider/drivers/anthropic"
	"github.com/jami1024/omnihub/internal/service/provider/drivers/claudeplatform"
	"github.com/jami1024/omnihub/internal/service/resolver"
	"github.com/jami1024/omnihub/internal/service/session"
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
	apiKeyPool  *apikey.Pool
)

const accountPoolRefreshInterval = 30 * time.Second

func main() {
	// Dispatch on the first non-flag arg so this binary doubles as
	// gateway daemon AND admin CLI. No args (or "serve") runs the
	// gateway; "account" routes into the management subcommands.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "serve":
			os.Args = append(os.Args[:1], os.Args[2:]...)
		case "account":
			runAccountCommand(os.Args[2:])
			return
		case "key":
			runKeyCommand(os.Args[2:])
			return
		case "help", "-h", "--help":
			printUsage(os.Stdout)
			return
		case "version", "-v", "--version":
			fmt.Printf("omnihub %s (%s, %s)\n", version, commit, date)
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
			printUsage(os.Stderr)
			os.Exit(2)
		}
	}

	runGateway()
}

// printUsage prints the top-level CLI synopsis. Subcommands have
// their own --help.
func printUsage(w io.Writer) {
	fmt.Fprintf(w, `omnihub — commercial-grade unified AI gateway

Usage:
  omnihub [serve]           Start the gateway (default when no args).
  omnihub account <cmd>     Manage upstream accounts (add/list/enable/disable/delete).
  omnihub key <cmd>         Manage virtual API keys (add/list/enable/disable/delete).
  omnihub version           Print build version.
  omnihub help              Print this help.

Run 'omnihub account help' / 'omnihub key help' for subcommand details.
`)
}

func runGateway() {
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
	if err := setupApiKeyPool(rootCtx); err != nil {
		slog.Error("api_keys pool init failed", "err", err)
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

	// Trusted proxies for c.ClientIP(): by default Gin trusts EVERY
	// proxy, which lets a client spoof its IP via X-Forwarded-For.
	// Operators behind nginx / a cloud LB should set
	// OMNIHUB_TRUSTED_PROXIES; without it we trust nobody and
	// c.ClientIP() returns the immediate peer (the LB itself).
	if err := configureTrustedProxies(r); err != nil {
		slog.Warn("trusted proxies config rejected; falling back to no-trust",
			"err", err.Error())
		_ = r.SetTrustedProxies(nil)
	}

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

	var auth *guard.Authenticator
	if apiKeyPool != nil && apiKeyPool.Size() > 0 {
		auth = guard.NewAuthenticator(func(submitted string) *apikey.Key {
			return apiKeyPool.LookupByHash(apikey.HashOf(submitted))
		})
		slog.Info("virtual key auth enabled", "key_count", apiKeyPool.Size())
	} else {
		auth = guard.NewAuthenticator(nil)
		slog.Warn("api_keys table is empty (and no OMNIHUB_API_KEYS env to seed); /v1/messages is OPEN — set OMNIHUB_API_KEYS or run `omnihub key add`")
	}

	healthCfg := loadHealthConfig()
	tracker := health.New(healthCfg)
	// Per-account overrides: when an account row has non-NULL
	// circuit_* columns, those values replace the global defaults
	// for that account only. Pool.ByID is O(1).
	tracker.SetConfigLookup(func(accountID int64) health.Config {
		a := accountPool.ByID(accountID)
		if a == nil {
			return healthCfg
		}
		cfg := healthCfg
		if a.CircuitFailureThreshold != nil {
			cfg.FailureThreshold = *a.CircuitFailureThreshold
		}
		if a.CircuitOpenDuration != nil {
			cfg.OpenDuration = *a.CircuitOpenDuration
		}
		if a.CircuitHalfOpenSuccess != nil {
			cfg.HalfOpenSuccessThreshold = *a.CircuitHalfOpenSuccess
		}
		return cfg
	})

	sessionTTL := loadSessionTTL()
	var sessions *session.Store
	if sessionTTL > 0 {
		sessions = session.New(sessionTTL)
		// Sweep at half the TTL so stale entries do not linger more
		// than 1.5× their lifetime on a fully idle deployment.
		sessions.Start(context.Background(), sessionTTL/2)
	}

	res := resolver.New(accountPool, registry, tracker, sessions)
	forwarder := forward.New(nil)
	prices := pricing.Default()

	clientGate := guard.NewClientGate(os.Getenv("OMNIHUB_ALLOWED_CLIENT_UA_PREFIXES"))
	if clientGate.IsOpen() {
		slog.Warn("OMNIHUB_ALLOWED_CLIENT_UA_PREFIXES=* — every client User-Agent is accepted; this gateway is no longer locked to Claude CLI")
	} else {
		slog.Info("client UA gate enabled", "allowed_prefixes", clientGate.Prefixes())
	}

	gw := r.Group("/", clientGate.Middleware(), auth.Middleware(), guard.RequestLog())
	gw.POST("/v1/messages", gateway.AnthropicMessagesHandler(forwarder, res, tracker, writeBuffer, prices))

	stickyDesc := "off"
	if sessions != nil {
		stickyDesc = sessionTTL.String()
	}
	slog.Info("gateway mounted",
		"path", "/v1/messages",
		"account_count", accountPool.Size(),
		"circuit_failure_threshold", healthCfg.FailureThreshold,
		"circuit_open_duration", healthCfg.OpenDuration,
		"circuit_half_open_success", healthCfg.HalfOpenSuccessThreshold,
		"session_stickiness", stickyDesc,
	)
}

// configureTrustedProxies reads OMNIHUB_TRUSTED_PROXIES (comma-
// separated CIDRs / IPs / hostnames) and hands them to Gin. An empty
// or unset value trusts no proxy, which is the safe default when the
// gateway is exposed directly. Common settings:
//
//	OMNIHUB_TRUSTED_PROXIES=10.0.0.0/8         # private LAN LBs
//	OMNIHUB_TRUSTED_PROXIES=127.0.0.1,::1       # reverse proxy on the same host
//	OMNIHUB_TRUSTED_PROXIES=*                   # trust every proxy (dangerous)
func configureTrustedProxies(r *gin.Engine) error {
	raw := strings.TrimSpace(os.Getenv("OMNIHUB_TRUSTED_PROXIES"))
	if raw == "" {
		return r.SetTrustedProxies(nil)
	}
	if raw == "*" {
		// Explicit opt-in to "trust everyone" — keep Gin's default
		// permissive behaviour. Logged loudly so operators see it.
		slog.Warn("OMNIHUB_TRUSTED_PROXIES=* — every proxy is trusted; clients can spoof X-Forwarded-For")
		return r.SetTrustedProxies([]string{"0.0.0.0/0", "::/0"})
	}
	var entries []string
	for _, p := range strings.Split(raw, ",") {
		if t := strings.TrimSpace(p); t != "" {
			entries = append(entries, t)
		}
	}
	return r.SetTrustedProxies(entries)
}

// setupApiKeyPool wires the in-memory api_keys pool against the DB
// (when configured) and bootstraps from OMNIHUB_API_KEYS when the
// table is empty so existing deployments upgrade transparently.
func setupApiKeyPool(ctx context.Context) error {
	if pool == nil {
		// log-only mode: no DB, no auth, gateway will not mount routes.
		return nil
	}

	repo := repository.NewApiKeyRepo(pool)
	count, err := repo.CountAll(ctx)
	if err != nil {
		return err
	}
	if count == 0 {
		seeded, err := seedApiKeysFromEnv(ctx, repo)
		if err != nil {
			return err
		}
		if seeded > 0 {
			slog.Info("auto-seeded api keys from OMNIHUB_API_KEYS", "count", seeded)
		}
	}

	apiKeyPool = apikey.NewPool(repo)
	if err := apiKeyPool.Start(ctx, accountPoolRefreshInterval); err != nil {
		return err
	}
	apikey.NewListener(pool, "", apiKeyPool.Refresh).Start(ctx)

	slog.Info("api_keys pool ready",
		"size", apiKeyPool.Size(),
		"refresh_interval", accountPoolRefreshInterval,
		"notify_channel", apikey.DefaultNotifyChannel,
	)
	return nil
}

// seedApiKeysFromEnv parses OMNIHUB_API_KEYS (the legacy
// "label:key,label:key" format) into one row per entry. The
// cleartext is hashed before insert so the env value remains
// disposable.
func seedApiKeysFromEnv(ctx context.Context, repo *repository.ApiKeyRepo) (int, error) {
	spec := strings.TrimSpace(os.Getenv("OMNIHUB_API_KEYS"))
	if spec == "" {
		return 0, nil
	}

	inserted := 0
	for _, raw := range strings.Split(spec, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		label, value := "default", raw
		if i := strings.Index(raw, ":"); i > 0 {
			label = strings.TrimSpace(raw[:i])
			value = strings.TrimSpace(raw[i+1:])
		}
		if value == "" {
			continue
		}
		name := label
		if name == "default" {
			name = fmt.Sprintf("env-%d", inserted+1)
		}
		_, err := repo.Insert(ctx, repository.ApiKeyInsertParams{
			Name:    name,
			Hash:    apikey.HashOf(value),
			Label:   label,
			Enabled: true,
		})
		if err != nil {
			return inserted, err
		}
		inserted++
	}
	return inserted, nil
}

// loadSessionTTL parses OMNIHUB_SESSION_TTL into a Go duration.
// Unset falls back to session.DefaultTTL (5 minutes). The special
// value "0" or "off" disables stickiness entirely. Malformed values
// log a warning and fall back to the default.
func loadSessionTTL() time.Duration {
	v := os.Getenv("OMNIHUB_SESSION_TTL")
	if v == "" {
		return session.DefaultTTL
	}
	if v == "0" || v == "off" || v == "false" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		slog.Warn("OMNIHUB_SESSION_TTL invalid; using default",
			"value", v, "default", session.DefaultTTL)
		return session.DefaultTTL
	}
	return d
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

	// Subscribe to NOTIFY for instant pool refresh on account
	// changes. The periodic ticker above stays as a safety net for
	// missed notifications (process restart, connection drop).
	account.NewListener(pool, "", accountPool.Refresh).Start(ctx)

	slog.Info("account pool ready",
		"size", accountPool.Size(),
		"refresh_interval", accountPoolRefreshInterval,
		"notify_channel", account.DefaultNotifyChannel,
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
