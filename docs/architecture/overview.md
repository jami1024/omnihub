# OmniHub Architecture Overview

> Status: **design phase, subject to change**. This document captures the initial design
> decisions for OmniHub. Detailed ADRs will follow under `docs/architecture/adr/`.

## 1. Goal

OmniHub is a unified AI gateway aiming to serve three audiences from a single Go binary:

| Mode         | Audience          | Key capabilities                                              |
| ------------ | ----------------- | ------------------------------------------------------------- |
| `team`       | Internal teams    | Multi-account scheduling, budgets, audit trail                |
| `enterprise` | Compliance-heavy  | SSO/RBAC, audit logs, regulated deployments                   |
| `saas`       | Commercial relay  | Plans, payment plugins, multi-level affiliate, key reselling  |

About 75% of the codebase is shared across modes. Mode-specific features are loaded as
opt-in modules at startup.

## 2. High-level dataflow

```
                  ┌────────────────────────────────────────────┐
                  │  Protocol entrypoints (OpenAI / Anthropic /  │
                  │  Gemini / MCP / Admin API)                  │
                  └──────────────────────┬─────────────────────┘
                                         │
                  ┌──────────────────────▼─────────────────────┐
                  │  Edge: Auth + ProtocolDetect + RateLimit   │
                  └──────────────────────┬─────────────────────┘
                                         │
                  ┌──────────────────────▼─────────────────────┐
                  │  Protocol Adapter Layer                    │
                  │  External protocol ⇄ internal UnifiedRequest │
                  └──────────────────────┬─────────────────────┘
                                         │
                  ┌──────────────────────▼─────────────────────┐
                  │  Guard Pipeline                             │
                  │  Quota → Session → Resolver → Circuit       │
                  │     → Forwarder → ResponseHandler           │
                  └──────────────────────┬─────────────────────┘
                                         │
        ┌──────────┬──────────┬──────────▼──────────┬──────────┬──────────┐
        │ Anthropic │  OpenAI  │ Bedrock │ Gemini │  Vertex  │  ...     │
        │  Driver   │  Driver  │  Driver │ Driver │  Driver  │  Driver  │
        └──────────┴──────────┴──────────┬──────────┴──────────┴──────────┘
                                         │
                  ┌──────────────────────▼─────────────────────┐
                  │  Async usage write + Prometheus / OTel     │
                  │  + Audit trail + Notifications              │
                  └────────────────────────────────────────────┘
```

## 3. Key design decisions

### 3.1 Single binary, embedded React UI

The web frontend is built with React + Vite, then embedded into the Go binary via
`go:embed`. There is no separate frontend service to deploy. This is inspired by
[sub2api](https://github.com/jami1024/sub2api)'s deployment model.

### 3.2 Layered architecture with hard boundaries

`internal/handler/` → `internal/service/` → `internal/repository/`.
These layers are enforced by `golangci-lint`'s `depguard` rule: a handler may not import
a repository directly, a service may not import a handler. Architecture-as-lint.

### 3.3 Unified internal request (IR)

External protocols (OpenAI / Anthropic / Gemini / Bedrock) are translated into a single
`UnifiedRequest` shape at the protocol adapter layer. All Guards and Drivers operate on the
IR. Drivers translate IR back to provider-specific wire formats on the way out.

The IR is a superset of OpenAI / Anthropic / Bedrock features: tools, vision, streaming,
cache control, extended thinking, etc. An `Extensions` field carries unknown / forward-compat
data.

### 3.4 Guard Pipeline

The request lifecycle is a chain of `Guard` implementations. Each Guard is independently
testable, has its own metrics labels, and can be replaced or extended by a plugin.

Default pipeline:

`Auth → RateLimit → Quota → ProtocolDetect → ModelRedirect → Session
 → Resolver → CircuitBreaker → Forwarder → Response`

### 3.5 Provider drivers — tiered model

Not every LLM provider needs to be a plugin. Drivers are tiered:

| Tier | Implementation                | Examples                                    |
| ---- | ----------------------------- | ------------------------------------------- |
| 1    | Manifest YAML, reuse OpenAI   | DeepSeek, Moonshot, Together, Groq          |
| 2    | Manifest + thin transform     | Cohere, Mistral direct                      |
| 3    | Built-in Go driver            | Anthropic, OpenAI, Bedrock, Gemini, Vertex  |
| 4    | gRPC out-of-process plugin    | Long-tail / experimental / private LLMs     |

Tiers 1 and 2 mean **adding a new OpenAI-compatible provider is zero code** — drop a YAML
manifest into `providers/`. Tier 3 ships in-process for the highest traffic providers.
Tier 4 is the extension point for the community.

### 3.6 Plugin architecture (gRPC, out-of-process)

OmniHub uses [HashiCorp go-plugin](https://github.com/hashicorp/go-plugin) for plugin
isolation. Plugins:

- Run as child processes, communicate over gRPC.
- Can be written in any language (Go, Node, Python, Rust).
- Are isolated — a plugin crash will not bring down the gateway.
- Are versioned independently of the core binary.

Planned plugin SPIs:

| SPI                  | Path           | Examples                                |
| -------------------- | -------------- | --------------------------------------- |
| `PaymentProvider`    | cold           | Stripe, Alipay, WeChat, custom          |
| `ProviderDriver`     | hot (tier 4)   | Long-tail LLM providers                 |
| `AuthProvider`       | cold           | OIDC, SAML, LDAP, enterprise IdP        |
| `NotificationSink`   | cold           | Slack, DingTalk, Feishu, email          |
| `AuditSink`          | cold           | Splunk, Elastic, Datadog                |
| `BackgroundJob`      | cold           | Custom scheduled tasks                  |

Hot-path components that need pluggable behaviour (rate limiter, session store, guardrails,
routing strategy) are exposed as **in-process Go interfaces with multiple implementations**,
not as IPC plugins. This avoids per-request IPC overhead on streaming responses.

### 3.7 Observability

- **Metrics** — `prometheus/client_golang`, exposed on `/metrics`.
- **Tracing** — OpenTelemetry SDK with configurable OTLP exporter.
- **Logging** — structured JSON via `log/slog` (stdlib) initially, may upgrade to
  `zerolog` if throughput becomes a concern.
- **Optional sinks** — Langfuse, Datadog, custom via plugins.
- **Decision-chain audit** — each request records which Guards ran, which provider was
  selected, and why.

### 3.8 Database & cache

- **PostgreSQL 16+** as the primary store (production deployments).
- **SQLite** as an embedded fallback for development and single-binary demos.
- **Redis 7+** for hot-path state: rate limit counters, session stickiness, circuit
  breaker state, prompt cache.

ORM choice (Ent vs sqlc vs raw `database/sql`) is still TBD — see future ADR.

### 3.9 Configuration

Config is loaded from YAML + environment variables (env wins). Sensitive values
(credentials, signing keys) are encrypted at rest in the database, never in config files.

## 4. Out of scope for v1

- Distributed tracing across multiple OmniHub instances (single-instance OTel is in scope).
- Read replicas / sharding (single-PG deployment is the v1 target).
- A managed cloud offering (the project is self-hosted only at launch).

## 5. Open questions

- Should the protocol adapter live as a Guard or as a separate pre-pipeline stage?
- Where do we draw the line between built-in driver and Tier-4 plugin driver?
- Do we ship a default guardrail engine, or rely entirely on plugins?

These will be resolved in follow-up ADRs.
