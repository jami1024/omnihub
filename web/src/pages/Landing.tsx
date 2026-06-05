import { Link } from 'react-router-dom'
import { useI18n } from '../lib/i18n'

// OmniHub marketing landing at "/". Brand register: a dark-capable
// "control plane" page that shows the real product (one endpoint, a
// provider pool, metering, observability) rather than describing it.
// Copy lives in a bilingual object — long-form prose is clearer here than
// dozens of flat i18n keys, and still switches with the app language.

const COPY = {
  en: {
    nav: { features: 'Routing', reliability: 'Reliability', billing: 'Billing', observability: 'Observability', signin: 'Sign in', start: 'Get started' },
    hero: {
      tag: 'Self-hosted AI gateway',
      title: 'One endpoint for every model you run.',
      sub: 'Put OmniHub in front of Claude, Claude on AWS, OpenAI and any OpenAI-compatible API. It pools your upstream accounts, fails over when one trips, and meters every request so you can price and resell access.',
      start: 'Get started',
      signin: 'Sign in',
      note: 'Pass-through, not a rewrite: matched-pair on /v1/messages and /v1/chat/completions.',
    },
    providers: { label: 'Speaks the native wire format of', items: ['Anthropic · Claude', 'Claude on AWS Bedrock', 'OpenAI', 'any OpenAI-compatible'] },
    routing: {
      title: 'Send one request. It finds a healthy account.',
      body: 'The resolver picks an upstream account by weight and priority, keeps a conversation on the same account for cache hits, and rolls over to the next one on a 5xx, a 429, or a dead connection. Per-account model redirects and parameter overrides happen on the way out.',
      points: ['Weighted + priority selection', 'Session stickiness for prompt cache', 'Multi-endpoint failover', 'Per-account model redirect'],
    },
    reliability: {
      title: 'A pool that stays up when an account does not.',
      body: 'A circuit breaker takes a failing account out of rotation before user traffic hits it, active health probes check upstreams off the request path, and failover timeouts are tuned so a dead endpoint is abandoned in seconds, not after a 30-second hang.',
      states: [
        { name: 'anthropic-1', state: 'closed', note: 'serving' },
        { name: 'anthropic-2', state: 'half-open', note: 'probing' },
        { name: 'openai-1', state: 'open', note: 'cooling down' },
      ],
    },
    billing: {
      title: 'Meter it, price it, resell it.',
      body: 'Every request records its cost and the amount billed. Give each user a prepaid wallet, a price ratio for your margin, and per-key daily caps. Hand out redemption codes for self-serve top-ups, or credit a balance from the console.',
      cards: [
        { k: 'Prepaid balance', v: 'Requests stop at $0.00, not a surprise invoice.' },
        { k: 'Price ratio', v: 'Charge cost × your markup, per user.' },
        { k: 'Redemption codes', v: 'Generate a batch, users redeem to top up.' },
      ],
    },
    observability: {
      title: 'See every request. Get paged when an account trips.',
      body: 'OmniHub exposes Prometheus metrics out of the box and ships an importable Grafana dashboard. When a circuit breaker opens or recovers, it notifies a webhook, Feishu, or DingTalk channel you manage from the console.',
      metrics: ['omnihub_ttfb_seconds', 'omnihub_cost_usd_total', 'omnihub_circuit_state', 'omnihub_upstream_failover_total'],
    },
    cta: { title: 'Run it on your own infrastructure.', body: 'One Go binary, Postgres, and your provider keys. Bring it up with Docker Compose and add an account.', start: 'Get started', },
    footer: { tagline: 'Self-hosted AI gateway.', portal: 'User portal', console: 'Admin console', rights: 'All rights reserved.' },
  },
  zh: {
    nav: { features: '路由', reliability: '可靠性', billing: '计费', observability: '可观测', signin: '登录', start: '开始使用' },
    hero: {
      tag: '自托管 AI 网关',
      title: '一个端点，接住你所有的模型。',
      sub: '把 OmniHub 放在 Claude、AWS 上的 Claude、OpenAI 以及任何 OpenAI 兼容 API 前面。它把上游账号汇成池、某个账号挂了自动切换、并为每次请求计量，让你能定价转售。',
      start: '开始使用',
      signin: '登录',
      note: '直通而非重写：/v1/messages 与 /v1/chat/completions 同协议匹配转发。',
    },
    providers: { label: '原生支持以下上游格式', items: ['Anthropic · Claude', 'AWS Bedrock 上的 Claude', 'OpenAI', '任意 OpenAI 兼容'] },
    routing: {
      title: '发一个请求，它自己找到健康的账号。',
      body: '解析器按权重和优先级挑选上游账号，把同一会话粘在同一账号上以命中缓存，遇到 5xx、429 或连接断开就切到下一个。每账号的模型重定向与参数覆盖在出站时完成。',
      points: ['权重 + 优先级选择', '会话粘性命中提示缓存', '多端点失败转移', '每账号模型重定向'],
    },
    reliability: {
      title: '某个账号挂了，池子照样在。',
      body: '熔断器在用户流量打到坏账号之前把它移出轮转，主动健康探测在请求路径之外检查上游，失败转移超时也调紧了：死端点几秒内放弃，而不是干等 30 秒。',
      states: [
        { name: 'anthropic-1', state: 'closed', note: '服务中' },
        { name: 'anthropic-2', state: 'half-open', note: '探测中' },
        { name: 'openai-1', state: 'open', note: '冷却中' },
      ],
    },
    billing: {
      title: '计量、定价、转售。',
      body: '每次请求都记录成本与计费金额。给每个用户一个预付费钱包、一个体现你毛利的价格倍率、以及每个 key 的日额度。发兑换码让用户自助充值，或在后台直接给余额充值。',
      cards: [
        { k: '预付费余额', v: '余额到 $0.00 就停，不会有意外账单。' },
        { k: '价格倍率', v: '按「成本 × 你的加价」计费，可按用户设。' },
        { k: '兑换码', v: '批量生成，用户兑换即充值。' },
      ],
    },
    observability: {
      title: '看见每个请求，账号一挂就收到告警。',
      body: 'OmniHub 开箱暴露 Prometheus 指标，并附带可导入的 Grafana 看板。熔断器打开或恢复时，它会通知你在后台管理的 webhook、飞书或钉钉渠道。',
      metrics: ['omnihub_ttfb_seconds', 'omnihub_cost_usd_total', 'omnihub_circuit_state', 'omnihub_upstream_failover_total'],
    },
    cta: { title: '跑在你自己的基础设施上。', body: '一个 Go 二进制、一个 Postgres、加上你的上游密钥。用 Docker Compose 拉起来，添加一个账号即可。', start: '开始使用' },
    footer: { tagline: '自托管 AI 网关。', portal: '用户门户', console: '管理后台', rights: '保留所有权利。' },
  },
}

