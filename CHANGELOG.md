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

[Unreleased]: https://github.com/jami1024/omnihub/commits/main
