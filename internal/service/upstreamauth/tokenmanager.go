package upstreamauth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/provider"
)

// Auth lifecycle statuses written by the TokenManager (subset of the
// accounts.auth_status CHECK constraint).
const (
	StatusOK            = "ok"
	StatusRefreshFailed = "refresh_failed"
	StatusLoginRequired = "login_required"
)

// ErrLoginRequired marks a refresh failure that no retry can fix (the
// refresh token itself is dead — revoked, rotated elsewhere, or
// expired). Plugins wrap it so the TokenManager parks the account as
// login_required instead of refresh_failed.
var ErrLoginRequired = errors.New("upstreamauth: re-login required")

const (
	// DefaultRefreshWindow is how far before expiry a token is renewed.
	DefaultRefreshWindow = 5 * time.Minute
	// forceRefreshDebounce suppresses force-refresh stampedes: a 401
	// retry skips the refresh when another request just rotated the
	// token successfully.
	forceRefreshDebounce = 30 * time.Second
	// failedRetryBackoff is how long the background sweeper waits
	// before re-attempting a refresh_failed account.
	failedRetryBackoff = 5 * time.Minute
)

// RefreshWindowFromEnv reads OMNIHUB_TOKEN_REFRESH_WINDOW (a Go
// duration like "5m" or "300s"), falling back to DefaultRefreshWindow.
func RefreshWindowFromEnv() time.Duration {
	raw := os.Getenv("OMNIHUB_TOKEN_REFRESH_WINDOW")
	if raw == "" {
		return DefaultRefreshWindow
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		slog.Warn("invalid OMNIHUB_TOKEN_REFRESH_WINDOW; using default",
			"value", raw, "default", DefaultRefreshWindow.String())
		return DefaultRefreshWindow
	}
	return d
}

// AccountStore is the slice of the account repository the TokenManager
// needs: a fresh decrypted read and the runtime-columns write.
type AccountStore interface {
	GetByID(ctx context.Context, id int64) (*provider.Account, bool, error)
	UpdateAuthRuntime(ctx context.Context, id int64, u repository.AuthRuntimeUpdate) error
}

// TokenManager sits between the resolver and the driver: before a
// request is dispatched to an OAuth-backed account it guarantees the
// access token has at least `window` of life left, refreshing through
// the account's auth plugin when it does not. All state lives in the
// accounts table; the in-process lock only serialises refreshes within
// this gateway instance (single-writer deployments today).
type TokenManager struct {
	store  AccountStore
	reg    *Registry
	window time.Duration

	mu    sync.Mutex
	locks map[int64]*sync.Mutex
}

// NewTokenManager wires the manager. window <= 0 selects the default.
func NewTokenManager(store AccountStore, reg *Registry, window time.Duration) *TokenManager {
	if window <= 0 {
		window = DefaultRefreshWindow
	}
	return &TokenManager{
		store:  store,
		reg:    reg,
		window: window,
		locks:  make(map[int64]*sync.Mutex),
	}
}

// EnsureFresh returns an account whose access token is good for at
// least the refresh window. Non-OAuth accounts pass through untouched.
// A failed refresh parks the account (refresh_failed / login_required)
// and returns an error so the caller can fail over to another account.
func (m *TokenManager) EnsureFresh(ctx context.Context, a *provider.Account) (*provider.Account, error) {
	if m == nil || a == nil || !a.UsesUpstreamOAuth() || a.AuthPlugin == "" {
		return a, nil
	}
	if !m.needsRefresh(a) {
		return a, nil
	}

	unlock := m.lock(a.ID)
	defer unlock()

	// Reload under the lock: another request may have refreshed while
	// we waited. A read failure is not worth failing the request over —
	// proceed with the (possibly stale) token we already hold.
	current, _, err := m.store.GetByID(ctx, a.ID)
	if err != nil || current == nil {
		slog.Warn("token refresh: reload failed; using in-memory account",
			"account", a.Name, "id", a.ID, "err", errString(err))
		current = a
	}
	if !m.needsRefresh(current) {
		return current, nil
	}
	return m.refreshLocked(ctx, current)
}

// ForceRefresh renews the token unconditionally (modulo a short
// debounce) — the 401 recovery path. The caller passes the account ID,
// not the struct, because the stored credentials may already differ
// from what the failing request used.
func (m *TokenManager) ForceRefresh(ctx context.Context, accountID int64) (*provider.Account, error) {
	if m == nil {
		return nil, fmt.Errorf("token manager not configured")
	}
	unlock := m.lock(accountID)
	defer unlock()

	current, _, err := m.store.GetByID(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("reload account %d: %w", accountID, err)
	}
	if !current.UsesUpstreamOAuth() || current.AuthPlugin == "" {
		return current, nil
	}
	// Another request may have just rotated the token (that is the
	// usual cause of a one-off 401 under concurrency) — reuse it.
	if current.AuthStatus == StatusOK && current.LastRefreshAt != nil &&
		time.Since(*current.LastRefreshAt) < forceRefreshDebounce {
		return current, nil
	}
	return m.refreshLocked(ctx, current)
}

