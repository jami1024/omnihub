// Package admin contains the auth primitives for the web admin UI:
// the User type, bcrypt password helpers, and a small HS256 JWT issuer
// for stateless session tokens.
//
// The package is intentionally narrow — it knows nothing about HTTP,
// pgx, or gin. Repository wiring lives in internal/repository, the
// login HTTP handler in internal/handler/admin, and the middleware that
// verifies tokens on every /admin/api/* request in
// internal/service/guard.
package admin

import "time"

// User is one row in admin_users. PasswordHash is bcrypt-encoded (60
// chars including algorithm/cost/salt); the cleartext is never held.
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	Enabled      bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
