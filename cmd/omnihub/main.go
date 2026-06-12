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

	"github.com/jami1024/omnihub/internal/crypto"
	"github.com/jami1024/omnihub/internal/db"
	adminhandler "github.com/jami1024/omnihub/internal/handler/admin"
	authhandler "github.com/jami1024/omnihub/internal/handler/auth"
	"github.com/jami1024/omnihub/internal/handler/gateway"
	portalhandler "github.com/jami1024/omnihub/internal/handler/portal"
	publichandler "github.com/jami1024/omnihub/internal/handler/public"
	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/account"
	"github.com/jami1024/omnihub/internal/service/admin"
	"github.com/jami1024/omnihub/internal/service/alert"
	"github.com/jami1024/omnihub/internal/service/alertchannel"
	"github.com/jami1024/omnihub/internal/service/apikey"
	"github.com/jami1024/omnihub/internal/service/billing"
	"github.com/jami1024/omnihub/internal/service/blockedip"
	"github.com/jami1024/omnihub/internal/service/forward"
	"github.com/jami1024/omnihub/internal/service/guard"
	"github.com/jami1024/omnihub/internal/service/health"
	"github.com/jami1024/omnihub/internal/service/healthlog"
	"github.com/jami1024/omnihub/internal/service/limits"
	"github.com/jami1024/omnihub/internal/service/metrics"
	"github.com/jami1024/omnihub/internal/service/pricesync"
	"github.com/jami1024/omnihub/internal/service/pricing"
	"github.com/jami1024/omnihub/internal/service/provider"
	"github.com/jami1024/omnihub/internal/service/provider/drivers/anthropic"
	"github.com/jami1024/omnihub/internal/service/provider/drivers/claudeplatform"
	"github.com/jami1024/omnihub/internal/service/provider/drivers/codex"
	"github.com/jami1024/omnihub/internal/service/provider/drivers/openai"
	"github.com/jami1024/omnihub/internal/service/resolver"
	"github.com/jami1024/omnihub/internal/service/session"
	"github.com/jami1024/omnihub/internal/service/upstreamauth"
	"github.com/jami1024/omnihub/internal/web"
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
	pool             *pgxpool.Pool
	writeBuffer      *repository.WriteBuffer
	accountPool      *account.Pool
	apiKeyPool       *apikey.Pool
	blockedIPPool    *blockedip.Pool
	alertChannelPool *alertchannel.Pool
	balanceGuard     *limits.BalanceGuard
	pricePool        *pricing.Pool
	priceRefresher   *pricesync.Refresher
	accountCipher    *crypto.Cipher
	gatewaySettings  *liveGatewaySettings
)

const accountPoolRefreshInterval = 30 * time.Second

// accountSpendRefreshInterval is how often the per-account spend guard
// reloads daily / total USD totals from the DB. Matches the pool
// refresh cadence; only accounts with a cap configured are queried.
const accountSpendRefreshInterval = 30 * time.Second

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
		case "admin":
			runAdminCommand(os.Args[2:])
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

	// At-rest encryption for sensitive account fields. A misconfigured
	// key must fail loudly rather than silently storing plaintext.
	var cipherErr error
	accountCipher, cipherErr = crypto.New(os.Getenv("OMNIHUB_ENCRYPTION_KEY"))
	if cipherErr != nil {
		slog.Error("OMNIHUB_ENCRYPTION_KEY invalid", "err", cipherErr)
		os.Exit(1)
	}
	slog.Info("at-rest encryption", "enabled", accountCipher.Enabled())

	registry := buildDriverRegistry()
	if err := setupAccountPool(rootCtx); err != nil {
		slog.Error("account pool init failed", "err", err)
		os.Exit(1)
	}
	if err := setupApiKeyPool(rootCtx); err != nil {
		slog.Error("api_keys pool init failed", "err", err)
		os.Exit(1)
	}
	if err := setupBlockedIPPool(rootCtx); err != nil {
		slog.Error("blocked_ips pool init failed", "err", err)
		os.Exit(1)
	}
	if err := setupAlertChannelPool(rootCtx); err != nil {
		slog.Error("alert_channels pool init failed", "err", err)
		os.Exit(1)
	}
	if err := setupPricePool(rootCtx); err != nil {
		slog.Error("model_prices pool init failed", "err", err)
		os.Exit(1)
	}
	setupGatewaySettings(rootCtx)

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

	// Prometheus metrics. Optionally guarded by a bearer token
	// (OMNIHUB_METRICS_TOKEN); when unset the endpoint is open and should
	// be restricted at the reverse proxy. Mounted unconditionally so
	// scrapers get a stable target even before any gateway traffic.
	r.GET("/metrics", gin.WrapH(metrics.Handler(os.Getenv("OMNIHUB_METRICS_TOKEN"))))

	// Upstream auth plugins (cold path: login / import / refresh).
	// Registered once and shared by the gateway's TokenManager and the
	// admin credential-import endpoints.
	authPlugins := upstreamauth.NewRegistry()
	authPlugins.Register("codex-oauth", upstreamauth.NewCodexOAuth())

	// mountGatewayRoutes builds the in-memory circuit-breaker Tracker;
	// hand it to the admin routes so the dashboard can read live breaker
	// state and reset it. It is nil when the gateway is disabled (no
	// accounts / no DB), and the admin circuit handlers treat nil as
	// "circuit data unavailable".
	tracker := mountGatewayRoutes(r, registry, authPlugins)
	mountAdminRoutes(r, tracker, registry, authPlugins)

	return r
}

