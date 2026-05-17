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
// MVP behaviour: a single Anthropic account is read from
// OMNIHUB_ANTHROPIC_API_KEY. If the variable is empty, gateway
// endpoints are skipped and only the health endpoints stay live.
func mountGatewayRoutes(r *gin.Engine) {
	apiKey := os.Getenv("OMNIHUB_ANTHROPIC_API_KEY")
	if apiKey == "" {
		slog.Warn("OMNIHUB_ANTHROPIC_API_KEY not set; /v1/messages disabled")
		return
	}

	driver := anthropic.New()

	account := &provider.Account{
		Name:        "default",
		Provider:    "anthropic",
		Credentials: map[string]string{"api_key": apiKey},
	}

	forwarder := forward.New(nil)
	r.POST("/v1/messages", gateway.AnthropicMessagesHandler(forwarder, driver, account))

	slog.Info("anthropic gateway mounted", "path", "/v1/messages")
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