// Start launches the background sweeper that keeps idle OAuth accounts
// fresh (and their admin-UI status truthful) even when no request
// traffic flows through them. list is typically accountPool.All.
func (m *TokenManager) Start(ctx context.Context, list func() []*provider.Account, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.sweep(ctx, list())
			}
		}
	}()
}

func (m *TokenManager) sweep(ctx context.Context, accounts []*provider.Account) {
	for _, a := range accounts {
		if !a.UsesUpstreamOAuth() || a.AuthPlugin == "" || !m.needsRefresh(a) {
			continue
		}
		switch a.AuthStatus {
		case StatusLoginRequired, "revoked", "disabled":
			// Parked pending admin action; retrying cannot help.
			continue
		case StatusRefreshFailed:
			if a.LastRefreshAt != nil && time.Since(*a.LastRefreshAt) < failedRetryBackoff {
				continue
			}
		}
		if _, err := m.EnsureFresh(ctx, a); err != nil {
			slog.Warn("background token refresh failed",
				"account", a.Name, "id", a.ID, "err", err.Error())
		}
	}
}

// needsRefresh: an OAuth token with a known expiry inside the refresh
// window. Unknown expiry (nil) is treated as non-expiring — import
// always records one, so nil only occurs on hand-crafted rows.
func (m *TokenManager) needsRefresh(a *provider.Account) bool {
	if a.AuthExpiresAt == nil {
		return false
	}
	return time.Until(*a.AuthExpiresAt) <= m.window
}

// refreshLocked performs the plugin refresh + persistence. Caller must
// hold the per-account lock.
func (m *TokenManager) refreshLocked(ctx context.Context, a *provider.Account) (*provider.Account, error) {
	plugin, ok := m.reg.Get(a.AuthPlugin)
	if !ok {
		err := fmt.Errorf("unknown auth plugin %q", a.AuthPlugin)
		m.persistFailure(ctx, a, StatusRefreshFailed, err)
		return nil, err
	}

	bundle, err := plugin.Refresh(ctx, &RefreshRequest{
		Credentials: a.Credentials,
		ProxyURL:    a.ProxyURL,
	})
	if err != nil {
		status := StatusRefreshFailed
		if errors.Is(err, ErrLoginRequired) {
			status = StatusLoginRequired
		}
		m.persistFailure(ctx, a, status, err)
		return nil, fmt.Errorf("refresh account %q: %w", a.Name, err)
	}

	now := time.Now().UTC()
	upd := repository.AuthRuntimeUpdate{
		Credentials:   bundle.Credentials,
		Status:        StatusOK,
		RefreshError:  "",
		ExpiresAt:     bundle.ExpiresAt,
		LastRefreshAt: &now,
	}
	if bundle.Profile != nil {
		upd.Subject = bundle.Profile.Subject
		upd.Email = bundle.Profile.Email
		upd.Plan = bundle.Profile.Plan
	}
	if err := m.store.UpdateAuthRuntime(ctx, a.ID, upd); err != nil {
		// The upstream accepted the refresh but we could not persist
		// it. Serve this request with the fresh in-memory token; the
		// next request will retry the write path.
		slog.Error("token refresh persisted nowhere: DB write failed",
			"account", a.Name, "id", a.ID, "err", err.Error())
	}

	fresh := *a
	fresh.Credentials = bundle.Credentials
	fresh.AuthStatus = StatusOK
	fresh.RefreshError = ""
	fresh.AuthExpiresAt = bundle.ExpiresAt
	fresh.LastRefreshAt = &now
	if bundle.Profile != nil {
		if bundle.Profile.Subject != "" {
			fresh.AuthSubject = bundle.Profile.Subject
		}
		if bundle.Profile.Email != "" {
			fresh.AuthEmail = bundle.Profile.Email
		}
		if bundle.Profile.Plan != "" {
			fresh.AuthPlan = bundle.Profile.Plan
		}
	}
	slog.Info("upstream token refreshed",
		"account", a.Name, "id", a.ID, "plugin", a.AuthPlugin,
		"expires_at", timeString(bundle.ExpiresAt))
	return &fresh, nil
}

// persistFailure records a refresh failure on the account row. The
// resolver skips non-routable statuses, so this is what actually takes
// the account out of rotation.
func (m *TokenManager) persistFailure(ctx context.Context, a *provider.Account, status string, cause error) {
	now := time.Now().UTC()
	upd := repository.AuthRuntimeUpdate{
		Status:        status,
		RefreshError:  truncate(cause.Error(), 500),
		LastRefreshAt: &now,
	}
	if err := m.store.UpdateAuthRuntime(ctx, a.ID, upd); err != nil {
		slog.Error("failed to persist token refresh failure",
			"account", a.Name, "id", a.ID, "status", status, "err", err.Error())
	}
}

func (m *TokenManager) lock(id int64) func() {
	m.mu.Lock()
	l, ok := m.locks[id]
	if !ok {
		l = &sync.Mutex{}
		m.locks[id] = l
	}
	m.mu.Unlock()
	l.Lock()
	return l.Unlock
}

func errString(err error) string {
	if err == nil {
		return "account missing"
	}
	return err.Error()
}

func timeString(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
