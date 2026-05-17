# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Initial project scaffolding (`README`, `.gitignore`, `Makefile`).
- Minimal HTTP entry point at `cmd/omnihub` with `/healthz`, `/readyz`, `/version` endpoints.
- Architecture overview document under `docs/architecture/`.
- Adopt `gin-gonic/gin` as the HTTP router and middleware stack.
- ADR 0001 records the rationale for choosing Gin over chi / Echo / Fiber.
- Add Simplified Chinese README (`README.zh-CN.md`) and cross-link with English.
- Define internal intermediate representation (`internal/ir/`): `UnifiedRequest`,
  `UnifiedResponse`, `UnifiedChunk`, plus message / content block / tool / usage
  types. This is the shared contract between protocol adapters, drivers, and Guards.
- Define the `Driver` contract under `internal/service/provider/`: `Driver` and
  `StreamIter` interfaces, plus `Account`, `Capabilities`, and a thread-safe
  `Registry` for runtime lookup. Drivers own pure transformation; the Forwarder
  Guard owns transport and retry.
- Implement the Anthropic driver (`internal/service/provider/drivers/anthropic/`):
  builds signed Messages API requests (x-api-key + anthropic-version + optional
  anthropic-beta header), parses non-streaming responses straight into IR, and
  decodes SSE streaming responses chunk-by-chunk via an explicit Close()
  iterator. Header-only fields (`anthropic_version`, `anthropic_beta`) are
  stripped from the request body.
- Wire end-to-end forwarding: `internal/service/forward/` owns the shared HTTP
  client (tuned for high-concurrency LLM forwarding) and pipes either streaming
  SSE or non-streaming bodies back to the client. `internal/handler/gateway/`
  exposes the Anthropic-compatible `/v1/messages` endpoint and lifts
  `anthropic-beta` from the HTTP header into the IR.
- `cmd/omnihub` now mounts `/v1/messages` when `OMNIHUB_ANTHROPIC_API_KEY` is
  set, so a single Anthropic account can be forwarded end-to-end.
- Add the Claude Platform on AWS driver
  (`internal/service/provider/drivers/claudeplatform/`). Composes with the
  Anthropic driver to reuse body marshalling, response parsing, and SSE
  decoding; overrides only BuildRequest to target the regional
  `aws-external-anthropic` endpoint, set the `anthropic-workspace-id`
  header, and authenticate via the AWS-issued `x-api-key`. SigV4 auth
  is intentionally deferred to the Bedrock work that already depends
  on the AWS SDK.
- Export `anthropic.MessagesBody` and `anthropic.ToWireBody` so sibling
  drivers sharing the Anthropic wire format do not duplicate the schema.
- `cmd/omnihub` now picks between Claude Platform and direct Anthropic
  from env vars; Claude Platform takes precedence when both are
  configured. Required env: `OMNIHUB_CLAUDE_PLATFORM_API_KEY`,
  `OMNIHUB_CLAUDE_PLATFORM_REGION`, `OMNIHUB_CLAUDE_PLATFORM_WORKSPACE_ID`.
- Introduce the Guard chain home (`internal/service/guard/`):
  - `Authenticator` parses a comma-separated `OMNIHUB_API_KEYS` spec
    supporting optional `label:key` syntax and validates incoming
    requests via constant-time comparison against `x-api-key` or
    `Authorization: Bearer`. Empty spec is allowed (auth disabled) but
    logs a warning so the operator knows the gateway is open.
  - `RequestLog` emits one structured slog line per request with method,
    path, status, duration, virtual-key label, response size, and the
    upstream model when the handler sets it. Health endpoints are
    skipped.
  - Helpers `guard.KeyName`, `guard.Model`, `guard.Stream` expose the
    well-known context keys to handlers without leaking string literals.
- `/v1/messages` is now mounted behind the auth + log guard chain; the
  health endpoints remain public.
