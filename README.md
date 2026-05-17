# OmniHub

**English** · [简体中文](README.zh-CN.md)

> **Commercial-grade unified AI gateway — pluggable providers, payments, and observability, built in Go.**

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Status: Early](https://img.shields.io/badge/status-early%20development-orange)]()
[![Go Version](https://img.shields.io/badge/go-1.24+-00ADD8?logo=go)]()

OmniHub is an open-source, **commercial-ready** AI gateway that unifies access to OpenAI,
Anthropic, AWS Bedrock, Google Gemini, Vertex AI, Azure OpenAI and more behind a single
OpenAI-compatible API.

One codebase, the full commercial spectrum:

- **Internal use** — multi-account scheduling, budget control, audit trail.
- **Compliance delivery** — SSO/RBAC, audit logs, multi-tenant governance.
- **External operation** — plan management, payment plugins, multi-level affiliate.

## ✨ Highlights

- 🔌 **Plugin-first** — payments, auth providers, notifications, and long-tail LLM providers
  all run as out-of-process gRPC plugins. Write your own in any language.
- 🚪 **Multi-protocol entry** — accept Anthropic Messages, OpenAI Chat Completions, Gemini,
  and Bedrock-style requests on the same gateway.
- 🧠 **Smart routing** — session stickiness, circuit breaker, weight / priority / tag-based
  strategies. Maximise prompt cache hit rate, minimise vendor lock-in.
- 💰 **Token-level billing** — virtual keys with per-key budgets, team/org rollup,
  real-time cost dashboard.
- 📊 **Observability built-in** — Prometheus metrics, OpenTelemetry tracing,
  decision-chain audit logs.
- 🚀 **Single binary** — Go backend with embedded React UI. Docker / Compose / K8s / systemd
  all supported.

## 🚧 Status

OmniHub is in **early development**. The architecture and SPI are being designed in public —
see [`docs/architecture/`](docs/architecture/) for design notes.

The first MVP targets Anthropic + OpenAI providers with a minimal Guard pipeline. Watch this
repo for updates.

## 🚀 Quick Start (development)

Requires Go 1.24+.

```bash
git clone https://github.com/jami1024/omnihub.git
cd omnihub
make run
```

The gateway listens on `:8080` by default. Verify it is up:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/version
```

Override the listen address with `OMNIHUB_LISTEN=:9000 make run`.

## 🏭 Production deploy (Docker + Caddy + Let's Encrypt)

`deploy/` ships a one-host stack: PostgreSQL + the gateway + Caddy
fronting them with auto-renewing TLS. Prereqs:

- Docker + Compose v2 on the target host.
- Ports 80 and 443 reachable from the public internet (firewall and
  any cloud security group must allow inbound).
- A DNS A and/or AAAA record for your domain already pointing at this
  host **before** the first `up` — Caddy will request the cert during
  startup via the HTTP-01 ACME challenge.

```bash
cp deploy/.env.example deploy/.env
$EDITOR deploy/.env       # set OMNIHUB_DOMAIN, POSTGRES_PASSWORD,
                          # OMNIHUB_API_KEYS, and one upstream
                          # credential block (Anthropic OR Claude
                          # Platform on AWS).

docker compose -f deploy/docker-compose.yaml --env-file deploy/.env up -d --build
```

First boot pulls Caddy, builds the gateway image locally, runs
migrations, and provisions the TLS cert. Watch progress with:

```bash
docker compose -f deploy/docker-compose.yaml --env-file deploy/.env logs -f
```

Once `caddy` logs "certificate obtained successfully" the gateway is
live at `https://${OMNIHUB_DOMAIN}/v1/messages`. Add upstream
accounts and virtual keys via the CLI inside the container:

```bash
docker exec -it omnihub-prod-gateway \
  omnihub account add --name=primary --provider=anthropic \
                      --api-key=sk-ant-...

docker exec -it omnihub-prod-gateway \
  omnihub key add --name=alice --daily-usd=50 --rpm=120
```

The cleartext key is printed once — store it.

## 📐 Architecture (preview)

```
Client → Edge (Auth / RateLimit) → Protocol Adapter → Guard Pipeline
       → Provider Driver (built-in or plugin) → Upstream LLM
       → Async usage write + Prometheus / OTel
```

Planned plugin SPIs (gRPC, out-of-process):

- `PaymentProvider` — Stripe, Alipay, WeChat, custom
- `ProviderDriver` — long-tail / experimental LLM providers
- `AuthProvider` — SSO, SAML, OAuth2, LDAP
- `NotificationSink` — Slack, DingTalk, Feishu, email
- `AuditSink` — SIEM systems
- `BackgroundJob` — custom scheduled tasks

See [`docs/architecture/overview.md`](docs/architecture/overview.md) for the full design.

## 🤝 Contributing

This project is in design phase — contributions welcome via [Issues](https://github.com/jami1024/omnihub/issues)
and [Discussions](https://github.com/jami1024/omnihub/discussions). A formal `CONTRIBUTING.md`
will land once we open up PRs.

## 📄 License

[MIT](LICENSE) © OmniHub contributors
