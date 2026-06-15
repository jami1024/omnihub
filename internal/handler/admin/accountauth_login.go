package admin

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/jami1024/omnihub/internal/repository"
	"github.com/jami1024/omnihub/internal/service/upstreamauth"
)

// oauthSessionTTL bounds how long a started browser-login may sit
// between begin and exchange before its PKCE verifier is discarded.
const oauthSessionTTL = 10 * time.Minute

// oauthSession holds the per-login PKCE state between begin and
// exchange. The verifier never leaves the gateway — only the session id
// is handed to the browser.
type oauthSession struct {
	accountID    int64
	plugin       string
	state        string
	codeVerifier string
	redirectURI  string
	proxyURL     string
	expiresAt    time.Time
}

// OAuthSessionStore is an in-memory store of in-flight browser logins.
// Single-instance only (the gateway's deployment model); a restart
// drops pending logins, which the admin simply retries.
type OAuthSessionStore struct {
	mu       sync.Mutex
	sessions map[string]oauthSession
}

// NewOAuthSessionStore returns an empty store and starts a janitor that
// evicts expired sessions every TTL.
func NewOAuthSessionStore(ctx context.Context) *OAuthSessionStore {
	s := &OAuthSessionStore{sessions: make(map[string]oauthSession)}
	go func() {
		t := time.NewTicker(oauthSessionTTL)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.gc()
			}
		}
	}()
	return s
}

func (s *OAuthSessionStore) put(id string, sess oauthSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = sess
}

func (s *OAuthSessionStore) take(id string) (oauthSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if ok {
		delete(s.sessions, id) // single-use
	}
	if ok && time.Now().After(sess.expiresAt) {
		return oauthSession{}, false
	}
	return sess, ok
}

func (s *OAuthSessionStore) gc() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sess := range s.sessions {
		if now.After(sess.expiresAt) {
			delete(s.sessions, id)
		}
	}
}

// BeginOAuthLoginHandler handles POST /admin/api/accounts/:id/oauth/begin.
// It asks the account's auth plugin for an authorize URL, stashes the
// PKCE verifier under a fresh session id, and returns the URL + session
// id for the operator to open in a browser.
func BeginOAuthLoginHandler(store accountAuthStore, reg *upstreamauth.Registry, sessions *OAuthSessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
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
			writeBadRequest(c, "account auth_type must be oauth or imported_oauth to log in")
			return
		}
		pluginName := account.AuthPlugin
		if pluginName == "" {
			writeBadRequest(c, "no auth plugin configured for this account")
			return
		}
		plugin, ok := reg.Get(pluginName)
		if !ok {
			writeBadRequest(c, "unknown auth plugin: "+pluginName)
			return
		}

		// Use the plugin's default redirect_uri (the native CLI loopback /
		// platform callback). begin and exchange must agree, so the empty
		// value flows through to the session below.
		resp, err := plugin.BeginAuth(c.Request.Context(), &upstreamauth.BeginAuthRequest{AccountID: id})
		if err != nil {
			if errors.Is(err, upstreamauth.ErrNotSupported) {
				writeBadRequest(c, "plugin "+pluginName+" does not support browser login")
				return
			}
			writeBadRequest(c, err.Error())
			return
		}

		sessionID, err := newSessionID()
		if err != nil {
			writeInternal(c, "could not start login session")
			return
		}
		sessions.put(sessionID, oauthSession{
			accountID: id, plugin: pluginName, state: resp.State,
			codeVerifier: resp.CodeVerifier, redirectURI: "",
			proxyURL: account.ProxyURL, expiresAt: time.Now().Add(oauthSessionTTL),
		})
		c.JSON(http.StatusOK, gin.H{
			"session_id":    sessionID,
			"authorize_url": resp.AuthorizeURL,
		})
	}
}

// exchangeOAuthInput is the callback body pasted back by the operator.
type exchangeOAuthInput struct {
	SessionID string `json:"session_id"`
	Code      string `json:"code"`
	State     string `json:"state"`
}

// ExchangeOAuthLoginHandler handles POST /admin/api/accounts/:id/oauth/
// exchange. It validates the session + state, exchanges the code through
// the plugin, and persists the resulting tokens onto the account — same
// account row, so history / limits / group survive.
func ExchangeOAuthLoginHandler(store accountAuthStore, reg *upstreamauth.Registry, sessions *OAuthSessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseIDParam(c)
		if !ok {
			return
		}
		var in exchangeOAuthInput
		if err := c.ShouldBindJSON(&in); err != nil {
			writeBadRequest(c, "invalid JSON: "+err.Error())
			return
		}
		in.Code = strings.TrimSpace(in.Code)
		if in.SessionID == "" || in.Code == "" {
			writeBadRequest(c, "session_id and code are required")
			return
		}
		sess, ok := sessions.take(in.SessionID)
		if !ok {
			writeBadRequest(c, "login session not found or expired; start again")
			return
		}
		if sess.accountID != id {
			writeBadRequest(c, "session does not belong to this account")
			return
		}
		// CSRF: the returned state must match (constant-time). Some
		// providers omit state in the redirect — only enforce when the
		// client sent one back.
		if in.State != "" && subtle.ConstantTimeCompare([]byte(in.State), []byte(sess.state)) != 1 {
			writeBadRequest(c, "oauth state mismatch")
			return
		}
		plugin, ok := reg.Get(sess.plugin)
		if !ok {
			writeBadRequest(c, "unknown auth plugin: "+sess.plugin)
			return
		}

		bundle, err := plugin.ExchangeCallback(c.Request.Context(), &upstreamauth.CallbackRequest{
			Code: in.Code, State: sess.state, CodeVerifier: sess.codeVerifier,
			RedirectURI: sess.redirectURI, ProxyURL: sess.proxyURL,
		})
		if err != nil {
			writeBadRequest(c, err.Error())
			return
		}

		now := time.Now().UTC()
		upd := repository.AuthRuntimeUpdate{
			Credentials: bundle.Credentials, Plugin: sess.plugin,
			Status: upstreamauth.StatusOK, RefreshError: "",
			ExpiresAt: bundle.ExpiresAt, LastRefreshAt: &now,
		}
		if bundle.Profile != nil {
			upd.Subject, upd.Email, upd.Plan = bundle.Profile.Subject, bundle.Profile.Email, bundle.Profile.Plan
		}
		// Best-effort identity enrichment via the profile endpoint.
		if pr, verr := plugin.Validate(c.Request.Context(), &upstreamauth.ValidateRequest{
			Credentials: bundle.Credentials, ProxyURL: sess.proxyURL,
		}); verr == nil && pr != nil {
			if pr.Subject != "" {
				upd.Subject = pr.Subject
			}
			if pr.Email != "" {
				upd.Email = pr.Email
			}
			if pr.Plan != "" {
				upd.Plan = pr.Plan
			}
			bundle.Profile = pr
		}
		if err := store.UpdateAuthRuntime(c.Request.Context(), id, upd); err != nil {
			writeInternal(c, "persist credentials: "+err.Error())
			return
		}

		resp := gin.H{"auth_status": upstreamauth.StatusOK, "plugin": sess.plugin}
		if bundle.ExpiresAt != nil {
			resp["auth_expires_at"] = bundle.ExpiresAt.UTC().Format(time.RFC3339)
		}
		if bundle.Profile != nil {
			resp["profile"] = bundle.Profile
		}
		c.JSON(http.StatusOK, resp)
	}
}

// newSessionID returns a random opaque session identifier.
func newSessionID() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
