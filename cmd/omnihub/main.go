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

	"github.com/jami1024/omnihub/internal/handler/gateway"
	"github.com/jami1024/omnihub/internal/service/forward"
	"github.com/jami1024/omnihub/internal/service/provider"
	"github.com/jami1024/omnihub/internal/service/provider/drivers/anthropic"
	"github.com/jami1024/omnihub/internal/service/provider/drivers/claudeplatform"
)

// Build info populated by the linker via -ldflags (see Makefile).
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	addr := os.Getenv("OMNIHUB_LISTEN")
	if addr == "" {
		addr = ":8080"
	}

	gin.SetMode(gin.ReleaseMode)
	r := newRouter()

	server := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		// WriteTimeout intentionally left at 0; streaming responses may run
		// for tens of seconds and must not be killed by the server clock.
		IdleTimeout: 120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
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

func newRouter() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/healthz", handleHealth)
	r.GET("/readyz", handleReady)
	r.GET("/version", handleVersion)

	mountGatewayRoutes(r)

	return r
}

// mountGatewayRoutes wires the LLM forwarding endpoints onto r.
//
// MVP behaviour: a single upstream account is selected by environment
// variables, with Claude Platform on AWS taking precedence over direct
// Anthropic when both are configured.
//
//   - Claude Platform on AWS — requires:
//     OMNIHUB_CLAUDE_PLATFORM_API_KEY
//     OMNIHUB_CLAUDE_PLATFORM_REGION
//     OMNIHUB_CLAUDE_PLATFORM_WORKSPACE_ID
//   - Direct Anthropic — requires:
//     OMNIHUB_ANTHROPIC_API_KEY
//
// If neither is configured, /v1/messages is not mounted and only the
// health endpoints remain live.
func mountGatewayRoutes(r *gin.Engine) {
	driver, account, ok := pickUpstream()
	if !ok {
		return
	}

	forwarder := forward.New(nil)
	r.POST("/v1/messages", gateway.AnthropicMessagesHandler(forwarder, driver, account))

	slog.Info("gateway mounted",
		"path", "/v1/messages",
		"driver", driver.Name(),
		"account", account.Name,
	)
}

// pickUpstream chooses the upstream driver+account based on env vars.
// Returns ok=false when nothing is configured.
func pickUpstream() (provider.Driver, *provider.Account, bool) {
	if cpKey := os.Getenv("OMNIHUB_CLAUDE_PLATFORM_API_KEY"); cpKey != "" {
		region := os.Getenv("OMNIHUB_CLAUDE_PLATFORM_REGION")
		workspace := os.Getenv("OMNIHUB_CLAUDE_PLATFORM_WORKSPACE_ID")
		if region == "" || workspace == "" {
			slog.Error("OMNIHUB_CLAUDE_PLATFORM_API_KEY is set but REGION or WORKSPACE_ID is missing; /v1/messages disabled")
			return nil, nil, false
		}
		return claudeplatform.New(), &provider.Account{
			Name:     "claude-platform-default",
			Provider: claudeplatform.DriverName,
			Credentials: map[string]string{
				"api_key":      cpKey,
				"aws_region":   region,
				"workspace_id": workspace,
			},
		}, true
	}

	if apiKey := os.Getenv("OMNIHUB_ANTHROPIC_API_KEY"); apiKey != "" {
		return anthropic.New(), &provider.Account{
			Name:        "anthropic-default",
			Provider:    anthropic.DriverName,
			Credentials: map[string]string{"api_key": apiKey},
		}, true
	}

	slog.Warn("no upstream credentials configured (OMNIHUB_ANTHROPIC_API_KEY or OMNIHUB_CLAUDE_PLATFORM_*); /v1/messages disabled")
	return nil, nil, false
}

func handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func handleReady(c *gin.Context) {
	// Will eventually verify DB / Redis / plugin connectivity.
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}

func handleVersion(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"version": version,
		"commit":  commit,
		"date":    date,
	})
}
