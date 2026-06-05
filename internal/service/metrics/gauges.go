package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// upstreamFailoverTotal counts how often a request abandoned one upstream
// account and rolled over to another. Labelled by provider only (bounded).
var upstreamFailoverTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "omnihub_upstream_failover_total",
	Help: "Times the gateway abandoned an upstream account mid-request and failed over.",
}, []string{"provider"})

// IncFailover records one account-to-account failover for a provider.
func IncFailover(provider string) {
	upstreamFailoverTotal.WithLabelValues(label(provider)).Inc()
}

// AccountGauge is one account's point-in-time gauge sample, collected at
// scrape time. CircuitState is the health.CircuitState string
// ("closed"/"half-open"/"open"); the metrics package maps it to a code so
// it need not import the health package.
type AccountGauge struct {
	AccountName  string
	CircuitState string
	SpendUSD     float64
	HasSpend     bool // false when the account has no measured spend
}

// GaugeSource returns the current per-account gauge samples. It is invoked
// on every /metrics scrape, so it must be cheap (in-memory reads only).
type GaugeSource func() []AccountGauge

var (
	circuitStateDesc = prometheus.NewDesc(
		"omnihub_circuit_state",
		"Per-account circuit breaker state (0=closed, 1=half-open, 2=open).",
		[]string{"account"}, nil,
	)
	accountSpendDesc = prometheus.NewDesc(
		"omnihub_account_spend_usd",
		"Per-account rolling-24h spend in USD (accounts with a configured cap only).",
		[]string{"account"}, nil,
	)
)

type accountCollector struct{ src GaugeSource }

func (c accountCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- circuitStateDesc
	ch <- accountSpendDesc
}

func (c accountCollector) Collect(ch chan<- prometheus.Metric) {
	for _, g := range c.src() {
		name := label(g.AccountName)
		ch <- prometheus.MustNewConstMetric(
			circuitStateDesc, prometheus.GaugeValue, circuitCode(g.CircuitState), name)
		if g.HasSpend {
			ch <- prometheus.MustNewConstMetric(
				accountSpendDesc, prometheus.GaugeValue, g.SpendUSD, name)
		}
	}
}

// circuitCode maps a circuit state to its numeric gauge value, ordered by
// severity so alerts can threshold on ">= 1" or "== 2".
func circuitCode(state string) float64 {
	switch state {
	case "open":
		return 2
	case "half-open":
		return 1
	default:
		return 0
	}
}

// RegisterAccountGauges installs a collector that emits circuit-state and
// spend gauges from src on each scrape. A nil src is a no-op. Safe to call
// once at startup; a duplicate registration returns an error rather than
// panicking.
func RegisterAccountGauges(src GaugeSource) error {
	if src == nil {
		return nil
	}
	err := prometheus.DefaultRegisterer.Register(accountCollector{src: src})
	if _, dup := err.(prometheus.AlreadyRegisteredError); dup {
		return nil
	}
	return err
}
