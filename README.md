# OmniHub

> **Unified AI gateway with pluggable providers, payments, and observability — built in Go.**

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Status: Early](https://img.shields.io/badge/status-early%20development-orange)]()
[![Go Version](https://img.shields.io/badge/go-1.24+-00ADD8?logo=go)]()

OmniHub is an open-source AI gateway that unifies access to OpenAI, Anthropic, AWS Bedrock,
Google Gemini, Vertex AI, Azure OpenAI and more — behind a single OpenAI-compatible API.

It is designed to serve three audiences from a single codebase, switchable by configuration:

- **Teams** — internal multi-account scheduling, budget control, audit trail.
- **Enterprises** — SSO/RBAC, compliance audit logs, multi-tenant governance.
- **SaaS operators** — plan management, payment plugins, multi-level affiliate.

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