// mountAdminRoutes wires the web admin UI: /admin/api/* JSON endpoints
// (login + auth-guarded data routes) plus the embedded React SPA served
// from the same binary under /admin/*.
//
// The admin surface is gated on two env-driven preconditions:
//
//   - OMNIHUB_ADMIN_JWT_SECRET must be set. Without a secret the issuer
//     would sign with no key, so we refuse to mount instead of crashing
//     later. Operators see a single startup warn and the gateway keeps
//     serving /v1/messages normally.
//   - OMNIHUB_ADMIN_EMAIL and a password source must be set. The admin
//     identity is env-owned; public signup only creates portal users.
//   - A database must be configured. Log-only deployments skip the web UI.
//
// The SPA is served via gin's NoRoute fallback rather than a wildcard
// route because /admin/api/* already lives under /admin/ and gin
// disallows a catch-all sharing a prefix with concrete routes.
func mountAdminRoutes(r *gin.Engine, tracker *health.Tracker, registry *provider.Registry, authPlugins *upstreamauth.Registry) {
	secret := os.Getenv("OMNIHUB_ADMIN_JWT_SECRET")
	if secret == "" {
		slog.Warn("/admin disabled: OMNIHUB_ADMIN_JWT_SECRET not set; the web UI will not authenticate")
		return
	}
	adminCreds, ok := adminCredentialsFromEnv()
	if !ok {
		slog.Warn("/admin disabled: set OMNIHUB_ADMIN_EMAIL and OMNIHUB_ADMIN_PASSWORD_HASH (or OMNIHUB_ADMIN_PASSWORD)")
		return
	}
	if pool == nil {
		slog.Warn("/admin disabled: no database configured (set OMNIHUB_DATABASE_URL)")
		return
	}

	issuer := admin.NewIssuer([]byte(secret), 24*time.Hour)
	accountRepo := repository.NewAccountRepo(pool, accountCipher)
	groupRepo := repository.NewProviderGroupRepo(pool)
	apiKeyRepo := repository.NewApiKeyRepo(pool)
	blockedIPRepo := repository.NewBlockedIPRepo(pool)
	messageRepo := repository.NewMessageRequestRepo(pool)
	walletRepo := repository.NewWalletRepo(pool)
	redemptionRepo := repository.NewRedemptionRepo(pool)
	healthEventRepo := repository.NewAccountHealthEventRepo(pool)
	adminAuth := guard.NewAdminAuthenticator(issuer)

	// A nil *health.Tracker (gateway disabled) must reach the handlers as
	// a nil interface, not a non-nil interface wrapping a nil pointer —
	// otherwise the handlers' nil check wouldn't fire and Snapshot/Reset
	// would panic. Only assign when the tracker actually exists.
	var circuitTracker adminhandler.CircuitTracker
	if tracker != nil {
		circuitTracker = tracker
	}

	api := r.Group("/admin/api")
	api.POST("/login", adminhandler.LoginHandler(adminCreds, issuer))

	authed := api.Group("", adminAuth.Middleware())
	authed.GET("/me", adminhandler.MeHandler())

	// M2 — account management. Writes flow through the accounts table's
	// NOTIFY trigger (migration 0006), so the in-memory account pool
	// refreshes within milliseconds of any create/update/delete.
	authed.GET("/accounts", adminhandler.ListAccountsHandler(accountRepo))
	authed.POST("/accounts", adminhandler.CreateAccountHandler(accountRepo))
	authed.PATCH("/accounts/:id", adminhandler.UpdateAccountHandler(accountRepo))
	authed.DELETE("/accounts/:id", adminhandler.DeleteAccountHandler(accountRepo))
	// M8a — connectivity test: probe the form's values before saving, or
	// an existing account by id using its stored credentials.
	authed.POST("/accounts/test", adminhandler.TestAccountHandler(registry))
	authed.POST("/accounts/:id/test", adminhandler.TestAccountByIDHandler(accountRepo, registry))
	// Upstream-OAuth phase 2 — credential import (paste ~/.codex/auth.json)
	// plus the plugin metadata list the account form's auth picker reads.
	authed.GET("/auth-plugins", adminhandler.ListAuthPluginsHandler(authPlugins))
	authed.POST("/accounts/:id/import-credentials", adminhandler.ImportAccountCredentialsHandler(accountRepo, authPlugins))

	// M8b-3 — provider groups: organisational buckets with a shared cost
	// multiplier. The provider_groups NOTIFY trigger (migration 0018)
	// refreshes the account pool so group-multiplier edits take effect
	// without a restart.
	authed.GET("/groups", adminhandler.ListGroupsHandler(groupRepo))
	authed.POST("/groups", adminhandler.CreateGroupHandler(groupRepo))
	authed.PATCH("/groups/:id", adminhandler.UpdateGroupHandler(groupRepo))
	authed.DELETE("/groups/:id", adminhandler.DeleteGroupHandler(groupRepo))

	// M3 — virtual key management. Writes flow through the api_keys
	// NOTIFY trigger (migration 0008), so the in-memory key pool refreshes
	// within milliseconds. The cleartext key is returned only on create.
	authed.GET("/keys", adminhandler.ListKeysHandler(apiKeyRepo))
	authed.POST("/keys", adminhandler.CreateKeyHandler(apiKeyRepo))
	authed.PATCH("/keys/:id", adminhandler.UpdateKeyHandler(apiKeyRepo))
	authed.DELETE("/keys/:id", adminhandler.DeleteKeyHandler(apiKeyRepo))

	// M4 — blocked-IP management. Writes flow through the blocked_ips
	// NOTIFY trigger (migration 0011), so the in-memory policy pool
	// refreshes within milliseconds. Rows are keyed by IP.
	authed.GET("/blocked-ips", adminhandler.ListBlockedIPsHandler(blockedIPRepo))
	authed.POST("/blocked-ips", adminhandler.CreateBlockedIPHandler(blockedIPRepo))
	authed.PATCH("/blocked-ips/:ip", adminhandler.UpdateBlockedIPHandler(blockedIPRepo))
	authed.DELETE("/blocked-ips/:ip", adminhandler.DeleteBlockedIPHandler(blockedIPRepo))

	// Alert channels: writes flow through the alert_channels NOTIFY
	// trigger (migration 0026) so the in-memory channel pool the Alerter
	// delivers through refreshes within milliseconds. url is encrypted.
	alertChannelRepo := repository.NewAlertChannelRepo(pool, accountCipher)
	authed.GET("/alert-channels", adminhandler.ListAlertChannelsHandler(alertChannelRepo))
	authed.POST("/alert-channels", adminhandler.CreateAlertChannelHandler(alertChannelRepo))
	authed.PATCH("/alert-channels/:id", adminhandler.UpdateAlertChannelHandler(alertChannelRepo))
	authed.DELETE("/alert-channels/:id", adminhandler.DeleteAlertChannelHandler(alertChannelRepo))
	authed.POST("/alert-channels/:id/test", adminhandler.TestAlertChannelHandler(alertChannelRepo))

	// M4 — usage dashboard. Read-only aggregation over message_requests.
	authed.GET("/usage", adminhandler.UsageHandler(messageRepo))

	// M4 — circuit-breaker health. Live state comes from the in-memory
	// tracker; the transition feed is read from account_health_events.
	authed.GET("/circuit", adminhandler.CircuitStatusHandler(circuitTracker, accountRepo))
	authed.GET("/circuit/events", adminhandler.CircuitEventsHandler(healthEventRepo))
	authed.POST("/accounts/:id/reset-breaker", adminhandler.ResetBreakerHandler(circuitTracker))

	// M5 — model pricing. Manual rows + LiteLLM sync; writes hot-reload
	// the in-memory price pool via the model_prices NOTIFY trigger.
	modelPriceRepo := repository.NewModelPriceRepo(pool)
	authed.GET("/prices", adminhandler.ListPricesHandler(modelPriceRepo))
	authed.POST("/prices", adminhandler.CreatePriceHandler(modelPriceRepo))
	authed.PATCH("/prices/:id", adminhandler.UpdatePriceHandler(modelPriceRepo))
	authed.DELETE("/prices/:id", adminhandler.DeletePriceHandler(modelPriceRepo))
	authed.POST("/prices/sync", adminhandler.SyncPricesHandler(priceRefresher))

	// M6 — end-user self-service portal at /portal/api/*. Open signup +
	// login mint a "user"-kind token (rejected by the admin guard); the
	// rest is scoped to the authenticated user's own keys and usage.
	userRepo := repository.NewUserRepo(pool)
	portalSettingsRepo := repository.NewPortalSettingsRepo(pool)
	gatewaySettingsRepo := repository.NewGatewaySettingsRepo(pool)
	announcementRepo := repository.NewAnnouncementRepo(pool)
	planRepo := repository.NewPlanRepo(pool)
	userAuth := guard.NewUserAuthenticator(issuer)
	publicAPI := r.Group("/public/api")
	publicAPI.GET("/pricing", publichandler.PricingHandler(planRepo, modelPriceRepo))
	authAPI := r.Group("/auth/api")
	authAPI.POST("/login", authhandler.LoginHandler(adminCreds, userRepo, issuer))
	papi := r.Group("/portal/api")
	papi.POST("/signup", portalhandler.SignupHandlerWithReservedEmail(userRepo, portalSettingsRepo, walletRepo, issuer, adminCreds.Email))
	papi.POST("/login", portalhandler.LoginHandler(userRepo, issuer))
	puser := papi.Group("", userAuth.Middleware())
	puser.GET("/me", portalhandler.MeHandler(userRepo))
	puser.GET("/keys", portalhandler.ListKeysHandler(apiKeyRepo, messageRepo))
	puser.POST("/keys", portalhandler.CreateKeyHandler(apiKeyRepo, portalSettingsRepo))
	puser.DELETE("/keys/:id", portalhandler.DeleteKeyHandler(apiKeyRepo))
	puser.GET("/usage", portalhandler.UsageHandler(messageRepo, apiKeyRepo))
	puser.GET("/wallet", portalhandler.WalletHandler(walletRepo, messageRepo))
	puser.POST("/redeem", portalhandler.RedeemHandler(redemptionRepo, balanceGuard))
	puser.GET("/requests", portalhandler.RequestsHandler(messageRepo, apiKeyRepo))
	puser.GET("/announcements", portalhandler.AnnouncementsHandler(announcementRepo))
	puser.GET("/plans", portalhandler.PlansHandler(planRepo))
	puser.GET("/me/plan", portalhandler.CurrentPlanHandler(planRepo))
	puser.POST("/plans/:id/claim", portalhandler.ClaimPlanHandler(planRepo))

	// M7 — admin oversight of portal users + the portal policy (signup
	// toggle, per-key limit default/ceiling).
	authed.GET("/users", adminhandler.ListUsersHandler(userRepo))
	authed.PATCH("/users/:id", adminhandler.UpdateUserHandler(userRepo))
	authed.DELETE("/users/:id", adminhandler.DeleteUserHandler(userRepo))
	// Prepaid wallet: view balance + ledger, and apply a credit (top-up /
	// adjust / refund). Balance is enforced only when OMNIHUB_BILLING_ENABLED.
	authed.GET("/users/:id/wallet", adminhandler.GetUserWalletHandler(walletRepo, messageRepo))
	authed.POST("/users/:id/recharge", adminhandler.RechargeUserHandler(walletRepo, messageRepo, balanceGuard))
	// Redemption (gift) codes: generate a batch, list batch summaries.
	authed.GET("/redemptions", adminhandler.ListRedemptionsHandler(redemptionRepo))
	authed.POST("/redemptions", adminhandler.GenerateRedemptionsHandler(redemptionRepo))
	authed.GET("/settings", adminhandler.GetSettingsHandler(portalSettingsRepo))
	authed.PUT("/settings", adminhandler.UpdateSettingsHandler(portalSettingsRepo))
	authed.GET("/gateway-settings", adminhandler.GetGatewaySettingsHandler(gatewaySettingsRepo))
	authed.PUT("/gateway-settings", adminhandler.UpdateGatewaySettingsHandler(gatewaySettingsRepo, gatewaySettings))
	authed.GET("/announcements", adminhandler.ListAnnouncementsHandler(announcementRepo))
	authed.POST("/announcements", adminhandler.CreateAnnouncementHandler(announcementRepo))
	authed.PATCH("/announcements/:id", adminhandler.UpdateAnnouncementHandler(announcementRepo))
	authed.DELETE("/announcements/:id", adminhandler.DeleteAnnouncementHandler(announcementRepo))
	authed.GET("/plans", adminhandler.ListPlansHandler(planRepo))
	authed.POST("/plans", adminhandler.CreatePlanHandler(planRepo))
	authed.PATCH("/plans/:id", adminhandler.UpdatePlanHandler(planRepo))
	authed.GET("/users/:id/plan-grants", adminhandler.ListUserPlanGrantsHandler(planRepo))
	authed.POST("/users/:id/plan-grants", adminhandler.GrantPlanToUserHandler(planRepo))

	if web.Available() {
		// One bundle, served from the root, backs both surfaces: the admin
		// console at /admin/* and the end-user portal at /portal/*, with
		// shared assets at /assets/*. The SPA handler serves a real file
		// when one matches and otherwise falls back to index.html so the
		// client router handles deep links.
		spa := web.SPAHandler("")
		r.NoRoute(func(c *gin.Context) {
			p := c.Request.URL.Path
			if strings.HasPrefix(p, "/admin") || strings.HasPrefix(p, "/portal") || p == "/login" ||
				strings.HasPrefix(p, "/assets") || p == "/" {
				spa(c)
				return
			}
			c.AbortWithStatus(http.StatusNotFound)
		})
		slog.Info("web UI mounted", "console", "/admin", "portal", "/portal")
	} else {
		slog.Info("admin API mounted (devui build, SPA served by external Vite dev server)",
			"api_paths", []string{"/admin/api/login", "/admin/api/me"})
	}
}

