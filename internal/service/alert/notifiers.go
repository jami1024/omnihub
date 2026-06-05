package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// httpClient is shared by the webhook-style notifiers. The per-send
// timeout is enforced by the caller's context (see Alerter.deliver); this
// client-level timeout is a backstop.
var httpClient = &http.Client{Timeout: 15 * time.Second}

// postJSON sends body as application/json and treats any 2xx as success.
func postJSON(ctx context.Context, url string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("non-2xx status %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}
	return nil
}

// NotifierFor builds the notifier for a channel kind + url. The second
// result is false for an unknown kind. Shared by the DB-backed channel
// pool and the admin test-send path so both honour the same type set.
func NotifierFor(kind, url string) (Notifier, bool) {
	switch kind {
	case "webhook":
		return WebhookNotifier{URL: url}, true
	case "feishu":
		return FeishuNotifier{URL: url}, true
	case "dingtalk":
		return DingTalkNotifier{URL: url}, true
	default:
		return nil, false
	}
}

// WebhookNotifier posts a structured JSON document to a generic endpoint.
type WebhookNotifier struct{ URL string }

func (w WebhookNotifier) Name() string { return "webhook" }

func (w WebhookNotifier) Send(ctx context.Context, e Event) error {
	return postJSON(ctx, w.URL, map[string]any{
		"level":        string(e.Level),
		"title":        e.Title,
		"text":         e.Text,
		"account_id":   e.AccountID,
		"account_name": e.AccountName,
		"time":         e.At.UTC().Format(time.RFC3339),
		"source":       "omnihub",
	})
}

// FeishuNotifier posts a plain-text message to a Feishu (Lark) custom bot
// webhook.
type FeishuNotifier struct{ URL string }

func (f FeishuNotifier) Name() string { return "feishu" }

func (f FeishuNotifier) Send(ctx context.Context, e Event) error {
	return postJSON(ctx, f.URL, map[string]any{
		"msg_type": "text",
		"content":  map[string]string{"text": formatText(e)},
	})
}

// DingTalkNotifier posts a plain-text message to a DingTalk custom robot
// webhook.
type DingTalkNotifier struct{ URL string }

func (d DingTalkNotifier) Name() string { return "dingtalk" }

func (d DingTalkNotifier) Send(ctx context.Context, e Event) error {
	return postJSON(ctx, d.URL, map[string]any{
		"msgtype": "text",
		"text":    map[string]string{"content": formatText(e)},
	})
}

// formatText renders an Event as a single human-readable line for the
// chat-style notifiers.
func formatText(e Event) string {
	prefix := "⚠️"
	if e.Level == LevelInfo {
		prefix = "✅"
	}
	return fmt.Sprintf("%s [OmniHub] %s\n%s", prefix, e.Title, e.Text)
}

// Config selects which notifiers to build (typically from env).
type Config struct {
	WebhookURL  string
	FeishuURL   string
	DingTalkURL string
	Throttle    time.Duration
}

// Notifiers builds the notifier list implied by the configured URLs.
// Empty URLs are skipped; the result is empty when nothing is configured.
func (c Config) Notifiers() []Notifier {
	var ns []Notifier
	if c.WebhookURL != "" {
		ns = append(ns, WebhookNotifier{URL: c.WebhookURL})
	}
	if c.FeishuURL != "" {
		ns = append(ns, FeishuNotifier{URL: c.FeishuURL})
	}
	if c.DingTalkURL != "" {
		ns = append(ns, DingTalkNotifier{URL: c.DingTalkURL})
	}
	return ns
}