export function LandingPage() {
  const { lang } = useI18n()
  const c = lang === 'zh' ? COPY.zh : COPY.en

  return (
    <div id="top" className="min-h-screen bg-bg text-ink">
      <BackdropGlow />
      <SiteNav c={c} />

      <main className="relative">
        {/* Hero */}
        <section className="mx-auto grid max-w-6xl items-center gap-12 px-6 pb-16 pt-16 lg:grid-cols-[1.05fr_0.95fr] lg:pb-24 lg:pt-24">
          <div className="reveal">
            <span className="badge badge-brand font-mono text-[11px]">{c.hero.tag}</span>
            <h1 className="mt-5 text-balance text-4xl font-semibold tracking-tight sm:text-5xl" style={{ letterSpacing: '-0.025em' }}>
              {c.hero.title}
            </h1>
            <p className="mt-5 max-w-xl text-pretty text-base leading-relaxed text-muted sm:text-lg">{c.hero.sub}</p>
            <div className="mt-7 flex flex-wrap items-center gap-3">
              <Link to="/portal/signup" className="btn btn-primary h-11 px-5 text-[15px]">{c.hero.start}</Link>
              <Link to="/portal/login" className="btn btn-secondary h-11 px-5 text-[15px]">{c.hero.signin}</Link>
            </div>
            <p className="mt-5 font-mono text-xs text-muted">{c.hero.note}</p>
          </div>
          <div className="reveal reveal-1">
            <RequestCard />
          </div>
        </section>

        {/* Providers */}
        <section className="mx-auto max-w-6xl px-6 pb-8">
          <div className="flex flex-col gap-4 border-y border-line py-6 sm:flex-row sm:items-center sm:gap-8">
            <span className="text-sm text-muted">{c.providers.label}</span>
            <div className="flex flex-wrap gap-x-6 gap-y-2">
              {c.providers.items.map((p) => (
                <span key={p} className="font-mono text-sm text-ink/80">{p}</span>
              ))}
            </div>
          </div>
        </section>

        {/* Routing */}
        <Feature id="routing" reverse title={c.routing.title} body={c.routing.body} visual={<RoutingDiagram />}>
          <ul className="mt-6 grid gap-2 sm:grid-cols-2">
            {c.routing.points.map((p) => (
              <li key={p} className="flex items-center gap-2 text-sm text-ink/85">
                <Check /> {p}
              </li>
            ))}
          </ul>
        </Feature>

        {/* Reliability */}
        <Feature id="reliability" title={c.reliability.title} body={c.reliability.body} visual={<PoolVisual states={c.reliability.states} />} />

        {/* Billing */}
        <section id="billing" className="mx-auto max-w-6xl px-6 py-16 lg:py-24">
          <div className="max-w-2xl">
            <h2 className="text-3xl font-semibold tracking-tight sm:text-4xl" style={{ letterSpacing: '-0.02em' }}>{c.billing.title}</h2>
            <p className="mt-4 text-pretty leading-relaxed text-muted">{c.billing.body}</p>
          </div>
          <div className="mt-10 grid gap-4 sm:grid-cols-3">
            {c.billing.cards.map((card, i) => (
              <div key={card.k} className="card reveal p-5" style={{ animationDelay: `${i * 70}ms` }}>
                <span className="inline-block h-2 w-2 rounded-sm" style={{ background: 'linear-gradient(135deg, var(--brand), var(--brand-2))' }} />
                <div className="mt-3 text-base font-semibold">{card.k}</div>
                <p className="mt-1.5 text-sm leading-relaxed text-muted">{card.v}</p>
              </div>
            ))}
          </div>
        </section>

        {/* Observability */}
        <Feature id="observability" reverse title={c.observability.title} body={c.observability.body} visual={<MetricsVisual metrics={c.observability.metrics} />} />

        {/* CTA band */}
        <section className="mx-auto max-w-6xl px-6 pb-20">
          <div className="relative overflow-hidden rounded-2xl border border-line-strong px-8 py-12 text-center sm:py-16" style={{ background: 'linear-gradient(180deg, var(--surface), var(--surface-2))' }}>
            <div className="pointer-events-none absolute inset-x-0 top-0 h-px" style={{ background: 'linear-gradient(90deg, transparent, var(--brand), transparent)' }} />
            <h2 className="mx-auto max-w-2xl text-balance text-3xl font-semibold tracking-tight sm:text-4xl" style={{ letterSpacing: '-0.02em' }}>{c.cta.title}</h2>
            <p className="mx-auto mt-4 max-w-xl text-pretty leading-relaxed text-muted">{c.cta.body}</p>
            <div className="mt-7 flex justify-center">
              <Link to="/portal/signup" className="btn btn-primary h-11 px-6 text-[15px]">{c.cta.start}</Link>
            </div>
          </div>
        </section>
      </main>

      <SiteFooter c={c} />
    </div>
  )
}

