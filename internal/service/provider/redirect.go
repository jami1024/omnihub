package provider

import (
	"regexp"
	"strings"
	"sync"
)

// ModelRedirectMatch is how a redirect rule's source is compared
// against the requested model name.
type ModelRedirectMatch string

const (
	MatchExact    ModelRedirectMatch = "exact"
	MatchPrefix   ModelRedirectMatch = "prefix"
	MatchSuffix   ModelRedirectMatch = "suffix"
	MatchContains ModelRedirectMatch = "contains"
	MatchRegex    ModelRedirectMatch = "regex"
)

// ModelRedirect rewrites a requested model name to a different upstream
// model before the driver builds its request. Rules are evaluated in
// order and the first match wins; this is a matched-pair pass-through
// rename, NOT a cross-protocol transform.
//
// For MatchRegex, Target may contain $1/${name} capture references
// expanded against Source's groups (Go regexp.Expand semantics).
type ModelRedirect struct {
	MatchType ModelRedirectMatch `json:"match_type"`
	Source    string             `json:"source"`
	Target    string             `json:"target"`
}

// Valid reports whether the rule is well-formed: a known match type,
// non-empty source/target, and (for regex) a compilable pattern.
func (r ModelRedirect) Valid() bool {
	if strings.TrimSpace(r.Source) == "" || strings.TrimSpace(r.Target) == "" {
		return false
	}
	switch r.MatchType {
	case MatchExact, MatchPrefix, MatchSuffix, MatchContains:
		return true
	case MatchRegex:
		_, err := compileRedirectRegex(r.Source)
		return err == nil
	default:
		return false
	}
}

// regexCache memoises compiled redirect patterns. Rules are reloaded
// from the DB on every account change, so this keeps hot-path matching
// from recompiling identical patterns each request.
var (
	regexCacheMu sync.RWMutex
	regexCache   = map[string]*regexp.Regexp{}
)

func compileRedirectRegex(pattern string) (*regexp.Regexp, error) {
	regexCacheMu.RLock()
	re, ok := regexCache[pattern]
	regexCacheMu.RUnlock()
	if ok {
		return re, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	regexCacheMu.Lock()
	regexCache[pattern] = re
	regexCacheMu.Unlock()
	return re, nil
}

// ApplyModelRedirects returns the rewritten model and true when a rule
// matches, or (model, false) when none do. Invalid rules are skipped.
func ApplyModelRedirects(rules []ModelRedirect, model string) (string, bool) {
	for _, r := range rules {
		switch r.MatchType {
		case MatchExact:
			if model == r.Source {
				return r.Target, true
			}
		case MatchPrefix:
			if strings.HasPrefix(model, r.Source) {
				return r.Target, true
			}
		case MatchSuffix:
			if strings.HasSuffix(model, r.Source) {
				return r.Target, true
			}
		case MatchContains:
			if strings.Contains(model, r.Source) {
				return r.Target, true
			}
		case MatchRegex:
			re, err := compileRedirectRegex(r.Source)
			if err != nil {
				continue
			}
			loc := re.FindStringSubmatchIndex(model)
			if loc == nil {
				continue
			}
			// Expand capture references in the target ($1, ${name}).
			return string(re.ExpandString(nil, r.Target, model, loc)), true
		}
	}
	return model, false
}
