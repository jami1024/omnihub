# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Per-key RPM enforcement (`internal/service/limits/rpm.go`): the
  `Limiter` now also honours `api_keys.rpm_limit`. Backed by a
  `golang.org/x/time/rate` token bucket per key — refills at
  `rpm/60` tokens per second with burst = `rpm`, so a key with
  `rpm=60` may fire 60 requests instantly and then waits one second
  per subsequent request. Exhausted buckets return HTTP 429
  `rate_limit_exceeded`. When an operator updates a key's
  `rpm_limit`, the cached bucket is rebuilt on the next request so
  the new policy takes effect immediately (any goodwill in the old
  bucket is discarded). Buckets are pure in-process state: zero DB
  cost on the hot path, ordered before the daily-USD check.
- Per-key policy enforcement (`internal/service/limits/`): the
  `Limiter` runs after authentication and rejects requests that
  violate either of two policies on the matching `api_keys` row.
  - **Model allow-list** (`allowed_models`): non-empty array rejects
    requests whose `model` is not listed, with HTTP 403
    `model_not_allowed`. Empty array means no restriction.
  - **Rolling 24h USD cap** (`daily_usd_limit`): rejects with HTTP
    429 `daily_limit_exceeded` once the SUM of `cost_usd` in
    `message_requests` over the past 24 hours reaches the limit.
    Backed by `SpendCache` — a per-key, 5-second-TTL cache that
    reloads from the DB on miss and is incrementally folded with
    each completed request via `RecordSpend`, so back-to-back calls
    against the same key see up-to-date totals without waiting for
    the WriteBuffer flush.
  - Fail-open semantics for the DB check: a transient Postgres
    error logs a warning and allows the request. Black-holing
    every call during a DB blip is a worse failure mode than a few
    minutes of unbilled usage.
  - `repository.MessageRequestRepo.SumCostByKey` is the
    authoritative source, served by the existing
    `message_requests(key_name, created_at DESC)` index — no new
    migration required.
  - Wired into the gateway handler before any upstream call, so a
    capped key burns zero upstream quota. The full `*apikey.Key` is
    now exposed via `guard.APIKey(c)` so downstream policies can
    read limit fields off context.
- Client User-Agent gate (`internal/service/guard/client_gate.go`): rejects
  non-Claude-CLI requests with HTTP 403 (`client_not_allowed`) BEFORE the
  authentication middleware runs, so scanners and curl one-liners do not
  consume a DB lookup. The allow-list is a comma-separated UA prefix list
  controlled by `OMNIHUB_ALLOWED_CLIENT_UA_PREFIXES`; the default is
  `claude-cli/`. Setting it to `*` opens the gate and logs a warning at
  startup. `deploy/.env.example` and `deploy/docker-compose.yaml` document
  and pass through the new variable.
- Multi-signal verification on the client gate: requests claiming to be
  `claude-cli/` must also carry `X-App: cli` and a non-empty
  `Anthropic-Beta` header — the headers a real Claude CLI emits — or
  they are rejected with a precise reason in the error body
  (`missing required header "X-App"` etc.). This closes the trivial
  UA-spoofing path (`curl -H 'User-Agent: claude-cli/...'`) inspired by
  claude-code-hub's `client-detector.ts` multi-signal rule, without
  reading the request body. Custom prefixes added by operators (e.g.
  `codex-cli/`) pass on UA match alone — signal enforcement is keyed to
  the prefix, not applied globally.

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
  - Seven resolver tests cover empty-pool error, priority bucket
    filtering, weighted distribution, allowed-provider filtering,
    driver lookup, missing driver rejection, and zero-weight
    fallback to uniform.
