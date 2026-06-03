package provider

import (
	"testing"
	"time"
)

func at(t *testing.T, tz, s string) time.Time {
	t.Helper()
	loc := time.UTC
	if tz != "" {
		l, err := time.LoadLocation(tz)
		if err != nil {
			t.Fatalf("load tz %q: %v", tz, err)
		}
		loc = l
	}
	tm, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return tm
}

func TestIsActiveAt(t *testing.T) {
	// 2026-06-03 is a Wednesday (weekday 3).
	bizHours := &Account{
		ActiveWindows: []ActiveWindow{{Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "18:00"}},
	}
	cases := []struct {
		name string
		when string // "YYYY-MM-DD HH:MM" UTC
		want bool
	}{
		{"inside weekday window", "2026-06-03 10:00", true},
		{"before open", "2026-06-03 08:59", false},
		{"after close", "2026-06-03 18:00", false},
		{"weekend excluded", "2026-06-06 10:00", false}, // Saturday
	}
	for _, tc := range cases {
		if got := bizHours.IsActiveAt(at(t, "", tc.when)); got != tc.want {
			t.Errorf("%s: IsActiveAt(%s) = %v, want %v", tc.name, tc.when, got, tc.want)
		}
	}

	// Empty windows → always active.
	none := &Account{}
	if !none.IsActiveAt(at(t, "", "2026-06-06 03:00")) {
		t.Error("no windows must mean always active")
	}

	// Midnight-wrapping window 22:00–02:00 (every day).
	night := &Account{ActiveWindows: []ActiveWindow{{Start: "22:00", End: "02:00"}}}
	if !night.IsActiveAt(at(t, "", "2026-06-03 23:30")) {
		t.Error("23:30 should be inside a 22:00-02:00 window")
	}
	if !night.IsActiveAt(at(t, "", "2026-06-03 01:00")) {
		t.Error("01:00 should be inside a 22:00-02:00 window")
	}
	if night.IsActiveAt(at(t, "", "2026-06-03 12:00")) {
		t.Error("noon should be outside a 22:00-02:00 window")
	}
}

func TestIsActiveAtTimezone(t *testing.T) {
	// Window 09:00-17:00 New York time. 14:00 UTC = 10:00 EDT (active);
	// 02:00 UTC = 22:00 prev-day EDT (inactive).
	a := &Account{
		ActiveWindows:  []ActiveWindow{{Start: "09:00", End: "17:00"}},
		ActiveTimezone: "America/New_York",
	}
	if !a.IsActiveAt(at(t, "", "2026-06-03 14:00")) {
		t.Error("14:00 UTC = 10:00 EDT should be active")
	}
	if a.IsActiveAt(at(t, "", "2026-06-03 02:00")) {
		t.Error("02:00 UTC = 22:00 EDT should be inactive")
	}
}

func TestActiveWindowValid(t *testing.T) {
	good := ActiveWindow{Days: []int{0, 6}, Start: "00:00", End: "23:59"}
	if !good.Valid() {
		t.Error("expected valid window")
	}
	for _, w := range []ActiveWindow{
		{Start: "9:00", End: "25:00"},                // bad hour
		{Start: "09:00", End: "18:62"},               // bad minute
		{Start: "0900", End: "1800"},                 // no colon
		{Days: []int{7}, Start: "1:00", End: "2:00"}, // bad day
	} {
		if w.Valid() {
			t.Errorf("expected invalid: %+v", w)
		}
	}
}
