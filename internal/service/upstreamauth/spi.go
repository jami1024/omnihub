// Package upstreamauth hosts the cold-path plugin interface that owns
// an upstream account's authentication lifecycle (login, credential
// import, token refresh, validation, revocation) plus the TokenManager
// that keeps OAuth-backed accounts fresh at request time.
//
// Plugins deal ONLY with auth material. They never see request bodies,
// never pick accounts, never touch billing or streaming — that is the
// resolver/forwarder/driver's territory (see
// docs/architecture/upstream-oauth-plugins.md §7).
package upstreamauth

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrNotSupported is returned by lifecycle methods a plugin does not
// implement (e.g. browser OAuth on an import-only plugin).
var ErrNotSupported = errors.New("upstreamauth: operation not supported by this plugin")

// Metadata describes a plugin for the admin UI and API.
type Metadata struct {
	Name               string   `json:"name"`
	DisplayName        string   `json:"display_name"`
	SupportedProviders []string `json:"supported_providers"`
	AuthMethods        []string `json:"auth_methods"`
	Experimental       bool     `json:"experimental"`
}

// AccountProfile is the authenticated upstream identity a plugin
// extracts during import/refresh/validate. Display and attribution
// only — never forwarded upstream.
type AccountProfile struct {
	Subject string `json:"subject"` // stable upstream account id
	Email   string `json:"email"`
	Plan    string `json:"plan"`
}

// TokenBundle is the result of a successful import, callback exchange
// or refresh: a full replacement credential set plus what the plugin
// learned about expiry and identity. Credentials follow the standard
// field names from the design doc §6 (access_token, refresh_token,
// expires_at, account_id, ...).
type TokenBundle struct {
	Credentials map[string]string
	ExpiresAt   *time.Time      // nil = non-expiring / unknown
	Profile     *AccountProfile // nil = identity unchanged
}

// BeginAuthRequest / BeginAuthResponse drive a browser or device-code
// login (not used by import-only plugins).
type BeginAuthRequest struct {
	AccountID   int64
	RedirectURI string
}

type BeginAuthResponse struct {
	AuthorizeURL string
	State        string
	CodeVerifier string // PKCE verifier the caller must hold for the callback
}

// CallbackRequest carries the OAuth callback parameters back to the
// plugin for the code exchange.
type CallbackRequest struct {
	Code         string
	State        string
	CodeVerifier string
	RedirectURI  string
	ProxyURL     string
}

// ImportCredentialsRequest carries raw pasted credentials (e.g. the
// content of ~/.codex/auth.json) for parsing into a TokenBundle.
type ImportCredentialsRequest struct {
	Payload []byte
}

// RefreshRequest carries the stored (decrypted) credentials of an
// account whose token should be renewed. ProxyURL, when set, must be
// honoured so IP-bound subscription accounts refresh through the same
// egress as their model traffic.
type RefreshRequest struct {
	Credentials map[string]string
	ProxyURL    string
}

// ValidateRequest asks the plugin to (re-)derive the account profile
// from stored credentials.
type ValidateRequest struct {
	Credentials map[string]string
	ProxyURL    string
}

// RevokeRequest asks the plugin to invalidate the stored credentials
// upstream where the provider supports it.
type RevokeRequest struct {
	Credentials map[string]string
	ProxyURL    string
}

// Provider is the upstream auth plugin SPI (UpstreamAuthProvider in
// the design doc). Implementations embed Unimplemented and override
// the methods they support.
type Provider interface {
	Metadata(ctx context.Context) (*Metadata, error)
	BeginAuth(ctx context.Context, req *BeginAuthRequest) (*BeginAuthResponse, error)
	ExchangeCallback(ctx context.Context, req *CallbackRequest) (*TokenBundle, error)
	ImportCredentials(ctx context.Context, req *ImportCredentialsRequest) (*TokenBundle, error)
	Refresh(ctx context.Context, req *RefreshRequest) (*TokenBundle, error)
	Validate(ctx context.Context, req *ValidateRequest) (*AccountProfile, error)
	Revoke(ctx context.Context, req *RevokeRequest) error
}

// Unimplemented provides ErrNotSupported defaults for every lifecycle
// method so plugins only implement what they actually support.
type Unimplemented struct{}

func (Unimplemented) BeginAuth(context.Context, *BeginAuthRequest) (*BeginAuthResponse, error) {
	return nil, ErrNotSupported
}

func (Unimplemented) ExchangeCallback(context.Context, *CallbackRequest) (*TokenBundle, error) {
	return nil, ErrNotSupported
}

func (Unimplemented) ImportCredentials(context.Context, *ImportCredentialsRequest) (*TokenBundle, error) {
	return nil, ErrNotSupported
}

func (Unimplemented) Refresh(context.Context, *RefreshRequest) (*TokenBundle, error) {
	return nil, ErrNotSupported
}

func (Unimplemented) Validate(context.Context, *ValidateRequest) (*AccountProfile, error) {
	return nil, ErrNotSupported
}

func (Unimplemented) Revoke(context.Context, *RevokeRequest) error {
	return ErrNotSupported
}

// Registry maps plugin names (accounts.auth_plugin values) to their
// implementations.
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]Provider
}

func NewRegistry() *Registry {
	return &Registry{plugins: make(map[string]Provider)}
}

func (r *Registry) Register(name string, p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[name] = p
}

func (r *Registry) Get(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[name]
	return p, ok
}

// List returns the metadata of every registered plugin, sorted by name
// for stable API output.
func (r *Registry) List(ctx context.Context) []Metadata {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Metadata, 0, len(r.plugins))
	for name, p := range r.plugins {
		md, err := p.Metadata(ctx)
		if err != nil || md == nil {
			md = &Metadata{Name: name}
		}
		out = append(out, *md)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