- **Health tracker + automatic failover** for upstream accounts:
  - `internal/service/health` ships a three-state circuit breaker per
    account (closed / open / half-open) modelled after claude-code-hub.
    Defaults: 5 consecutive failures trip → 30 s cooldown → 1 success
    in half-open closes the breaker. Disabled (FailureThreshold ≤ 0)
    by configuration if needed.
  - `forward.Forwarder` split into `Dispatch` + `WriteResponse` so the
    handler can read the upstream status code BEFORE committing any
    bytes to the client. Retriable failures (5xx + 429 + transport
    errors) roll over to a different account; once `WriteResponse`
    starts writing, the response is committed.
  - `forward.IsRetriable` exposes the retry policy.
  - `resolver.WeightedResolver` now accepts an `excludedAccountIDs`
    slice (skip accounts already tried in this request) and consults
    `health.Tracker` to filter out accounts whose circuit is open.
    A nil tracker preserves the old behaviour (no health filtering).
  - The `/v1/messages` handler runs a bounded retry loop
    (`maxFailoverAttempts = 3`). Each failure is recorded against the
    account, and the loop terminates when a non-retriable response is
    seen or the candidate pool is exhausted. Exhausted retries
    surface a 502 (or 429 if the last attempt was a 429) and the
    final attempt is persisted into `message_requests`.
  - Eleven health-tracker tests cover state transitions, cooldown
    timing, half-open promotion / demotion, disabled mode, per-account
    isolation, and concurrent recording (race-detectable).
  - Resolver tests gain `TestResolveExcludesAlreadyTriedIDs` and
    `TestResolveSkipsUnhealthyAccounts`.
  - **Session stickiness** for prompt-cache friendliness:
    - `internal/service/session` derives a per-conversation key as
      `sha256(virtualKey + model + system_prompt[:8KB] + first_user_text)`,
      stable across the turns of one chat and distinct across
      callers / prompts / conversations.
    - In-memory `session.Store` with default 5-minute TTL (matching
      Anthropic's 5-minute prompt-cache window). A background
      sweeper bounds memory to TTL × request rate.
    - The resolver checks the sticky binding first; on health /
      provider / exclusion mismatch it transparently falls back to
      fresh selection. New selections on the "clean" path bind to
      the session for future turns; retry-loop fallbacks do NOT
      bind (so a sick attempt does not freeze the session onto a
      sub-par account).
    - `OMNIHUB_SESSION_TTL` env var configures the TTL; the values
      `0`, `off`, or `false` disable stickiness entirely.
    - 12 unit tests cover key derivation stability + uniqueness,
      bind / get / TTL expiry / refresh, sticky re-use across
      iterations, fallback when bound account turns unhealthy, and
      the retry-loop "don't bind" guarantee.
  - **Virtual API keys as a first-class DB entity** — foundation
    for the upcoming Limits Guard (daily $, RPM, model allow-list):
    - Migration `0008_api_keys.sql` creates the `api_keys` table
      (name unique, sha256-hex `key_hash` unique, label, enabled,
      `daily_usd_limit`, `rpm_limit`, `allowed_models JSONB`,
      timestamps) and installs an `omnihub_api_keys_changed` NOTIFY
      trigger that mirrors the accounts pattern.
    - `internal/service/apikey` adds `Key`, `HashOf` (sha256 hex),
      `Generate` (32-byte random keys prefixed `omni-`), `Pool`
      indexed by hash, and a `Listener` with backoff reconnect.
    - `internal/repository/api_key.go` provides `ListEnabled`,
      `ListAll`, `CountAll`, `Insert`, `SetEnabled`, `Delete`.
    - `guard.Authenticator` now takes a `KeyLookup` callback; the
      gateway wires it to `apiKeyPool.LookupByHash(HashOf(submitted))`.
      The auth path therefore hits an O(1) in-memory map; database
      pressure remains zero on the hot path.
    - CLI `omnihub key add|list|enable|disable|delete`:
      - `add` generates a random 48-char key (or accepts `--key=...`),
        stores only the hash, and prints the cleartext **once** so
        operators can hand it to the user. Optional `--label`,
        `--daily-usd`, `--rpm`, `--allowed-models`, `--disabled`.
      - `list` renders a tab-aligned table without ever printing the
        hash or any secret material.
    - First-boot bootstrap: when the `api_keys` table is empty AND
      `OMNIHUB_API_KEYS` is set, the gateway hashes every legacy
      `label:key` entry and inserts it. Existing deployments upgrade
      transparently; the env var becomes unnecessary after the first
      boot (the DB is now the source of truth).
    - The `daily_usd_limit` / `rpm_limit` / `allowed_models`
      columns are populated but NOT yet enforced — Step 2 lights up
      the Limits Guard in a follow-up commit.
  - **Outbound header optimisations inspired by claude-code-hub and
    sub2api header analysis**:
    - The Forwarder now forces `Accept-Encoding: identity` on every
      upstream request. gzip / brotli compression interferes with
      streaming SSE because the transport-level decompressor buffers
      chunks before emitting them, breaking the per-event flush
      cadence. Tested by claude-code-hub the hard way; we apply it
      gateway-wide.
    - The IR gains a `ClientMetadata` map. The handler lifts an
      allow-list of SDK identifier headers from the inbound request
      (`x-stainless-lang`, `x-stainless-package-version`,
      `x-stainless-os`, `x-stainless-arch`, `x-stainless-runtime`,
      `x-stainless-runtime-version`, `x-stainless-retry-count`,
      `x-stainless-timeout`, `x-stainless-helper-method`, `x-app`,
      `x-claude-code-session-id`, `x-client-request-id`) into the IR,
      and both the Anthropic and Claude-Platform drivers emit them on
      the outbound request. The list is intentionally narrow:
      identifiers Anthropic uses for cache partitioning and analytics,
      with zero PII / IP / User-Agent leakage. Other inbound headers
      (User-Agent, IP, auth, transfer-encoding, etc.) remain stripped.
  - **Per-request client identity capture**:
    - Migration `0007_request_client_metadata.sql` adds `client_ip
      VARCHAR(45)` and `user_agent TEXT` columns to
      `message_requests` plus a partial index on client_ip for IP
      forensics.
    - The handler reads `c.ClientIP()` and `User-Agent` at entry and
      surfaces them on the gin.Context; the RequestLog guard emits
      them and the WriteBuffer persists them. The audit log now
      includes `client_ip` and `user_agent` fields per request.
    - Gin's `SetTrustedProxies` is wired via `OMNIHUB_TRUSTED_PROXIES`
      (comma-separated CIDR / IP / hostname; empty = trust no
      proxy). Default safely returns the immediate peer IP so a
      direct-exposed gateway is not spoofable; behind a reverse
      proxy, operators set the list to that proxy's range so
      `c.ClientIP()` reflects the real caller.
  - **Strip forwarding headers on outbound upstream calls**: the
    Forwarder explicitly removes `X-Forwarded-For`, `X-Real-IP`,
    `Forwarded`, `CF-Connecting-IP`, `True-Client-IP`, and the
    `X-Forwarded-Host` / `X-Forwarded-Proto` companions before
    dispatch. The upstream (Anthropic / Claude Platform) therefore
    only ever sees the gateway's outbound IP — no client identity
    leaks downstream.
  - **Instant account-pool refresh via PostgreSQL LISTEN/NOTIFY**:
    - Migration `0006_accounts_notify_trigger.sql` installs a
      statement-level trigger on the `accounts` table that fires
      `pg_notify('omnihub_accounts_changed', TG_OP)` on every
      INSERT / UPDATE / DELETE.
    - `internal/service/account/notify.go` runs a background
      listener that holds one pool connection, issues `LISTEN
      omnihub_accounts_changed`, and invokes `Pool.Refresh` on each
      notification. Reconnection is automatic with exponential
      backoff (1 s → 30 s) on transport errors, so a restarted DB
      or transient network blip self-heals without intervention.
    - The 30-second periodic refresh stays in place as a safety
      net: a notification missed during reconnect is still picked
      up by the next tick.
    - Account changes (CLI, SQL, or future admin API) now propagate
      to the routing pool in well under a second instead of waiting
      up to 30 seconds.
  - Per-account circuit-breaker overrides: the `accounts` table now
    carries three nullable columns (`circuit_failure_threshold`,
    `circuit_open_duration_ms`, `circuit_half_open_success`).
    NULL values inherit the env-driven gateway default; non-NULL
    values replace it for that single account. Migration
    `0005_account_circuit_overrides.sql` adds the columns with CHECK
    constraints. The `omnihub account add` CLI gains
    `--circuit-failure-threshold`, `--circuit-open-duration`, and
    `--circuit-half-open-success` flags. `health.Tracker` exposes
    `SetConfigLookup` so the gateway wires `accountPool.ByID` as the
    resolver, giving O(1) per-call cost on the hot path.
  - Global circuit-breaker thresholds are configurable via env vars:
    `OMNIHUB_CIRCUIT_FAILURE_THRESHOLD` (int, ≥0; 0 disables),
    `OMNIHUB_CIRCUIT_OPEN_DURATION` (Go duration, e.g. `30s`, `2m`),
    `OMNIHUB_CIRCUIT_HALF_OPEN_SUCCESS` (int, >0). Defaults match
    claude-code-hub (5 / 30 s / 1). Per-account DB-level overrides
    arrive in a follow-up commit.
- Account-management CLI baked into the main binary
  (`omnihub account ...`):
  - `omnihub account add` inserts a row with explicit flags so
    operators no longer write JSONB literals. Provider-specific
    credentials (`--api-key`, `--aws-region`, `--workspace-id`) plus
    routing knobs (`--weight`, `--priority`, `--cost-multiplier`,
    `--base-url`, `--disabled`).
  - `omnihub account list` prints a tab-aligned table including the
    enabled flag and credential KEYS — values are deliberately
    suppressed so secrets never leak to logs / pasted output.
  - `omnihub account enable|disable <name>` flips the enabled flag;
    the change becomes routable within the next pool refresh tick.
  - `omnihub account delete <name>` hard-deletes a row.
  - `omnihub help` / `omnihub version` for discoverability.
  - The same binary still runs the gateway via `omnihub` (no args)
    or `omnihub serve`; subcommands open their own short-lived
    Postgres pool (4 connections) and do not run migrations — the
    gateway is the canonical migration owner.
  - `repository.AccountRepo` gained `ListAll`, `SetEnabled`, and
    `Delete` to back the new subcommands. `ErrAccountNotFound`
    surfaces missing rows.
- **BREAKING:** Removed env-var based upstream-account bootstrap.
  `OMNIHUB_ANTHROPIC_API_KEY`, `OMNIHUB_CLAUDE_PLATFORM_API_KEY`,
  `OMNIHUB_CLAUDE_PLATFORM_REGION`, and
  `OMNIHUB_CLAUDE_PLATFORM_WORKSPACE_ID` are no longer read. Accounts
  live in the database; an empty `accounts` table leaves /v1/messages
  unmounted and the gateway logs an INSERT SQL snippet for the
  operator. Running without `OMNIHUB_DATABASE_URL` also leaves
  /v1/messages unmounted (log-only mode for smoke tests).

[Unreleased]: https://github.com/jami1024/omnihub/commits/main