- Add PostgreSQL scaffolding under `internal/db/`:
  - `pgx/v5` connection pool with explicit MaxConns / MaxConnLifetime
    tuning and a 5 s start-up Ping.
  - Forward-only migration runner (~120 lines, no external migrate
    library) that walks embedded `migrations/*.sql` files in lexical
    order, records applied migrations in `schema_migrations`, and runs
    each as its own transaction so an aborted migration cannot land
    half-applied.
  - First migration creates `message_requests` (token usage / latency
    / status / model / account columns plus three indexes) — the
    target for the upcoming WriteBuffer.
- `/readyz` now reports `503` when the database is unreachable so
  load balancers can drain unhealthy instances. `/healthz` stays a
  pure liveness probe.
- `cmd/omnihub` opens the pool and runs migrations at startup when
  `OMNIHUB_DATABASE_URL` is set. An empty DSN keeps the process in
  log-only mode (no persistence) — useful for local smoke tests and
  for the existing MVP flow.
- Add `deploy/docker-compose.dev.yaml` for a one-command local
  PostgreSQL (`docker compose -f ... up -d`).
- Add multi-stage `Dockerfile`: golang:1.26-alpine builder produces a
  static binary stripped via `-trimpath -ldflags '-s -w'` with version
  metadata; alpine:3.20 runtime adds CA certs, tzdata, and a non-root
  user. Final image ≈ 30–40 MB. `HEALTHCHECK` probes `/healthz` every
  10 s and `LABEL`s tie the image back to the GitHub repo.
- Add production-shape `deploy/docker-compose.yaml` and
  `deploy/.env.example`: PostgreSQL is internal-only; only the gateway
  listens on the host. `depends_on: service_healthy` makes the
  gateway wait for PG before starting. Defaults to
  `ghcr.io/jami1024/omnihub:latest`; a commented `build:` stanza
  builds from source before the first image is published.
- `.dockerignore` keeps the build context small (excludes git
  metadata, build outputs, secrets, frontend artefacts).
- Persist one row per request in `message_requests` when the DB is
  configured:
  - `internal/repository/message_request.go` — bulk-INSERT repo over
    pgx using parameterised multi-row VALUES.
  - `internal/repository/write_buffer.go` — async batched writer:
    250 ms / 200 rows / 5 000 max-pending cap, serialised flushes,
    failure puts the batch back at the head of the queue, drop-oldest
    under overflow, two-pass drain on Stop. Modelled after
    claude-code-hub's MessageRequestWriteBuffer.
  - `internal/service/usage/usage.go` — Anthropic-format usage
    extraction. Non-streaming bodies parse straight from JSON;
    streaming responses use an `SSESniffer` that merges
    `message_start` (input + cache breakdown + ids) with
    `message_delta` (final output_tokens + stop reason).
  - `forward.Forwarder.Forward` now returns a `Result` carrying
    StatusCode, Usage, and TTFB (streaming only). The forwarder feeds
    every SSE line to the sniffer in parallel with passing it to the
    client.
  - The `/v1/messages` handler builds a complete `MessageRequest`
    record at request end and enqueues it on the buffer. The buffer
    is nil-safe so log-only mode still works without a DB.
  - The `RequestLog` guard now emits `input_tokens`, `output_tokens`,
    optional cache counts, `actual_model`, `upstream_request_id`,
    and `ttfb_ms` when present.
- `cmd/omnihub` wires the WriteBuffer to the process lifecycle:
  initialised together with the DB pool, drained on shutdown via a
  15 s timeout so in-flight inserts complete before the binary exits.
- Compute USD cost per request:
  - `internal/service/pricing` ships a hard-coded Anthropic table
    covering Claude Haiku / Sonnet / Opus 4.5 / 4.6 / 4.7. Lookup uses
    longest-prefix match so `claude-haiku-4-5-20251001` resolves to
    the `claude-haiku-4-5` row.
  - The handler resolves the cost from the upstream-reported model
    (falling back to the requested alias) and stores the float64 USD
    on the `MessageRequest` record. Unknown models log a warning and
    persist NULL.
  - Migration `0002_add_cost_usd.sql` adds `cost_usd NUMERIC(12, 6)`
    plus a partial index on non-NULL values for quick cost rollups.
  - `RequestLog` emits `cost_usd` so log-only deployments see cost too.
  - 6 unit tests covering known/unknown models, prefix match,
    cache-token charging, and longest-prefix tie-break.
