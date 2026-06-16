// Package accountquota holds the latest rate-limit usage windows for
// subscription (OAuth) accounts, captured passively from upstream
// response headers as real traffic flows. The codex backend reports its
// 5-hour / weekly windows only on the /responses response headers (no
// usage endpoint), so the gateway sniffs them on every request and the
// admin UI reads the last-known snapshot — no extra upstream calls.
//
// Snapshots are in-memory and ephemeral: a restart clears them and the
// next request through each account repopulates. That matches how 429
// cooldowns are tracked and avoids a migration for inherently transient
// data.
package accountquota

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jami1024/omnihub/internal/service/provider"
)

// Window labels are shared with the claude-subscription quota probe so
// the frontend maps a single label set to friendly text.
const (
	labelFiveHour = "five_hour"
	labelSevenDay = "seven_day"
	// fiveHourMaxMinutes is the upper bound (with slack) for classifying
	// a window as the 5-hour bucket by its window_minutes; anything larger
	// is the weekly/7-day bucket. codex reports 300 (5h) and 10080 (7d).
	fiveHourMaxMinutes = 360
)

// Snapshot is the latest known usage for one account.
type Snapshot struct {
	Windows   []provider.QuotaWindow
	UpdatedAt time.Time
}

// Store is a concurrency-safe map of account id → latest snapshot.
type Store struct {
	mu   sync.RWMutex
	snap map[int64]Snapshot
	now  func() time.Time
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{snap: make(map[int64]Snapshot), now: time.Now}
}

// Get returns the latest windows for an account (nil if none captured).
func (s *Store) Get(accountID int64) []provider.QuotaWindow {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snap[accountID].Windows
}

// RecordCodex parses the x-codex-primary/secondary-* usage headers from
// an upstream response and stores the normalised 5h/7d windows. A header
// set carrying no usable usage fields is ignored (e.g. non-codex
// responses, or an error response without the headers).
func (s *Store) RecordCodex(accountID int64, h http.Header) {
	wins := parseCodexWindows(h, s.now())
	if len(wins) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snap[accountID] = Snapshot{Windows: wins, UpdatedAt: s.now()}
}

// parseCodexWindows extracts the primary/secondary windows and labels
// each as 5-hour or 7-day by its window_minutes (falling back to the
// historical primary=7d / secondary=5h convention when window_minutes is
// absent).
func parseCodexWindows(h http.Header, now time.Time) []provider.QuotaWindow {
	specs := []struct{ prefix, fallback string }{
		{"primary", labelSevenDay},
		{"secondary", labelFiveHour},
	}
	var out []provider.QuotaWindow
	for _, sp := range specs {
		used, resetSec, windowMin, ok := readCodexWindow(h, sp.prefix)
		if !ok {
			continue
		}
		win := provider.QuotaWindow{Label: classifyWindow(windowMin, sp.fallback), UsedPercent: used}
		if resetSec > 0 {
			win.ResetsAt = now.Add(time.Duration(resetSec) * time.Second).UTC().Format(time.RFC3339)
		}
		out = append(out, win)
	}
	return out
}

// readCodexWindow reads one window's three headers. ok is false when the
// used-percent header is absent (the window was not reported).
func readCodexWindow(h http.Header, prefix string) (used float64, resetSec int64, windowMin int, ok bool) {
	up := strings.TrimSpace(h.Get("x-codex-" + prefix + "-used-percent"))
	if up == "" {
		return 0, 0, 0, false
	}
	used, _ = strconv.ParseFloat(up, 64)
	resetSec, _ = strconv.ParseInt(strings.TrimSpace(h.Get("x-codex-"+prefix+"-reset-after-seconds")), 10, 64)
	windowMin, _ = strconv.Atoi(strings.TrimSpace(h.Get("x-codex-" + prefix + "-window-minutes")))
	return used, resetSec, windowMin, true
}

// classifyWindow maps a window_minutes value to a 5h/7d label; when the
// value is missing (0) it uses the supplied fallback.
func classifyWindow(windowMin int, fallback string) string {
	if windowMin > 0 {
		if windowMin <= fiveHourMaxMinutes {
			return labelFiveHour
		}
		return labelSevenDay
	}
	return fallback
}
