package admin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Claims is the JSON payload of the admin JWT. Field names follow the
// RFC 7519 short names so any standard JWT debugger renders the token.
type Claims struct {
	Sub  string `json:"sub"`           // username
	UID  int64  `json:"uid"`           // admin_users.id (or users.id for kind=user)
	Kind string `json:"knd,omitempty"` // "" / "admin" = admin console; "user" = end-user portal
	Iat  int64  `json:"iat"`           // issued-at (unix seconds)
	Exp  int64  `json:"exp"`           // expiry  (unix seconds)
}

// KindAdmin and KindUser distinguish the two token audiences signed with
// the same secret, so an end-user token can never authenticate against
// the admin console and vice versa.
const (
	KindAdmin = "admin"
	KindUser  = "user"
)

// Issuer mints and verifies HS256 JWTs for the admin UI. Implemented
// directly against stdlib (hmac + base64) so the gateway does not pull
// in a third-party JWT package — the surface is small and the format
// is fully described by RFC 7519 §3.1.
type Issuer struct {
	secret []byte
	ttl    time.Duration
}

// NewIssuer wires a secret + token TTL. The caller is expected to feed
// in OMNIHUB_ADMIN_JWT_SECRET; an empty secret panics rather than
// silently signing with no key.
func NewIssuer(secret []byte, ttl time.Duration) *Issuer {
	if len(secret) == 0 {
		panic("admin: jwt secret is empty")
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Issuer{secret: secret, ttl: ttl}
}

// TTL exposes the configured token lifetime so callers can compute the
// expires_at field in the login response.
func (i *Issuer) TTL() time.Duration { return i.ttl }

// Issue produces a fresh signed token for the given user. The returned
// Time is the token's expiry (so the handler can echo it back to the
// client without recomputing).
func (i *Issuer) Issue(username string, uid int64) (string, time.Time, error) {
	return i.IssueKind(username, uid, KindAdmin)
}

// IssueKind mints a token for a specific audience (admin console vs the
// end-user portal). The kind is embedded as a claim and enforced by the
// matching authenticator.
func (i *Issuer) IssueKind(username string, uid int64, kind string) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(i.ttl)
	token, err := i.encode(Claims{Sub: username, UID: uid, Kind: kind, Iat: now.Unix(), Exp: exp.Unix()})
	if err != nil {
		return "", time.Time{}, err
	}
	return token, exp, nil
}

func (i *Issuer) encode(c Claims) (string, error) {
	header := `{"alg":"HS256","typ":"JWT"}` // fixed for our single algorithm
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	h64 := base64.RawURLEncoding.EncodeToString([]byte(header))
	p64 := base64.RawURLEncoding.EncodeToString(payload)
	signing := h64 + "." + p64
	mac := hmac.New(sha256.New, i.secret)
	mac.Write([]byte(signing))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signing + "." + sig, nil
}

// ErrInvalidToken signals a malformed token, wrong signature, or wrong
// algorithm. The handler maps it to 401.
var ErrInvalidToken = errors.New("invalid token")

// ErrTokenExpired is returned when the signature is valid but `exp` has
// passed. Distinct from ErrInvalidToken so the frontend can tell "log
// in again because your session aged out" from "this isn't a real
// token".
var ErrTokenExpired = errors.New("token expired")

// Verify checks the signature and expiry. It does NOT touch the
// database — issuance baked the user identity into the payload, and a
// disabled account is enforced by re-checking enabled at the handler
// layer if/when long-lived tokens become a concern.
func (i *Issuer) Verify(token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}
	signing := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, i.secret)
	mac.Write([]byte(signing))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return nil, ErrInvalidToken
	}
	// Also enforce the algorithm: a token whose header says "alg":"none"
	// would have parts[2] == "" and trip the HMAC check above, but we
	// re-check the header explicitly to avoid any future surprise from a
	// downgrade attempt.
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrInvalidToken
	}
	var h struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(header, &h); err != nil || h.Alg != "HS256" {
		return nil, ErrInvalidToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidToken
	}
	var c Claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, ErrInvalidToken
	}
	if c.Exp > 0 && time.Now().Unix() > c.Exp {
		return nil, ErrTokenExpired
	}
	return &c, nil
}