/* ── Chrome ──────────────────────────────────────────────────────── */

function SiteNav({ c }: { c: typeof COPY.en }) {
  return (
    <header className="sticky top-0 z-30 border-b border-line/70 bg-bg/80 backdrop-blur">
      <nav className="mx-auto flex h-16 max-w-6xl items-center justify-between px-6">
        <a href="#top" className="flex items-center gap-2">
          <Mark />
          <span className="text-[17px] font-semibold tracking-tight">OmniHub</span>
        </a>
        <div className="hidden items-center gap-7 md:flex">
          <a href="#routing" className="text-sm text-muted transition-colors hover:text-ink">{c.nav.features}</a>
          <a href="#reliability" className="text-sm text-muted transition-colors hover:text-ink">{c.nav.reliability}</a>
          <a href="#billing" className="text-sm text-muted transition-colors hover:text-ink">{c.nav.billing}</a>
          <a href="#observability" className="text-sm text-muted transition-colors hover:text-ink">{c.nav.observability}</a>
        </div>
        <div className="flex items-center gap-2">
          <Link to="/portal/login" className="btn btn-ghost h-9 px-3 text-sm">{c.nav.signin}</Link>
          <Link to="/portal/signup" className="btn btn-primary h-9 px-3.5 text-sm">{c.nav.start}</Link>
        </div>
      </nav>
    </header>
  )
}