- **Fix Anthropic price sheet** to match the official
  platform.claude.com pricing page (2026-05):
  - Opus 4.7 is **$5/$25 per MTok**, not $15/$75 — Anthropic reduced
    Opus rates on 2026-04-16. Cache fields adjusted to the canonical
    ratios off input (5m = 1.25×, 1h = 2.00×, read = 0.10×).
  - Removed the non-existent `claude-sonnet-4-7` entry; 4.7 is
    Opus-only as of 2026-05.
  - Added legacy `claude-opus-4-1` so older alias hits the right row.
- Align pricing data model with claude-code-hub / LiteLLM conventions:
  - `Price` fields renamed to per-token (`InputCostPerToken`, …) with
    `json:` tags matching LiteLLM's
    `model_prices_and_context_window.json` snake_case names. A future
    commit can sync upstream prices without touching the struct.
  - `Calculate` now returns a `Breakdown` (input / output /
    cache_creation_5m / cache_creation_1h / cache_read / total /
    multiplier) instead of a single float so analytics can answer
    "where did the spend go". Breakdown.Total carries the rolled-up
    cost for callers that only need a number.
  - Canonical cache-rate fallbacks (5m = 1.25×, 1h = 2.0×,
    read = 0.10× of input) kick in when a thin upstream entry omits
    explicit cache prices.
  - `Account.CostMultiplier` scales every bucket; the applied factor
    is preserved in the persisted breakdown so analytics can recover
    the base upstream cost.
  - Migration `0003_add_cost_breakdown.sql` adds a JSONB column;
    `message_requests.cost_breakdown` is populated alongside the
    existing `cost_usd` total. Eight unit tests cover Opus 4.7 new
    pricing, cache fallback ratios, prefix tie-break, and multiplier
    application.
- **Multi-account support + Resolver Guard** (replaces single-account
  env-var configuration):
  - Migration `0004_accounts.sql` adds the `accounts` table
    (name, provider, enabled, weight, priority, cost_multiplier,
    base_url, credentials JSONB).
  - `internal/repository/account.go` provides `ListEnabled`,
    `CountAll`, and `Insert`. Credentials are cleartext JSONB in MVP;
    encryption at rest is a follow-up commit.
  - `internal/service/account/Pool` is the in-memory cache. It
    refreshes from the repository every 30 s in a background
    goroutine. A failed refresh leaves the previous snapshot in place
    so a DB blip never drains the routable set.
  - `internal/service/resolver` implements priority-tiered weighted-
    random selection. Lower `priority` is the preferred tier; inside
    a tier, accounts are picked proportional to `weight`. The
    resolver looks up the driver from the registry by
    `account.provider`, so adding a driver row to the DB immediately
    makes it available for routing.
  - Both `anthropic` and `claude-platform` drivers are now registered
    on every startup; the resolver freely mixes accounts of either
    provider for `/v1/messages` requests.
  - First-boot migration: if the `accounts` table is empty AND
    `OMNIHUB_ANTHROPIC_API_KEY` / `OMNIHUB_CLAUDE_PLATFORM_*` env
    vars are set, the gateway auto-inserts one matching row so
    existing deployments upgrade transparently.
  - When no DB is configured (log-only mode), the pool sources from
    env vars directly in memory — smoke tests still work.
  - Seven resolver tests cover empty-pool error, priority bucket
    filtering, weighted distribution, allowed-provider filtering,
    driver lookup, missing driver rejection, and zero-weight
    fallback to uniform.

[Unreleased]: https://github.com/jami1024/omnihub/commits/main
