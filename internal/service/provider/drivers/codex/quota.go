package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jami1024/omnihub/internal/service/provider"
)

// Quota queries the codex backend's usage endpoint. The wham/usage
// payload schema is not publicly stable, so it is returned verbatim in
// Raw (the admin UI renders it as-is) with a best-effort extraction of
// percent-style windows when the known field names are present.
func (d *Driver) Quota(ctx context.Context, account *provider.Account) (*provider.QuotaInfo, error) {
	if account == nil {
		return nil, errors.New("codex: nil account")
	}
	token := account.Credential("access_token")
	if token == "" {
		return nil, errors.New("codex: account has no access_token")
	}
	base := DefaultBaseURL
	if account.BaseURL != "" {
		base = account.BaseURL
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+usageProbePath, nil)
	if err != nil {
		return nil, fmt.Errorf("codex: build quota request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	if accountID := account.Credential("account_id"); accountID != "" {
		httpReq.Header.Set("chatgpt-account-id", accountID)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("originator", originatorValue)

	client := &http.Client{Timeout: 15 * time.Second}
	if account.ProxyURL != "" {
		if u, perr := url.Parse(account.ProxyURL); perr == nil {
			client.Transport = &http.Transport{Proxy: http.ProxyURL(u)}
		}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("codex: quota request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("codex: quota rejected (HTTP %d)", resp.StatusCode)
	}

	info := &provider.QuotaInfo{Raw: json.RawMessage(body)}
	info.Windows = extractCodexWindows(body)
	return info, nil
}

// extractCodexWindows best-effort-parses rate-limit windows out of the
// wham/usage payload. The known convention (mirrored from the
// x-codex-* response headers) is primary/secondary objects carrying
// used_percent + reset/window fields; anything unrecognised simply
// yields no windows and the UI falls back to the raw payload.
func extractCodexWindows(body []byte) []provider.QuotaWindow {
	var payload struct {
		RateLimits map[string]struct {
			UsedPercent       float64 `json:"used_percent"`
			ResetsInSeconds   int64   `json:"resets_in_seconds"`
			ResetAfterSeconds int64   `json:"reset_after_seconds"`
		} `json:"rate_limits"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || len(payload.RateLimits) == 0 {
		return nil
	}
	var out []provider.QuotaWindow
	for _, label := range []string{"primary", "secondary"} {
		w, ok := payload.RateLimits[label]
		if !ok {
			continue
		}
		win := provider.QuotaWindow{Label: label, UsedPercent: w.UsedPercent}
		if secs := w.ResetsInSeconds + w.ResetAfterSeconds; secs > 0 {
			win.ResetsAt = time.Now().Add(time.Duration(secs) * time.Second).UTC().Format(time.RFC3339)
		}
		out = append(out, win)
	}
	return out
}
