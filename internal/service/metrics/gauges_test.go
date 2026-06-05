package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestIncFailover(t *testing.T) {
	before := testutil.ToFloat64(upstreamFailoverTotal.WithLabelValues("anthropic"))
	IncFailover("anthropic")
	IncFailover("anthropic")
	if got := testutil.ToFloat64(upstreamFailoverTotal.WithLabelValues("anthropic")); got != before+2 {
		t.Errorf("failover total = %v, want %v", got, before+2)
	}
}

func TestCircuitCode(t *testing.T) {
	cases := map[string]float64{"closed": 0, "half-open": 1, "open": 2, "": 0, "weird": 0}
	for in, want := range cases {
		if got := circuitCode(in); got != want {
			t.Errorf("circuitCode(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestAccountCollector(t *testing.T) {
	c := accountCollector{src: func() []AccountGauge {
		return []AccountGauge{
			{AccountName: "acc-open", CircuitState: "open", SpendUSD: 4.5, HasSpend: true},
			{AccountName: "acc-closed", CircuitState: "closed"}, // no spend → gauge omitted
		}
	}}

	const want = `
# HELP omnihub_account_spend_usd Per-account rolling-24h spend in USD (accounts with a configured cap only).
# TYPE omnihub_account_spend_usd gauge
omnihub_account_spend_usd{account="acc-open"} 4.5
# HELP omnihub_circuit_state Per-account circuit breaker state (0=closed, 1=half-open, 2=open).
# TYPE omnihub_circuit_state gauge
omnihub_circuit_state{account="acc-closed"} 0
omnihub_circuit_state{account="acc-open"} 2
`
	if err := testutil.CollectAndCompare(c, strings.NewReader(want),
		"omnihub_circuit_state", "omnihub_account_spend_usd"); err != nil {
		t.Fatalf("collector output mismatch: %v", err)
	}
}

func TestRegisterAccountGaugesNilIsNoOp(t *testing.T) {
	if err := RegisterAccountGauges(nil); err != nil {
		t.Fatalf("nil source should be a no-op, got %v", err)
	}
}