func adminCredentialsFromEnv() (adminhandler.EnvAdminCredentials, bool) {
	email := strings.ToLower(strings.TrimSpace(os.Getenv("OMNIHUB_ADMIN_EMAIL")))
	hash := strings.TrimSpace(os.Getenv("OMNIHUB_ADMIN_PASSWORD_HASH"))
	if email == "" {
		return adminhandler.EnvAdminCredentials{}, false
	}
	if hash == "" {
		password := os.Getenv("OMNIHUB_ADMIN_PASSWORD")
		if password == "" {
			return adminhandler.EnvAdminCredentials{}, false
		}
		var err error
		hash, err = admin.HashPassword(password)
		if err != nil {
			slog.Error("admin password hash failed", "err", err.Error())
			return adminhandler.EnvAdminCredentials{}, false
		}
	}
	return adminhandler.EnvAdminCredentials{Email: email, PasswordHash: hash}, true
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
func mountGatewayRoutes(r *gin.Engine, registry *provider.Registry, authPlugins *upstreamauth.Registry) *health.Tracker {
	if accountPool == nil {
		slog.Error("/v1/messages disabled: no database configured; set OMNIHUB_DATABASE_URL and add accounts to the accounts table")
		return nil
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
		return nil
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

	healthCfg := currentCircuitConfig()
	tracker := health.New(healthCfg)
	// Per-account overrides: when an account row has non-NULL
	// circuit_* columns, those values replace the global defaults
	// for that account only. Pool.ByID is O(1).
	tracker.SetConfigLookup(func(accountID int64) health.Config {
		a := accountPool.ByID(accountID)
		if a == nil {
			return healthCfg
		}
		cfg := currentCircuitConfig()
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

	// Persist every circuit-breaker transition to account_health_events
	// so operators can query the flap history with plain SQL. The
	// recorder runs an async writer goroutine; the hot path on the
	// tracker side is non-blocking.
	var transitionHandlers []health.TransitionHandler
	if pool != nil {
		recorder := healthlog.New(repository.NewAccountHealthEventRepo(pool), accountPool)
		recorder.Start(context.Background())
		transitionHandlers = append(transitionHandlers, recorder.Handler)
		slog.Info("account health event recorder running", "queue_size", 256)
	}
	// Operational alerting: notify external channels (webhook / Feishu /
	// DingTalk) when an account's breaker trips OPEN or recovers. Off
	// unless at least one OMNIHUB_ALERT_*_URL is set. Works without a DB
	// (it only needs the in-memory account pool for names).
	if alerter := buildAlerter(accountPool, alertChannelPool); alerter != nil {
		alerter.Start(context.Background())
		transitionHandlers = append(transitionHandlers, alerter.Handler)
		slog.Info("circuit-breaker alerting enabled", "channels", alerter.NotifierNames())
	}
	if len(transitionHandlers) > 0 {
		tracker.SetTransitionHandler(health.FanOut(transitionHandlers...))
	}

	sessionTTL := loadSessionTTL()
	var sessions *session.Store
	if sessionTTL > 0 {
		sessions = session.New(sessionTTL)
		// Sweep at half the TTL so stale entries do not linger more
		// than 1.5× their lifetime on a fully idle deployment.
		sessions.Start(context.Background(), sessionTTL/2)
	}

	res := resolver.New(accountPool, registry, tracker, sessions)
	// Per-account spend caps: a background-refreshed guard skips
	// accounts that have reached their daily / total USD limit. Requires
	// a DB-backed spend source; stays disabled in log-only mode.
	var accountGuard *limits.AccountGuard
	if pool != nil {
		accountGuard = limits.NewAccountGuard(repository.NewMessageRequestRepo(pool))
		accountGuard.Start(context.Background(), accountPool.All, accountSpendRefreshInterval)
		res.SetSpendFilter(accountGuard)
		slog.Info("per-account spend caps enabled", "refresh_interval", accountSpendRefreshInterval)
	}

	// Per-account observability gauges: circuit state + measured spend,
	// read live from the tracker/guard at each /metrics scrape.
	registerAccountGauges(tracker, accountPool, accountGuard)

	// Active health probes: a background prober checks each opted-in
	// account's upstream reachability and feeds the verdict into the same
	// circuit breaker the resolver already honours, so a sick upstream is
	// taken out of rotation before user traffic hits it. Globally off by
	// default (probes cost a real upstream GET); opt in per account.
	probeCfg := currentProbeConfig()
	prober := health.NewProberWithConfig(tracker, registry, func() health.ProbeConfig {
		return currentProbeConfig()
	})
	prober.Start(context.Background(), accountPool.All, probeCfg.Interval)
	slog.Info("active health probes started",
		"global_default", probeCfg.GlobalDefault,
		"interval", probeCfg.Interval,
		"concurrency", probeCfg.Concurrency)

	// TokenManager: keeps oauth / imported_oauth account tokens fresh.
	// On-demand at dispatch time (EnsureFresh / 401 ForceRefresh) plus a
	// background sweep so idle accounts stay fresh and the admin UI's
	// auth_status stays truthful.
	tokenManager := upstreamauth.NewTokenManager(
		repository.NewAccountRepo(pool, accountCipher),
		authPlugins,
		upstreamauth.RefreshWindowFromEnv(),
	)
	tokenManager.Start(context.Background(), accountPool.All, time.Minute)

	forwarder := forward.New(nil)
	// Hot-reloadable, DB-backed price source (built by setupPricePool).
	// Falls back to the static defaults if the pool wasn't built.
	var prices pricing.Calculator = pricePool
	if pricePool == nil {
		prices = pricing.Default()
	}

	clientGate := guard.NewClientGate(os.Getenv("OMNIHUB_ALLOWED_CLIENT_UA_PREFIXES"))
	if clientGate.IsOpen() {
		slog.Warn("OMNIHUB_ALLOWED_CLIENT_UA_PREFIXES=* — every client User-Agent is accepted; this gateway is no longer locked to Claude CLI")
	} else {
		slog.Info("client UA gate enabled", "allowed_prefixes", clientGate.Prefixes())
	}

	limiter, rpmCache := buildLimiter()
	var billingCharger gateway.BillingCharger
	if pool != nil && billingEnabled() {
		billingCharger = billing.New(billing.NewRepositoryStore(
			repository.NewPlanRepo(pool),
			repository.NewWalletRepo(pool),
			repository.NewMessageRequestRepo(pool),
		))
	}

	gw := r.Group("/",
		guard.IPBlockMiddleware(blockedIPPool, rpmCache),
		clientGate.Middleware(),
		auth.Middleware(),
		guard.RequestLog(),
	)
	gw.POST("/v1/messages", gateway.AnthropicMessagesHandler(forwarder, res, tracker, writeBuffer, prices, limiter, blockedIPPool, billingCharger, tokenManager, gatewaySettings))

	// OpenAI Chat Completions endpoint. OpenAI SDK clients are not Claude
	// CLI, so the Claude-CLI client gate is intentionally omitted here;
	// IP-block, auth, and request-log still apply. Requests route only to
	// openai-family accounts — when none exist the resolver returns a
	// clean 503 no_upstream_available.
	gwOpenAI := r.Group("/",
		guard.IPBlockMiddleware(blockedIPPool, rpmCache),
		auth.Middleware(),
		guard.RequestLog(),
	)
	gwOpenAI.POST("/v1/chat/completions", gateway.OpenAIChatCompletionsHandler(forwarder, res, tracker, writeBuffer, prices, limiter, blockedIPPool, billingCharger, tokenManager, gatewaySettings))

	// OpenAI Responses endpoint (EXPERIMENTAL): pass-through to Codex
	// subscription accounts via the openai-codex driver. Same middleware
	// stack as /v1/chat/completions (Codex CLI is not Claude CLI, so the
	// Claude UA gate is omitted). /v1/models serves the static codex
	// model list for Responses-speaking clients.
	gwOpenAI.POST("/v1/responses", gateway.ResponsesHandler(forwarder, res, tracker, writeBuffer, prices, limiter, blockedIPPool, billingCharger, tokenManager, gatewaySettings))
	gwOpenAI.GET("/v1/models", gateway.ModelsHandler(codex.KnownModels))

	stickyDesc := "off"
	if sessions != nil {
		stickyDesc = sessionTTL.String()
	}
	slog.Info("gateway mounted",
		"paths", []string{"/v1/messages", "/v1/chat/completions", "/v1/responses"},
		"account_count", accountPool.Size(),
		"circuit_failure_threshold", healthCfg.FailureThreshold,
		"circuit_open_duration", healthCfg.OpenDuration,
		"circuit_half_open_success", healthCfg.HalfOpenSuccessThreshold,
		"session_stickiness", stickyDesc,
	)
	return tracker
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

// setupBlockedIPPool wires the in-memory IP blocklist against the
// DB. A nil DB pool degrades to a no-op (the middleware then skips
// the check) so log-only / dev runs keep working without the
// migration applied.
func setupBlockedIPPool(ctx context.Context) error {
	if pool == nil {
		return nil
	}
	repo := repository.NewBlockedIPRepo(pool)
	blockedIPPool = blockedip.NewPool(repo)
	if err := blockedIPPool.Start(ctx, accountPoolRefreshInterval); err != nil {
		return err
	}
	blockedip.NewListener(pool, "", blockedIPPool.Refresh).Start(ctx)
	slog.Info("blocked_ips pool ready",
		"size", blockedIPPool.Size(),
		"refresh_interval", accountPoolRefreshInterval,
		"notify_channel", blockedip.DefaultNotifyChannel,
	)
	return nil
}

// setupAlertChannelPool wires the in-memory alert-channel pool against the
// DB, hot-reloaded on the alert_channels NOTIFY trigger (migration 0026).
// A nil DB pool degrades to a no-op (env-configured alert channels still
// work).
func setupAlertChannelPool(ctx context.Context) error {
	if pool == nil {
		return nil
	}
	repo := repository.NewAlertChannelRepo(pool, accountCipher)
	alertChannelPool = alertchannel.NewPool(repo)
	if err := alertChannelPool.Start(ctx, accountPoolRefreshInterval); err != nil {
		return err
	}
	alertchannel.NewListener(pool, "", alertChannelPool.Refresh).Start(ctx)
	slog.Info("alert_channels pool ready",
		"size", alertChannelPool.Size(),
		"refresh_interval", accountPoolRefreshInterval,
		"notify_channel", alertchannel.DefaultNotifyChannel,
	)
	return nil
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

// setupPricePool wires the DB-backed price pool: the built-in
// pricing.Default() table overlaid with the model_prices rows. On an
// empty table it seeds once from LiteLLM (async, non-fatal). Writes —
// the admin UI or a sync pass — refresh the pool via the NOTIFY trigger
// from migration 0013.
func setupPricePool(ctx context.Context) error {
	if pool == nil {
		// Log-only mode: no DB. Hand the gateway the static defaults so
		// pricing still works for known models.
		pricePool = pricing.NewPool(pricing.Default())
		return nil
	}

	repo := repository.NewModelPriceRepo(pool)
	pricePool = pricing.NewPool(pricing.Default())
	priceRefresher = pricesync.New(repo, pricePool, pricing.Default())

	// Load any existing rows into the pool before serving traffic.
	if err := priceRefresher.Refresh(ctx); err != nil {
		return err
	}
	pricesync.NewListener(pool, "", priceRefresher.Refresh).Start(ctx)

	// First-boot seed from LiteLLM runs in the background so a slow or
	// unreachable price source never delays startup; the built-in
	// defaults price traffic until it lands.
	syncURL := os.Getenv("OMNIHUB_PRICE_SYNC_URL")
	go priceRefresher.EnsureSeeded(context.Background(), syncURL)

	slog.Info("model_prices pool ready",
		"size", pricePool.Size(),
		"notify_channel", pricesync.DefaultNotifyChannel,
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
// registerAccountGauges installs the /metrics account gauge source. It
// reads circuit state from the tracker and measured spend from the guard
// (nil in log-only mode) for every account in the pool, on each scrape.
func registerAccountGauges(tracker *health.Tracker, pool *account.Pool, guard *limits.AccountGuard) {
	if tracker == nil || pool == nil {
		return
	}
	src := func() []metrics.AccountGauge {
		accs := pool.All()
		out := make([]metrics.AccountGauge, 0, len(accs))
		for _, a := range accs {
			g := metrics.AccountGauge{
				AccountName:  a.Name,
				CircuitState: string(tracker.Snapshot(a.ID).State),
			}
			if guard != nil {
				if spend, ok := guard.DailySpend(a.ID); ok {
					g.SpendUSD, g.HasSpend = spend, true
				}
			}
			out = append(out, g)
		}
		return out
	}
	if err := metrics.RegisterAccountGauges(src); err != nil {
		slog.Warn("account gauges not registered", "err", err.Error())
	}
}

// buildAlerter wires the circuit-breaker alerter. Its notifier set is the
// union of the static OMNIHUB_ALERT_* env channels and the DB-backed
// channel pool (managed from the admin UI), resolved live on each
// delivery. Returns nil only when there are no env channels AND no DB
// pool (alerting fully off). pool resolves account ids to names.
func buildAlerter(pool *account.Pool, channelPool *alertchannel.Pool) *alert.Alerter {
	envNotifiers := alert.Config{
		WebhookURL:  os.Getenv("OMNIHUB_ALERT_WEBHOOK_URL"),
		FeishuURL:   os.Getenv("OMNIHUB_ALERT_FEISHU_URL"),
		DingTalkURL: os.Getenv("OMNIHUB_ALERT_DINGTALK_URL"),
	}.Notifiers()
	if len(envNotifiers) == 0 && channelPool == nil {
		return nil
	}
	source := func() []alert.Notifier {
		ns := make([]alert.Notifier, 0, len(envNotifiers)+2)
		ns = append(ns, envNotifiers...)
		if channelPool != nil {
			for _, ch := range channelPool.Enabled() {
				if n, ok := alert.NotifierFor(ch.Type, ch.URL); ok {
					ns = append(ns, n)
				}
			}
		}
		return ns
	}
	return alert.New(source, pool, loadAlertThrottle())
}

// loadAlertThrottle parses OMNIHUB_ALERT_THROTTLE (a Go duration); 0 lets
// the alert package apply its default flap-suppression window.
func loadAlertThrottle() time.Duration {
	raw := os.Getenv("OMNIHUB_ALERT_THROTTLE")
	if raw == "" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		slog.Warn("OMNIHUB_ALERT_THROTTLE invalid; using default", "value", raw)
		return 0
	}
	return d
}

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

// loadHealthProbeConfig builds the active-health-probe configuration.
//
//   - OMNIHUB_HEALTH_PROBE_ENABLED      bool ("true"/"false"); default false.
//     The global default for accounts that don't override via the
//     health_probe_enabled column. The prober loop always runs so a
//     per-account true is still honoured.
//   - OMNIHUB_HEALTH_PROBE_INTERVAL     Go duration; default 60s, min 10s.
//   - OMNIHUB_HEALTH_PROBE_CONCURRENCY  integer; default 4, clamped 1..16.
//   - OMNIHUB_HEALTH_PROBE_RED_THRESHOLD integer; consecutive red probes.
//   - OMNIHUB_HEALTH_PROBE_GREEN_THRESHOLD integer; consecutive green probes.
//   - OMNIHUB_HEALTH_PROBE_TIMEOUT      Go duration; per-probe timeout.
//   - OMNIHUB_HEALTH_PROBE_SLOW_THRESHOLD Go duration; green probe above
//     this latency is treated as yellow and will not eject a supplier.
func loadHealthProbeConfig() health.ProbeConfig {
	cfg := health.DefaultProbeConfig()

	if v := os.Getenv("OMNIHUB_HEALTH_PROBE_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.GlobalDefault = b
		} else {
			slog.Warn("OMNIHUB_HEALTH_PROBE_ENABLED invalid; using default",
				"value", v, "default", cfg.GlobalDefault)
		}
	}

	if v := os.Getenv("OMNIHUB_HEALTH_PROBE_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 10*time.Second {
			cfg.Interval = d
		} else {
			slog.Warn("OMNIHUB_HEALTH_PROBE_INTERVAL invalid or < 10s; using default",
				"value", v, "default", cfg.Interval)
		}
	}

	if v := os.Getenv("OMNIHUB_HEALTH_PROBE_CONCURRENCY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 16 {
			cfg.Concurrency = n
		} else {
			slog.Warn("OMNIHUB_HEALTH_PROBE_CONCURRENCY invalid; using default",
				"value", v, "default", cfg.Concurrency)
		}
	}

	if v := os.Getenv("OMNIHUB_HEALTH_PROBE_RED_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			cfg.RedThreshold = n
		} else {
			slog.Warn("OMNIHUB_HEALTH_PROBE_RED_THRESHOLD invalid; using default",
				"value", v, "default", cfg.RedThreshold)
		}
	}

	if v := os.Getenv("OMNIHUB_HEALTH_PROBE_GREEN_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			cfg.GreenThreshold = n
		} else {
			slog.Warn("OMNIHUB_HEALTH_PROBE_GREEN_THRESHOLD invalid; using default",
				"value", v, "default", cfg.GreenThreshold)
		}
	}

	if v := os.Getenv("OMNIHUB_HEALTH_PROBE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Timeout = d
		} else {
			slog.Warn("OMNIHUB_HEALTH_PROBE_TIMEOUT invalid; using default",
				"value", v, "default", cfg.Timeout)
		}
	}

	if v := os.Getenv("OMNIHUB_HEALTH_PROBE_SLOW_THRESHOLD"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.SlowThreshold = d
		} else {
			slog.Warn("OMNIHUB_HEALTH_PROBE_SLOW_THRESHOLD invalid; using default",
				"value", v, "default", cfg.SlowThreshold)
		}
	}

	return health.NormalizeProbeConfig(cfg)
}

