package claudesub

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
	"github.com/jami1024/omnihub/internal/service/provider/drivers/anthropic"
)

// claudeUsageWindow is one bucket of the /api/oauth/usage reply.
// utilization is 0..1.
type claudeUsageWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"`
}

// Quota queries /api/oauth/usage and normalises the rolling windows
// (5-hour, 7-day, 7-day model-specific) for the admin UI.
func (d *Driver) Quota(ctx context.Context, account *provider.Account) (*provider.QuotaInfo, error) {
	if account == nil {
		return nil, errors.New("claudesub: nil account")
	}
	token := account.Credential("access_token")
	if token == "" {
		return nil, errors.New("claudesub: account has no access_token")
	}
	base := anthropic.DefaultBaseURL
	if account.BaseURL != "" {
		base = account.BaseURL
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+usageProbePath, nil)
	if err != nil {
		return nil, fmt.Errorf("claudesub: build quota request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("anthropic-beta", oauthBeta)
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", userAgent)

	resp, err := quotaClient(account.ProxyURL).Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("claudesub: quota request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("claudesub: quota rejected (HTTP %d)", resp.StatusCode)
	}

	var raw map[string]claudeUsageWindow
	if err := json.Unmarshal(body, &raw); err != nil {
		// Unexpected shape: still useful raw.
		return &provider.QuotaInfo{Raw: json.RawMessage(body)}, nil
	}
	info := &provider.QuotaInfo{Raw: json.RawMessage(body)}
	for _, label := range []string{"five_hour", "seven_day", "seven_day_sonnet", "seven_day_opus"} {
		if w, ok := raw[label]; ok {
			info.Windows = append(info.Windows, provider.QuotaWindow{
				Label:       label,
				UsedPercent: w.Utilization * 100,
				ResetsAt:    w.ResetsAt,
			})
		}
	}
	return info, nil
}

// quotaClient builds the HTTP client for quota probes, honouring the
// account proxy when set.
func quotaClient(proxyURL string) *http.Client {
	c := &http.Client{Timeout: 15 * time.Second}
	if proxyURL != "" {
		if u, err := url.Parse(proxyURL); err == nil {
			c.Transport = &http.Transport{Proxy: http.ProxyURL(u)}
		}
	}
	return c
}
