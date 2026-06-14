package provider

import (
	"fmt"
	"net/url"
	"time"
)

// Proxy is an upstream egress proxy (migration 0038). Accounts bind to
// one by id; the ProxyResolver turns the binding into a proxy URL for
// the forwarder, applying expiry fallback. Password is the decrypted
// value (encrypted at rest).
type Proxy struct {
	ID            int64
	Name          string
	Protocol      string // http | https | socks5 | socks5h
	Host          string
	Port          int
	Username      string
	Password      string
	Status        string // active | disabled
	ExpiresAt     *time.Time
	FallbackMode  string // none | direct | proxy
	BackupProxyID *int64
}

// URL renders the proxy as a dial URL (scheme://[user[:pass]@]host:port),
// the form net/http and the SOCKS dialer accept. Returns "" for a nil
// proxy.
func (p *Proxy) URL() string {
	if p == nil {
		return ""
	}
	u := &url.URL{
		Scheme: p.Protocol,
		Host:   fmt.Sprintf("%s:%d", p.Host, p.Port),
	}
	if p.Username != "" {
		if p.Password != "" {
			u.User = url.UserPassword(p.Username, p.Password)
		} else {
			u.User = url.User(p.Username)
		}
	}
	return u.String()
}

// IsExpired reports whether the proxy is past its expiry. A nil expiry
// never expires.
func (p *Proxy) IsExpired(now time.Time) bool {
	return p != nil && p.ExpiresAt != nil && !p.ExpiresAt.After(now)
}

// Active reports whether the proxy is enabled (status, not expiry).
func (p *Proxy) Active() bool {
	return p != nil && p.Status == "active"
}