function SiteFooter({ c }: { c: typeof COPY.en }) {
  return (
    <footer className="border-t border-line">
      <div className="mx-auto flex max-w-6xl flex-col gap-4 px-6 py-8 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-2">
          <Mark />
          <span className="text-sm font-semibold">OmniHub</span>
          <span className="text-sm text-muted">· {c.footer.tagline}</span>
        </div>
        <div className="flex items-center gap-6 text-sm text-muted">
          <Link to="/portal" className="transition-colors hover:text-ink">{c.footer.portal}</Link>
          <Link to="/admin" className="transition-colors hover:text-ink">{c.footer.console}</Link>
          <span className="font-mono text-xs">© {new Date().getFullYear()}</span>
        </div>
      </div>
    </footer>
  )
}

/* ── Feature row ─────────────────────────────────────────────────── */

function Feature({
  id, title, body, visual, reverse, children,
}: {
  id: string; title: string; body: string; visual: React.ReactNode; reverse?: boolean; children?: React.ReactNode
}) {
  return (
    <section id={id} className="mx-auto max-w-6xl px-6 py-16 lg:py-24">
      <div className={`grid items-center gap-12 lg:grid-cols-2 ${reverse ? '' : ''}`}>
        <div className={reverse ? 'lg:order-2' : ''}>
          <h2 className="text-balance text-3xl font-semibold tracking-tight sm:text-4xl" style={{ letterSpacing: '-0.02em' }}>{title}</h2>
          <p className="mt-4 max-w-xl text-pretty leading-relaxed text-muted">{body}</p>
          {children}
        </div>
        <div className={`reveal ${reverse ? 'lg:order-1' : ''}`}>{visual}</div>
      </div>
    </section>
  )
}

/* ── Visuals (honest UI / diagram motifs, not stock photos) ──────── */

function RequestCard() {
  return (
    <div className="card overflow-hidden p-0">
      <div className="flex items-center gap-1.5 border-b border-line px-4 py-3">
        <Dot className="bg-danger" /><Dot className="bg-warning" /><Dot className="bg-success" />
        <span className="ml-2 font-mono text-xs text-muted">POST /v1/messages</span>
      </div>
      <div className="space-y-2.5 p-4 font-mono text-[13px] leading-relaxed">
        <div className="text-muted"><span className="text-brand">$</span> curl gateway/v1/messages -d model=claude-sonnet-4</div>
        <div className="flex items-center gap-2 text-ink/85"><Arrow /> resolve · <span className="text-ink">anthropic-3</span> <Pill tone="success">closed</Pill></div>
        <div className="flex items-center gap-2 text-muted">stickiness · same account for the session</div>
        <div className="flex items-center gap-2 text-ink/85"><span className="text-success">←</span> 200 · ttfb <span className="text-ink">480ms</span> · billed <span className="text-ink">$0.0041</span></div>
      </div>
    </div>
  )
}

function RoutingDiagram() {
  const providers = [
    { n: 'claude', s: 'closed' as const },
    { n: 'bedrock', s: 'closed' as const },
    { n: 'openai', s: 'open' as const },
  ]
  return (
    <div className="card p-6">
      <div className="grid grid-cols-[auto_1fr] items-center gap-x-5 gap-y-4">
        <Node label="client" />
        <Wire />
        <Node label="omnihub" accent />
        <div className="flex flex-wrap gap-2">
          {providers.map((p) => (
            <span key={p.n} className="inline-flex items-center gap-1.5 rounded-lg border border-line bg-surface-2 px-2.5 py-1 font-mono text-xs">
              {p.n} <Pill tone={p.s === 'open' ? 'danger' : 'success'}>{p.s}</Pill>
            </span>
          ))}
        </div>
      </div>
      <p className="mt-5 border-t border-line pt-4 font-mono text-xs text-muted">openai · open → failover to next healthy account</p>
    </div>
  )
}

function PoolVisual({ states }: { states: ReadonlyArray<{ name: string; state: string; note: string }> }) {
  return (
    <div className="card p-2">
      {states.map((s, i) => (
        <div key={s.name} className={`flex items-center justify-between gap-4 rounded-lg px-4 py-3.5 ${i % 2 ? 'bg-surface-2' : ''}`}>
          <div className="flex items-center gap-3">
            <CircuitGlyph state={s.state} />
            <span className="font-mono text-sm">{s.name}</span>
          </div>
          <div className="flex items-center gap-3">
            <span className="text-xs text-muted">{s.note}</span>
            <Pill tone={s.state === 'open' ? 'danger' : s.state === 'half-open' ? 'warning' : 'success'}>{s.state}</Pill>
          </div>
        </div>
      ))}
    </div>
  )
}

