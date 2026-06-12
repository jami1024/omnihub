package provider

import (
	"context"
	"encoding/json"
)

// QuotaWindow is one rolling usage window of a subscription account
// (e.g. Claude's 5-hour and 7-day buckets).
type QuotaWindow struct {
	Label       string  `json:"label"`
	UsedPercent float64 `json:"used_percent"`
	ResetsAt    string  `json:"resets_at,omitempty"`
}

// QuotaInfo is a driver's quota report. Windows is the normalised
// view for the admin UI; Raw carries the upstream payload verbatim for
// providers whose schema is not stable enough to normalise (codex).
type QuotaInfo struct {
	Windows []QuotaWindow   `json:"windows"`
	Raw     json.RawMessage `json:"raw,omitempty"`
}

// QuotaProber is the optional driver extension for querying a
// subscription account's remaining quota. Mirrors the Tester pattern:
// handlers type-assert and degrade gracefully when a driver does not
// implement it.
type QuotaProber interface {
	Quota(ctx context.Context, account *Account) (*QuotaInfo, error)
}
