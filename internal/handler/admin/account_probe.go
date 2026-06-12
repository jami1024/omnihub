package admin

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/service/provider"
)

// testTimeout bounds a connectivity probe so a hung upstream can't pin
// the admin request.
const testTimeout = 15 * time.Second

// runTest validates the URL, looks up the driver, and runs its Tester
// (if any), writing the classified result. Shared by both test handlers.
func runTest(c *gin.Context, registry *provider.Registry, acct *provider.Account) {
	if err := provider.ValidateUpstreamURL(acct.BaseURL); err != nil {
		writeBadRequest(c, "base URL rejected: "+err.Error())
		return
	}
	driver, ok := registry.Get(acct.Provider)
	if !ok {
		writeBadRequest(c, "unknown provider type: "+acct.Provider)
		return
	}
	tester, ok := driver.(provider.Tester)
	if !ok {
		c.JSON(http.StatusOK, provider.TestResult{
			Status:  provider.TestYellow,
			Message: "connectivity test isn't available for the " + acct.Provider + " provider type",
		})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), testTimeout)
	defer cancel()
	c.JSON(http.StatusOK, tester.Test(ctx, acct))
}

// TestAccountHandler handles POST /admin/api/accounts/test — probe the
// upstream for the form's provider/base_url/credentials before saving.
func TestAccountHandler(registry *provider.Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in accountInput
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		in.Provider = strings.TrimSpace(in.Provider)
		if in.Provider == "" {
			writeBadRequest(c, "provider is required")
			return
		}
		if len(in.Credentials) == 0 {
			writeBadRequest(c, "credentials are required to test")
			return
		}
		runTest(c, registry, &provider.Account{
			Provider:    in.Provider,
			BaseURL:     strings.TrimSpace(in.BaseURL),
			Credentials: in.Credentials,
		})
	}
}

// QuotaAccountByIDHandler handles GET /admin/api/accounts/:id/quota —
// query a subscription account's remaining quota windows through its
// driver's QuotaProber (codex wham/usage, claude /api/oauth/usage).
func QuotaAccountByIDHandler(store accountStore, registry *provider.Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		acct, _, err := store.GetByID(c.Request.Context(), id)
		if err != nil {
			writeError(c, http.StatusNotFound, "not_found", "account not found")
			return
		}
		if err := provider.ValidateUpstreamURL(acct.BaseURL); err != nil {
			writeBadRequest(c, "base URL rejected: "+err.Error())
			return
		}
		driver, ok := registry.Get(acct.Provider)
		if !ok {
			writeBadRequest(c, "unknown provider type: "+acct.Provider)
			return
		}
		prober, ok := driver.(provider.QuotaProber)
		if !ok {
			writeBadRequest(c, "quota query isn't available for the "+acct.Provider+" provider type")
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), testTimeout)
		defer cancel()
		info, qerr := prober.Quota(ctx, acct)
		if qerr != nil {
			writeError(c, http.StatusBadGateway, "quota_failed", qerr.Error())
			return
		}
		c.JSON(http.StatusOK, info)
	}
}

// TestAccountByIDHandler handles POST /admin/api/accounts/:id/test —
// probe an existing account using its stored (write-only) credentials,
// so an operator can re-check a live account without re-entering secrets.
func TestAccountByIDHandler(store accountStore, registry *provider.Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		acct, _, err := store.GetByID(c.Request.Context(), id)
		if err != nil {
			writeError(c, http.StatusNotFound, "not_found", "account not found")
			return
		}
		runTest(c, registry, acct)
	}
}
