package admin

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/provider"
	"github.com/jami1024/omnihub/internal/service/upstreamauth"
)

// accountAuthStore is the slice of the account repository the
// credential-import endpoint needs.
type accountAuthStore interface {
	GetByID(ctx context.Context, id int64) (*provider.Account, bool, error)
	UpdateAuthRuntime(ctx context.Context, id int64, u repository.AuthRuntimeUpdate) error
}

// ListAuthPluginsHandler returns GET /admin/api/auth-plugins: the
// registered upstream auth plugins' metadata, for the account form's
// auth-method picker.
func ListAuthPluginsHandler(reg *upstreamauth.Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"plugins": reg.List(c.Request.Context())})
	}
}

// importCredentialsInput is the POST /accounts/:id/import-credentials
// body: the raw pasted credential file plus an optional plugin
// override (defaults to the account's configured auth_plugin).
type importCredentialsInput struct {
	Plugin  string `json:"plugin"`
	Payload string `json:"payload"`
}

// ImportAccountCredentialsHandler parses pasted CLI credentials (e.g.
// ~/.codex/auth.json) through the account's auth plugin and persists
// the resulting token bundle onto the existing account row — the
// account's history, limits and group membership stay intact.
func ImportAccountCredentialsHandler(store accountAuthStore, reg *upstreamauth.Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		var in importCredentialsInput
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		if strings.TrimSpace(in.Payload) == "" {
			writeBadRequest(c, "payload is required (paste the credential file content)")
			return
		}

		account, _, err := store.GetByID(c.Request.Context(), id)
		if err != nil {
			if errors.Is(err, repository.ErrAccountNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "load account: " + err.Error()})
			return
		}
		if !account.UsesUpstreamOAuth() {
			writeBadRequest(c, "account auth_type must be oauth or imported_oauth to import credentials")
			return
		}

		pluginName := strings.TrimSpace(in.Plugin)
		if pluginName == "" {
			pluginName = account.AuthPlugin
		}
		if pluginName == "" {
			writeBadRequest(c, "no auth plugin configured for this account; pass \"plugin\" explicitly")
			return
		}
		plugin, ok := reg.Get(pluginName)
		if !ok {
			writeBadRequest(c, "unknown auth plugin: "+pluginName)
			return
		}

		bundle, err := plugin.ImportCredentials(c.Request.Context(), &upstreamauth.ImportCredentialsRequest{
			Payload: []byte(in.Payload),
		})
		if err != nil {
			if errors.Is(err, upstreamauth.ErrNotSupported) {
				writeBadRequest(c, "plugin "+pluginName+" does not support credential import")
				return
			}
			writeBadRequest(c, err.Error())
			return
		}

		now := time.Now().UTC()
		upd := repository.AuthRuntimeUpdate{
			Credentials:   bundle.Credentials,
			Plugin:        pluginName,
			Status:        upstreamauth.StatusOK,
			RefreshError:  "",
			ExpiresAt:     bundle.ExpiresAt,
			LastRefreshAt: &now,
		}
		if bundle.Profile != nil {
			upd.Subject = bundle.Profile.Subject
			upd.Email = bundle.Profile.Email
			upd.Plan = bundle.Profile.Plan
		}
		if err := store.UpdateAuthRuntime(c.Request.Context(), id, upd); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "persist credentials: " + err.Error()})
			return
		}

		resp := gin.H{"auth_status": upstreamauth.StatusOK, "plugin": pluginName}
		if bundle.ExpiresAt != nil {
			resp["auth_expires_at"] = bundle.ExpiresAt.UTC().Format(time.RFC3339)
		}
		if bundle.Profile != nil {
			resp["profile"] = bundle.Profile
		}
		c.JSON(http.StatusOK, resp)
	}
}
