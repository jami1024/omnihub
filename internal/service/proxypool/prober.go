package proxypool

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/jami1024/omnihub/internal/service/provider"
)

const (
	// defaultProbeTarget is a lightweight, globally reachable endpoint
	// (returns 204). ANY HTTP response means the proxy carried traffic;
	// only a transport error / timeout counts as a failed probe.
	defaultProbeTarget      = "https://www.gstatic.com/generate_204"
	defaultProbeTimeout     = 10 * time.Second
	defaultProbeConcurrency = 4
)

// Prober actively checks each active proxy's reachability in the
// background and feeds the verdict into the Pool's health state. The
// resolver then degrades an unhealthy proxy to its backup / direct, so
// a proxy that dies between requests is routed around without waiting
// for the bound accounts' circuit breakers to trip.
type Prober struct {
	pool        *Pool
	interval    time.Duration
	timeout     time.Duration
	target      string
	concurrency int
}

// NewProber wires a prober to the pool. A non-positive interval falls
// back to one minute.
func NewProber(pool *Pool, interval time.Duration) *Prober {
	if interval <= 0 {
		interval = time.Minute
	}
	return &Prober{
		pool:        pool,
		interval:    interval,
		timeout:     defaultProbeTimeout,
		target:      defaultProbeTarget,
		concurrency: defaultProbeConcurrency,
	}
}

// Start runs one immediate pass, then re-probes every interval until
// ctx is cancelled.
func (pr *Prober) Start(ctx context.Context) {
	if pr == nil || pr.pool == nil {
		return
	}
	go func() {
		pr.probeAll(ctx)
		ticker := time.NewTicker(pr.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pr.probeAll(ctx)
			}
		}
	}()
}

func (pr *Prober) probeAll(ctx context.Context) {
	proxies := pr.pool.AllActive()
	sem := make(chan struct{}, pr.concurrency)
	var wg sync.WaitGroup
	for _, p := range proxies {
		wg.Add(1)
		go func(p *provider.Proxy) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ok, latency := pr.probe(ctx, p)
			pr.pool.RecordProbe(p.ID, ok, latency)
			if !ok {
				slog.Debug("proxy probe failed", "proxy", p.Name, "id", p.ID)
			}
		}(p)
	}
	wg.Wait()
}

// probe sends one request through the proxy and reports reachability
// plus the round-trip latency in milliseconds.
func (pr *Prober) probe(ctx context.Context, p *provider.Proxy) (bool, int64) {
	u, err := url.Parse(p.URL())
	if err != nil || u.Host == "" {
		return false, 0
	}
	client := &http.Client{
		Timeout:   pr.timeout,
		Transport: &http.Transport{Proxy: http.ProxyURL(u)},
	}
	cctx, cancel := context.WithTimeout(ctx, pr.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, pr.target, nil)
	if err != nil {
		return false, 0
	}
	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return false, latency
	}
	_ = resp.Body.Close()
	return true, latency
}
