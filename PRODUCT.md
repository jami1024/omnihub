# OmniHub

## Register

product

OmniHub is mostly app UI (admin console + end-user portal). The marketing
landing page at `/` is a brand surface handled in brand register; everything
else (dashboard, accounts, keys, wallet, settings) is product register.

## What it is

A self-hosted AI gateway. It puts one endpoint in front of many upstream
providers (Anthropic/Claude, Claude Platform on AWS, OpenAI and any
OpenAI-compatible API), routes each request through a pool of upstream
accounts with weighted/priority selection, session stickiness, circuit
breakers and multi-endpoint failover, then meters and bills it.

Three pillars:
- **One endpoint, many providers** — matched-pair pass-through (no
  cross-protocol re-rendering); `/v1/messages` and `/v1/chat/completions`.
- **Account pool reliability** — weighted/priority resolver, session
  stickiness, circuit breaker, active health probes, fast failover.
- **Operate & resell** — prepaid wallet balance, redemption codes,
  per-user price ratio (markup), per-key/daily caps; Prometheus metrics,
  Grafana dashboard, and alerting (webhook/Feishu/DingTalk) on account
  health.

## Users & purpose

Technical operators who run an AI gateway for a team or resell access:
they add upstream accounts, hand out virtual keys, set prices and credit
balances, and watch reliability. Their context is a control plane — they
want density, truth, and fast answers, not hand-holding. The end users of
those operators sign in to a lightweight portal to manage their own keys,
see request logs, and top up a wallet.

## Brand personality

Precise, technical, calm under load. Three words: **instrumented,
graphite, dependable**. It should feel like a control plane an SRE trusts
at 3am, not a consumer SaaS funnel. Honesty over hype: show the real
endpoint, the real metric name, the real circuit state.

## Anti-references

- SaaS-cream / warm-neutral landing pages with a hero-metric template.
- Editorial-typographic (display-serif + italic + mono-label) brand lane.
- Buzzword copy (streamline / supercharge / seamless / enterprise-grade).
- Stock-photo "team collaboration" imagery; the product is infrastructure,
  so imagery is honest UI/diagram motifs, not people at laptops.

## Accessibility

WCAG AA contrast (body ≥ 4.5:1), full light + dark themes (already in the
token system), visible keyboard focus, and a reduced-motion path for every
animation.
