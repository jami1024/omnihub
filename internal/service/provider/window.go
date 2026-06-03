package provider

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

// ActiveWindow is a recurring time-of-day window during which an account
// is routable. Days restricts the window to certain weekdays (0=Sunday
// .. 6=Saturday); empty Days means "every day". Start/End are "HH:MM"
// 24-hour local times evaluated in the account's ActiveTimezone. A window
// whose Start is after its End wraps past midnight (e.g. 22:00–02:00).
type ActiveWindow struct {
	Days  []int  `json:"days,omitempty"`
	Start string `json:"start"`
	End   string `json:"end"`
}

// locationCache memoises parsed IANA timezones so the resolver hot-path
// never re-parses tzdata.
var locationCache sync.Map // map[string]*time.Location

// locationFor returns the *time.Location for an IANA name, falling back
// to UTC for an empty or unloadable name.
func locationFor(name string) *time.Location {
	if name == "" {
		return time.UTC
	}
	if v, ok := locationCache.Load(name); ok {
		return v.(*time.Location)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		loc = time.UTC
	}
	locationCache.Store(name, loc)
	return loc
}

// parseHHMM converts "HH:MM" into minutes-since-midnight, or -1 when
// malformed.
func parseHHMM(s string) int {
	s = strings.TrimSpace(s)
	h, m, ok := strings.Cut(s, ":")
	if !ok {
		return -1
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return -1
	}
	return hh*60 + mm
}

// matches reports whether local time lt falls inside this window.
func (w ActiveWindow) matches(lt time.Time) bool {
	if len(w.Days) > 0 {
		day := int(lt.Weekday())
		found := false
		for _, d := range w.Days {
			if d == day {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	start := parseHHMM(w.Start)
	end := parseHHMM(w.End)
	if start < 0 || end < 0 {
		return false // malformed window never matches
	}
	cur := lt.Hour()*60 + lt.Minute()
	if start == end {
		return false // zero-width window
	}
	if start < end {
		return cur >= start && cur < end
	}
	// Wraps past midnight: active from Start to 24:00 and 00:00 to End.
	return cur >= start || cur < end
}

// Valid reports whether the window is well-formed (parseable times,
// weekdays in range).
func (w ActiveWindow) Valid() bool {
	if parseHHMM(w.Start) < 0 || parseHHMM(w.End) < 0 {
		return false
	}
	for _, d := range w.Days {
		if d < 0 || d > 6 {
			return false
		}
	}
	return true
}

// IsActiveAt reports whether the account is routable at instant t. An
// account with no windows is always active; otherwise it is active when
// t (in the account's timezone) falls inside at least one window.
func (a *Account) IsActiveAt(t time.Time) bool {
	if a == nil || len(a.ActiveWindows) == 0 {
		return true
	}
	lt := t.In(locationFor(a.ActiveTimezone))
	for _, w := range a.ActiveWindows {
		if w.matches(lt) {
			return true
		}
	}
	return false
}
