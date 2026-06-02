package provider

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TestStatus is the traffic-light verdict of a connectivity test.
type TestStatus string

const (
	TestGreen  TestStatus = "green"  // reachable + authenticated
	TestYellow TestStatus = "yellow" // reachable but slow / rate-limited / unverifiable
	TestRed    TestStatus = "red"    // unreachable or rejected
)

// TestResult is the classified outcome of probing an upstream account.
type TestResult struct {
	Status     TestStatus `json:"status"`
	HTTPStatus int        `json:"http_status,omitempty"`
	LatencyMs  int64      `json:"latency_ms"`
	Message    string     `json:"message"`
}

// Tester is the optional interface a driver implements to support a cheap
// connectivity/auth check (no real generation, no token cost where
// possible). Drivers that can't test cheaply simply don't implement it,
// and the admin API reports the test as unsupported.
type Tester interface {
	Test(ctx context.Context, account *Account) TestResult
}

// testClient is the shared probe client. The handler also sets a ctx
// deadline; this is the hard ceiling.
var testClient = &http.Client{Timeout: 12 * time.Second}

// slowThresholdMs above which a successful probe is downgraded to yellow.
const slowThresholdMs = 2500

// ProbeGET sends a GET to url with the given headers and classifies the
// outcome. It's the shared engine behind each driver's Test — a driver
// only has to supply the right models-list URL and auth headers.
func ProbeGET(ctx context.Context, url string, headers map[string]string) TestResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return TestResult{Status: TestRed, Message: "invalid request: " + err.Error()}
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	start := time.Now()
	resp, err := testClient.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return TestResult{Status: TestRed, LatencyMs: latency,
			Message: "could not reach upstream: " + cleanErr(err)}
	}
	defer resp.Body.Close()
	return classify(resp.StatusCode, latency)
}

func classify(status int, latencyMs int64) TestResult {
	r := TestResult{HTTPStatus: status, LatencyMs: latencyMs}
	switch {
	case status >= 200 && status < 300:
		if latencyMs > slowThresholdMs {
			r.Status, r.Message = TestYellow, "reachable but slow"
			return r
		}
		r.Status, r.Message = TestGreen, "reachable and authenticated"
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		r.Status, r.Message = TestRed, "authentication failed — check the API key"
	case status == http.StatusNotFound:
		r.Status, r.Message = TestRed, "endpoint not found — check the base URL"
	case status == http.StatusTooManyRequests:
		r.Status, r.Message = TestYellow, "reachable but rate-limited (429)"
	case status >= 500:
		r.Status, r.Message = TestRed, fmt.Sprintf("upstream error (HTTP %d)", status)
	default:
		r.Status, r.Message = TestRed, fmt.Sprintf("unexpected response (HTTP %d)", status)
	}
	return r
}

func cleanErr(err error) string {
	s := err.Error()
	// Trim the noisy "Get \"url\": " prefix net/http adds.
	if i := strings.LastIndex(s, ": "); i >= 0 && strings.HasPrefix(s, "Get ") {
		s = s[i+2:]
	}
	return s
}

// ValidateUpstreamURL rejects a base URL that points at the local host or
// a private / link-local network — a basic SSRF guard for the
// admin-supplied upstream address. An empty URL is allowed (the driver's
// default public endpoint is used).
func ValidateUpstreamURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("not a valid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL must start with http:// or https://")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL has no host")
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".local") || strings.HasSuffix(lower, ".internal") {
		return fmt.Errorf("upstream host may not be a local address")
	}
	// Block IP literals in private / loopback / link-local ranges (incl.
	// the 169.254.169.254 cloud metadata endpoint).
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return fmt.Errorf("upstream host may not be a private or loopback address")
		}
	}
	return nil
}

// modelsURL derives a "GET models" probe URL from a base, mirroring the
// OpenAI-style normalisation: "", "https://h", and "https://h/v1" all
// resolve under /v1/models. A base already ending in /models is used
// verbatim. Shared by the OpenAI-compatible and Anthropic testers.
func ModelsURL(base, fallback string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = fallback
	}
	switch {
	case strings.HasSuffix(base, "/models"):
		return base
	case strings.HasSuffix(base, "/v1"):
		return base + "/models"
	default:
		return base + "/v1/models"
	}
}
