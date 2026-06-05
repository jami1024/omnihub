# Grafana

`omnihub-dashboard.json` is an importable starting dashboard for the
metrics OmniHub exposes at `/metrics`.

## Import

1. Point a Prometheus instance at the gateway's `/metrics` endpoint, e.g.:

   ```yaml
   scrape_configs:
     - job_name: omnihub
       # If OMNIHUB_METRICS_TOKEN is set, add:
       #   authorization: { credentials: "<token>" }
       static_configs:
         - targets: ["omnihub:8080"]
   ```

2. In Grafana: **Dashboards → New → Import**, upload the JSON, and select
   your Prometheus data source for the `DS_PROMETHEUS` input.

## Panels

Request rate by status · error ratio · cost/hour · request-latency p95 ·
upstream TTFB p95 · tokens/sec · failover rate · per-account circuit
state (0 closed / 1 half-open / 2 open) · per-account 24h spend.

It is a baseline — adjust queries, thresholds, and add alerts to taste.