function MetricsVisual({ metrics }: { metrics: ReadonlyArray<string> }) {
  return (
    <div className="card p-6">
      <Sparkline />
      <div className="mt-5 grid gap-2 border-t border-line pt-5">
        {metrics.map((m) => (
          <div key={m} className="flex items-center gap-2 font-mono text-[13px] text-ink/85">
            <span className="text-brand">#</span> {m}
          </div>
        ))}
      </div>
      <div className="mt-5 flex flex-wrap gap-2">
        <Pill tone="neutral">webhook</Pill>
        <Pill tone="neutral">feishu</Pill>
        <Pill tone="neutral">dingtalk</Pill>
        <span className="text-xs text-muted">alert on circuit open / recover</span>
      </div>
    </div>
  )
}

/* ── Primitives ──────────────────────────────────────────────────── */

function Mark() {
  return (
    <span className="flex h-8 w-8 items-center justify-center rounded-lg" style={{ background: 'linear-gradient(135deg, var(--brand), var(--brand-2))' }}>
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden>
        <circle cx="12" cy="12" r="2.6" fill="white" />
        <circle cx="5" cy="6" r="1.8" fill="white" opacity="0.7" />
        <circle cx="19" cy="6" r="1.8" fill="white" opacity="0.7" />
        <circle cx="5" cy="18" r="1.8" fill="white" opacity="0.7" />
        <circle cx="19" cy="18" r="1.8" fill="white" opacity="0.7" />
      </svg>
    </span>
  )
}

function BackdropGlow() {
  return (
    <div aria-hidden className="pointer-events-none fixed inset-0 -z-10 overflow-hidden">
      <div className="absolute left-1/2 top-[-12%] h-[42rem] w-[42rem] -translate-x-1/2 rounded-full opacity-60" style={{ background: 'radial-gradient(closest-side, var(--glow), transparent)' }} />
    </div>
  )
}

function Node({ label, accent }: { label: string; accent?: boolean }) {
  return (
    <span className={`inline-flex items-center rounded-lg border px-3 py-1.5 font-mono text-sm ${accent ? 'border-line-strong bg-brand-subtle text-brand' : 'border-line bg-surface-2'}`}>
      {label}
    </span>
  )
}

function Wire() {
  return <div className="h-px w-full" style={{ background: 'linear-gradient(90deg, var(--border-strong), var(--brand))' }} />
}

function CircuitGlyph({ state }: { state: string }) {
  const color = state === 'open' ? 'var(--danger)' : state === 'half-open' ? 'var(--warning)' : 'var(--success)'
  return <span className="inline-block h-2.5 w-2.5 rounded-full" style={{ background: color, boxShadow: `0 0 0 3px color-mix(in oklch, ${color} 18%, transparent)` }} />
}

function Sparkline() {
  return (
    <svg viewBox="0 0 320 64" className="h-16 w-full" preserveAspectRatio="none" aria-hidden>
      <polyline points="0,44 32,40 64,46 96,30 128,34 160,20 192,26 224,14 256,22 288,10 320,16" fill="none" stroke="var(--brand)" strokeWidth="2" strokeLinejoin="round" strokeLinecap="round" />
      <polyline points="0,44 32,40 64,46 96,30 128,34 160,20 192,26 224,14 256,22 288,10 320,16 320,64 0,64" fill="var(--glow)" stroke="none" />
    </svg>
  )
}

function Pill({ tone, children }: { tone: 'success' | 'danger' | 'warning' | 'neutral'; children: React.ReactNode }) {
  const cls = tone === 'success' ? 'badge-success' : tone === 'danger' ? 'badge-danger' : tone === 'warning' ? 'badge-warning' : 'badge-neutral'
  return <span className={`badge ${cls} text-[11px]`}>{children}</span>
}

function Dot({ className }: { className: string }) {
  return <span className={`h-2.5 w-2.5 rounded-full ${className}`} />
}

function Arrow() {
  return <span className="text-brand">→</span>
}

function Check() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden className="shrink-0 text-brand">
      <path d="M3.5 8.5l3 3 6-7" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}
