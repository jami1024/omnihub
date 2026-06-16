package accountquota

import (
	"net/http"
	"testing"
	"time"
)

func TestRecordCodex_NormalisesWindows(t *testing.T) {
	s := NewStore()
	h := http.Header{}
	// primary = weekly (10080 min), secondary = 5h (300 min).
	h.Set("x-codex-primary-used-percent", "61")
	h.Set("x-codex-primary-window-minutes", "10080")
	h.Set("x-codex-primary-reset-after-seconds", "384607")
	h.Set("x-codex-secondary-used-percent", "12.5")
	h.Set("x-codex-secondary-window-minutes", "300")
	h.Set("x-codex-secondary-reset-after-seconds", "3600")

	s.RecordCodex(7, h)
	wins := s.Get(7)
	if len(wins) != 2 {
		t.Fatalf("want 2 windows, got %d: %+v", len(wins), wins)
	}
	byLabel := map[string]float64{}
	resets := map[string]string{}
	for _, w := range wins {
		byLabel[w.Label] = w.UsedPercent
		resets[w.Label] = w.ResetsAt
	}
	if byLabel["seven_day"] != 61 {
		t.Errorf("seven_day used should be 61, got %v", byLabel["seven_day"])
	}
	if byLabel["five_hour"] != 12.5 {
		t.Errorf("five_hour used should be 12.5, got %v", byLabel["five_hour"])
	}
	if resets["five_hour"] == "" {
		t.Error("five_hour should carry a resets_at")
	}
}

func TestRecordCodex_FallbackWhenNoWindowMinutes(t *testing.T) {
	s := NewStore()
	h := http.Header{}
	// No window-minutes: primary→7d, secondary→5h (legacy convention).
	h.Set("x-codex-primary-used-percent", "5")
	h.Set("x-codex-secondary-used-percent", "9")
	s.RecordCodex(1, h)
	wins := s.Get(1)
	got := map[string]bool{}
	for _, w := range wins {
		got[w.Label] = true
	}
	if !got["seven_day"] || !got["five_hour"] {
		t.Fatalf("expected both labels via fallback, got %+v", wins)
	}
}

func TestRecordCodex_IgnoresNonCodexHeaders(t *testing.T) {
	s := NewStore()
	h := http.Header{}
	h.Set("content-type", "application/json")
	s.RecordCodex(2, h)
	if w := s.Get(2); w != nil {
		t.Fatalf("expected no windows for non-codex headers, got %+v", w)
	}
}

func TestGet_UnknownAccount(t *testing.T) {
	s := NewStore()
	if w := s.Get(999); w != nil {
		t.Fatalf("expected nil for unknown account, got %+v", w)
	}
}

func TestClassifyWindow(t *testing.T) {
	if classifyWindow(300, labelSevenDay) != labelFiveHour {
		t.Error("300 min should classify as five_hour")
	}
	if classifyWindow(10080, labelFiveHour) != labelSevenDay {
		t.Error("10080 min should classify as seven_day")
	}
	if classifyWindow(0, labelSevenDay) != labelSevenDay {
		t.Error("missing window minutes should use fallback")
	}
}

// keep time import used even if assertions above change.
var _ = time.Now
