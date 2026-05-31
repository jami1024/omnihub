// Package admin hosts the HTTP handlers that back the web admin UI.
//
// All responses use the same JSON envelope:
//
//	success → { ... payload ... }                  (200/201)
//	error   → { "error": { "message", "type", "code" } }
//
// The single error shape lets the React SPA centralise error rendering
// in one fetch wrapper. Both /admin/api/login (open) and the
// AdminAuthenticator-guarded endpoints use it.
package admin

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// writeError emits the canonical admin-API error envelope.
func writeError(c *gin.Context, status int, code, msg string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": msg,
			"type":    code,
			"code":    code,
		},
	})
}

// writeBadRequest is a convenience for 400 with a "bad_request" code.
func writeBadRequest(c *gin.Context, msg string) {
	writeError(c, http.StatusBadRequest, "bad_request", msg)
}

// writeInternal is a convenience for 500 with an "internal_error" code.
func writeInternal(c *gin.Context, msg string) {
	writeError(c, http.StatusInternalServerError, "internal_error", msg)
}
