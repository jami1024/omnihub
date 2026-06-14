package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/provider"
)

// proxyStore is the slice of repository.ProxyRepo the proxy handlers
// depend on, narrowed for testability.
type proxyStore interface {
	ListAll(ctx context.Context) ([]*provider.Proxy, error)
	GetByID(ctx context.Context, id int64) (*provider.Proxy, error)
	Insert(ctx context.Context, p repository.ProxyParams) (int64, error)
	Update(ctx context.Context, id int64, p repository.ProxyParams) error
	Delete(ctx context.Context, id int64) error
}

// proxyDTO is the redacted JSON shape returned to the SPA. The password
// is never returned — only whether one is set.
type proxyDTO struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Protocol      string `json:"protocol"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Username      string `json:"username"`
	HasPassword   bool   `json:"has_password"`
	Status        string `json:"status"`
	ExpiresAt     *int64 `json:"expires_at"`
	FallbackMode  string `json:"fallback_mode"`
	BackupProxyID *int64 `json:"backup_proxy_id"`
}

func proxyToDTO(p *provider.Proxy) proxyDTO {
	var exp *int64
	if p.ExpiresAt != nil {
		u := p.ExpiresAt.Unix()
		exp = &u
	}
	return proxyDTO{
		ID:            p.ID,
		Name:          p.Name,
		Protocol:      p.Protocol,
		Host:          p.Host,
		Port:          p.Port,
		Username:      p.Username,
		HasPassword:   p.Password != "",
		Status:        p.Status,
		ExpiresAt:     exp,
		FallbackMode:  p.FallbackMode,
		BackupProxyID: p.BackupProxyID,
	}
}

// proxyInput is the create/update body. Password is a pointer so an
// edit can omit it (nil = keep the stored secret); "" explicitly clears
// it. ExpiresAt is unix seconds (null = never).
type proxyInput struct {
	Name          string  `json:"name"`
	Protocol      string  `json:"protocol"`
	Host          string  `json:"host"`
	Port          int     `json:"port"`
	Username      string  `json:"username"`
	Password      *string `json:"password"`
	Status        string  `json:"status"`
	ExpiresAt     *int64  `json:"expires_at"`
	FallbackMode  string  `json:"fallback_mode"`
	BackupProxyID *int64  `json:"backup_proxy_id"`
}

var validProxyProtocols = map[string]bool{"http": true, "https": true, "socks5": true, "socks5h": true}
var validProxyStatuses = map[string]bool{"active": true, "disabled": true}
var validFallbackModes = map[string]bool{"none": true, "direct": true, "proxy": true}

// validateProxyInput trims and bounds-checks the input, returning the
// cleaned values as ProxyParams (Password left for the caller) or an
// error message.
func validateProxyInput(in *proxyInput) (repository.ProxyParams, string) {
	name := strings.TrimSpace(in.Name)
	host := strings.TrimSpace(in.Host)
	if name == "" || host == "" {
		return repository.ProxyParams{}, "name and host are required"
	}
	if in.Port < 1 || in.Port > 65535 {
		return repository.ProxyParams{}, "port must be between 1 and 65535"
	}
	protocol := strings.TrimSpace(in.Protocol)
	if protocol == "" {
		protocol = "http"
	}
	if !validProxyProtocols[protocol] {
		return repository.ProxyParams{}, "protocol must be http, https, socks5, or socks5h"
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = "active"
	}
	if !validProxyStatuses[status] {
		return repository.ProxyParams{}, "status must be active or disabled"
	}
	fallback := strings.TrimSpace(in.FallbackMode)
	if fallback == "" {
		fallback = "none"
	}
	if !validFallbackModes[fallback] {
		return repository.ProxyParams{}, "fallback_mode must be none, direct, or proxy"
	}
	if fallback == "proxy" && in.BackupProxyID == nil {
		return repository.ProxyParams{}, "fallback_mode 'proxy' requires backup_proxy_id"
	}
	params := repository.ProxyParams{
		Name:          name,
		Protocol:      protocol,
		Host:          host,
		Port:          in.Port,
		Username:      strings.TrimSpace(in.Username),
		Status:        status,
		FallbackMode:  fallback,
		BackupProxyID: in.BackupProxyID,
	}
	if in.ExpiresAt != nil {
		t := time.Unix(*in.ExpiresAt, 0).UTC()
		params.ExpiresAt = &t
	}
	return params, ""
}

// ListProxiesHandler returns GET /admin/api/proxies → {"proxies":[…]}.
func ListProxiesHandler(store proxyStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		proxies, err := store.ListAll(c.Request.Context())
		if err != nil {
			slog.Error("admin: list proxies failed", "err", err.Error())
			writeInternal(c, "could not list proxies")
			return
		}
		out := make([]proxyDTO, len(proxies))
		for i, p := range proxies {
			out[i] = proxyToDTO(p)
		}
		c.JSON(http.StatusOK, gin.H{"proxies": out})
	}
}

// CreateProxyHandler handles POST /admin/api/proxies.
func CreateProxyHandler(store proxyStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in proxyInput
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		params, verr := validateProxyInput(&in)
		if verr != "" {
			writeBadRequest(c, verr)
			return
		}
		if in.Password != nil {
			params.Password = *in.Password
		}
		id, err := store.Insert(c.Request.Context(), params)
		if err != nil {
			if errors.Is(err, repository.ErrProxyNameTaken) {
				writeError(c, http.StatusConflict, "name_taken", "a proxy named "+params.Name+" already exists")
				return
			}
			slog.Error("admin: create proxy failed", "err", err.Error())
			writeInternal(c, "could not create proxy")
			return
		}
		p, err := store.GetByID(c.Request.Context(), id)
		if err != nil {
			c.JSON(http.StatusCreated, gin.H{"id": id})
			return
		}
		slog.Info("admin: proxy created", "id", id, "name", params.Name, "admin", adminActor(c))
		c.JSON(http.StatusCreated, proxyToDTO(p))
	}
}

// UpdateProxyHandler handles PATCH /admin/api/proxies/:id.
func UpdateProxyHandler(store proxyStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		var in proxyInput
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		params, verr := validateProxyInput(&in)
		if verr != "" {
			writeBadRequest(c, verr)
			return
		}
		// Password: nil = keep the stored secret; non-nil replaces it.
		if in.Password != nil {
			params.Password = *in.Password
		} else {
			existing, err := store.GetByID(c.Request.Context(), id)
			if err != nil {
				writeError(c, http.StatusNotFound, "not_found", "proxy not found")
				return
			}
			params.Password = existing.Password
		}
		if err := store.Update(c.Request.Context(), id, params); err != nil {
			switch {
			case errors.Is(err, repository.ErrProxyNotFound):
				writeError(c, http.StatusNotFound, "not_found", "proxy not found")
			case errors.Is(err, repository.ErrProxyNameTaken):
				writeError(c, http.StatusConflict, "name_taken", "a proxy named "+params.Name+" already exists")
			default:
				slog.Error("admin: update proxy failed", "id", id, "err", err.Error())
				writeInternal(c, "could not update proxy")
			}
			return
		}
		p, err := store.GetByID(c.Request.Context(), id)
		if err != nil {
			writeInternal(c, "proxy updated but could not be re-read")
			return
		}
		slog.Info("admin: proxy updated", "id", id, "name", params.Name, "admin", adminActor(c))
		c.JSON(http.StatusOK, proxyToDTO(p))
	}
}

// DeleteProxyHandler handles DELETE /admin/api/proxies/:id → 204.
func DeleteProxyHandler(store proxyStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		if err := store.Delete(c.Request.Context(), id); err != nil {
			if errors.Is(err, repository.ErrProxyNotFound) {
				writeError(c, http.StatusNotFound, "not_found", "proxy not found")
				return
			}
			slog.Error("admin: delete proxy failed", "id", id, "err", err.Error())
			writeInternal(c, "could not delete proxy")
			return
		}
		slog.Info("admin: proxy deleted", "id", id, "admin", adminActor(c))
		c.Status(http.StatusNoContent)
	}
}

// proxyTestTarget is a lightweight, globally reachable endpoint used to
// verify a proxy actually carries traffic. Any HTTP response (even 204)
// means the proxy connected.
const proxyTestTarget = "https://www.gstatic.com/generate_204"

// TestProxyHandler handles POST /admin/api/proxies/:id/test — sends a
// probe request through the stored proxy and classifies the outcome.
func TestProxyHandler(store proxyStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		p, err := store.GetByID(c.Request.Context(), id)
		if err != nil {
			writeError(c, http.StatusNotFound, "not_found", "proxy not found")
			return
		}
		u, err := url.Parse(p.URL())
		if err != nil || u.Host == "" {
			c.JSON(http.StatusOK, gin.H{"status": "red", "message": "invalid proxy URL"})
			return
		}
		client := &http.Client{
			Timeout:   testTimeout,
			Transport: &http.Transport{Proxy: http.ProxyURL(u)},
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), testTimeout)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, proxyTestTarget, nil)
		start := time.Now()
		resp, err := client.Do(req)
		latency := time.Since(start).Milliseconds()
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"status": "red", "latency_ms": latency,
				"message": fmt.Sprintf("proxy unreachable: %v", err)})
			return
		}
		_ = resp.Body.Close()
		c.JSON(http.StatusOK, gin.H{"status": "green", "http_status": resp.StatusCode,
			"latency_ms": latency, "message": "proxy reachable"})
	}
}
