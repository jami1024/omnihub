package admin

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
)

// blockedIPStore is the slice of repository.BlockedIPRepo the blocked-IP
// handlers depend on, narrowed for testability.
type blockedIPStore interface {
	ListRecords(ctx context.Context) ([]repository.BlockedIPRecord, error)
	Insert(ctx context.Context, ip string, p repository.BlockedIPParams, createdBy string) error
	Update(ctx context.Context, ip string, p repository.BlockedIPParams) error
	Delete(ctx context.Context, ip string) error
}

// blockedIPDTO is the JSON shape returned to the SPA. `blocked` is the
// derived hard-block flag (all three limits null → 403); when any limit
// is set the IP is allowed but capped.
type blockedIPDTO struct {
	IP              string    `json:"ip"`
	Reason          string    `json:"reason"`
	RPMLimit        *int      `json:"rpm_limit"`
	TPMLimit        *int64    `json:"tpm_limit"`
	ConcurrentLimit *int      `json:"concurrent_limit"`
	Blocked         bool      `json:"blocked"`
	CreatedAt       time.Time `json:"created_at"`
	CreatedBy       string    `json:"created_by"`
}

func toBlockedIPDTO(rec repository.BlockedIPRecord) blockedIPDTO {
	return blockedIPDTO{
		IP:              rec.IP,
		Reason:          rec.Reason,
		RPMLimit:        rec.RPMLimit,
		TPMLimit:        rec.TPMLimit,
		ConcurrentLimit: rec.ConcurrentLimit,
		Blocked:         rec.RPMLimit == nil && rec.TPMLimit == nil && rec.ConcurrentLimit == nil,
		CreatedAt:       rec.CreatedAt,
		CreatedBy:       rec.CreatedBy,
	}
}

// blockedIPInput is the create/update body. On create `ip` is required;
// on update the IP comes from the path and the body field is ignored.
// All limits are optional pointers — omit every limit for a hard block.
type blockedIPInput struct {
	IP              string `json:"ip"`
	Reason          string `json:"reason"`
	RPMLimit        *int   `json:"rpm_limit"`
	TPMLimit        *int64 `json:"tpm_limit"`
	ConcurrentLimit *int   `json:"concurrent_limit"`
}

// params projects the input onto the repository params, after the
// caller has validated the limits.
func (in *blockedIPInput) params() repository.BlockedIPParams {
	return repository.BlockedIPParams{
		Reason:          strings.TrimSpace(in.Reason),
		RPMLimit:        in.RPMLimit,
		TPMLimit:        in.TPMLimit,
		ConcurrentLimit: in.ConcurrentLimit,
	}
}

// validateLimits rejects non-positive caps (the DB CHECK enforces the
// same, but a 400 with a clear message beats a raw constraint error).
func (in *blockedIPInput) validateLimits(c *gin.Context) bool {
	if in.RPMLimit != nil && *in.RPMLimit <= 0 {
		writeBadRequest(c, "rpm_limit must be greater than 0 (omit it for no cap)")
		return false
	}
	if in.TPMLimit != nil && *in.TPMLimit <= 0 {
		writeBadRequest(c, "tpm_limit must be greater than 0 (omit it for no cap)")
		return false
	}
	if in.ConcurrentLimit != nil && *in.ConcurrentLimit <= 0 {
		writeBadRequest(c, "concurrent_limit must be greater than 0 (omit it for no cap)")
		return false
	}
	return true
}

// ListBlockedIPsHandler returns GET /admin/api/blocked-ips.
func ListBlockedIPsHandler(store blockedIPStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		recs, err := store.ListRecords(c.Request.Context())
		if err != nil {
			slog.Error("admin: list blocked ips failed", "err", err.Error())
			writeInternal(c, "could not list blocked IPs")
			return
		}
		out := make([]blockedIPDTO, len(recs))
		for i, rec := range recs {
			out[i] = toBlockedIPDTO(rec)
		}
		c.JSON(http.StatusOK, gin.H{"blocked_ips": out})
	}
}

// CreateBlockedIPHandler handles POST /admin/api/blocked-ips.
func CreateBlockedIPHandler(store blockedIPStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in blockedIPInput
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		ip := strings.TrimSpace(in.IP)
		if net.ParseIP(ip) == nil {
			writeBadRequest(c, "ip must be a valid IPv4 or IPv6 address")
			return
		}
		if !in.validateLimits(c) {
			return
		}

		if err := store.Insert(c.Request.Context(), ip, in.params(), adminActor(c)); err != nil {
			if errors.Is(err, repository.ErrBlockedIPExists) {
				writeError(c, http.StatusConflict, "already_exists",
					ip+" is already in the blocklist")
				return
			}
			slog.Error("admin: create blocked ip failed", "ip", ip, "err", err.Error())
			writeInternal(c, "could not block IP")
			return
		}
		slog.Info("admin: ip blocked", "ip", ip, "admin", adminActor(c))
		c.JSON(http.StatusCreated, gin.H{"ip": ip})
	}
}

// UpdateBlockedIPHandler handles PATCH /admin/api/blocked-ips/:ip.
func UpdateBlockedIPHandler(store blockedIPStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip, ok := parseIPParam(c)
		if !ok {
			return
		}
		var in blockedIPInput
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		if !in.validateLimits(c) {
			return
		}

		if err := store.Update(c.Request.Context(), ip, in.params()); err != nil {
			if errors.Is(err, repository.ErrBlockedIPNotFound) {
				writeError(c, http.StatusNotFound, "not_found", "blocked IP not found")
				return
			}
			slog.Error("admin: update blocked ip failed", "ip", ip, "err", err.Error())
			writeInternal(c, "could not update blocked IP")
			return
		}
		slog.Info("admin: blocked ip updated", "ip", ip, "admin", adminActor(c))
		c.Status(http.StatusNoContent)
	}
}

// DeleteBlockedIPHandler handles DELETE /admin/api/blocked-ips/:ip → 204.
func DeleteBlockedIPHandler(store blockedIPStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip, ok := parseIPParam(c)
		if !ok {
			return
		}
		if err := store.Delete(c.Request.Context(), ip); err != nil {
			if errors.Is(err, repository.ErrBlockedIPNotFound) {
				writeError(c, http.StatusNotFound, "not_found", "blocked IP not found")
				return
			}
			slog.Error("admin: delete blocked ip failed", "ip", ip, "err", err.Error())
			writeInternal(c, "could not unblock IP")
			return
		}
		slog.Info("admin: ip unblocked", "ip", ip, "admin", adminActor(c))
		c.Status(http.StatusNoContent)
	}
}

// parseIPParam reads the :ip path segment, writing a 400 on a malformed
// address. The IP doubles as the row's primary key.
func parseIPParam(c *gin.Context) (string, bool) {
	ip := strings.TrimSpace(c.Param("ip"))
	if net.ParseIP(ip) == nil {
		writeBadRequest(c, "invalid IP address in path")
		return "", false
	}
	return ip, true
}