func loadFailoverMaxAttempts() int {
	const def = 3
	v := os.Getenv("OMNIHUB_FAILOVER_MAX_ATTEMPTS")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 10 {
		slog.Warn("OMNIHUB_FAILOVER_MAX_ATTEMPTS invalid; using default",
			"value", v, "default", def)
		return def
	}
	return n
}

// spendCacheTTL bounds how long a cached per-key 24h USD total may
// drift from the authoritative DB SUM. Short enough that operator-side
// adjustments (e.g. wiping test rows) reflect quickly; long enough to
// keep hot keys off the DB. Tuneable via OMNIHUB_LIMIT_REFRESH_TTL.
const spendCacheTTL = 5 * time.Second

// buildLimiter wires the per-key limits service. Returns the
// Limiter and the shared RPMCache so the IP guard middleware can
// keep its own buckets in the same cache (namespaced by "ip:" key
// prefix). The spend cache requires a DB-backed SpendSource and
// stays nil in log-only mode, which makes the daily-USD path a
// no-op at call sites.
func buildLimiter() (*limits.Limiter, *limits.RPMCache) {
	rpm := limits.NewRPMCache()
	if pool == nil {
		return limits.New(nil, rpm), rpm
	}
	src := repository.NewMessageRequestRepo(pool)
	cache := limits.NewSpendCache(src, spendCacheTTL)
	l := limits.New(cache, rpm)
	slog.Info("per-key limits enabled",
		"spend_cache_ttl", spendCacheTTL,
		"daily_window", "24h rolling",
		"rpm_enforcement", "in-process token bucket",
	)

	// Prepaid-balance billing is opt-in (OMNIHUB_BILLING_ENABLED). Off by
	// default so deploying the code never starts rejecting existing portal
	// users (who would otherwise have a zero balance). Balance = lifetime
	// wallet credits minus lifetime request cost, cached like spend.
	if billingEnabled() {
		// Admission balance must reflect what Charge actually draws down:
		// active plan credit first, then the pay-as-you-go wallet. Reusing
		// the billing store keeps the gate and the charge path in lockstep,
		// so a plan-only user (no wallet top-up) is not wrongly rejected.
		billingStore := billing.NewRepositoryStore(
			repository.NewPlanRepo(pool),
			repository.NewWalletRepo(pool),
			src,
		)
		balSrc := limits.BalanceFunc(billing.New(billingStore).AvailableBalance)
		balanceGuard = limits.NewBalanceGuard(balSrc, spendCacheTTL)
		l.SetBalanceGuard(balanceGuard)
		slog.Info("prepaid balance billing enabled", "balance_cache_ttl", spendCacheTTL)
	}
	return l, rpm
}

// billingEnabled reports whether prepaid-balance enforcement is on.
func billingEnabled() bool {
	b, _ := strconv.ParseBool(os.Getenv("OMNIHUB_BILLING_ENABLED"))
	return b
}

// buildDriverRegistry registers every built-in driver. Adding a new
// driver in code means appending one line here.
func buildDriverRegistry() *provider.Registry {
	reg := provider.NewRegistry()
	reg.MustRegister(anthropic.New())
	reg.MustRegister(claudeplatform.New())
	reg.MustRegister(openai.New())
	reg.MustRegister(codex.New())
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

	repo := repository.NewAccountRepo(pool, accountCipher)

	// Migrate any legacy plaintext secrets to encrypted form before the
	// pool loads. Idempotent; a no-op when encryption is disabled.
	if n, err := repo.ReencryptSecrets(ctx); err != nil {
		return fmt.Errorf("re-encrypt account secrets: %w", err)
	} else if n > 0 {
		slog.Info("account secret encryption migration complete", "rows_migrated", n)
	}

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
