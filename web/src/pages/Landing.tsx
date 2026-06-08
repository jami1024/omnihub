import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { useI18n } from '../lib/i18n'
import type { Plan } from '../lib/plans'
import { PER_MILLION_TOKENS, type PublicModelPrice, type PublicPricing, usePublicPricing } from '../lib/publicPricing'

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
      title: 'One gateway for every model provider you run.',
      sub: 'OmniHub turns Claude, Claude on AWS, OpenAI, and compatible APIs into one reliable endpoint — with health-aware routing and per-request cost tracking.',
      start: 'Get started',
      signin: 'Sign in',
      note: 'Protocol pass-through for /v1/messages and /v1/chat/completions. No cross-format rewriting.',
    },
    providers: { label: 'Routes requests to the native API format of', items: ['Anthropic · Claude', 'Claude on AWS Bedrock', 'OpenAI', 'any OpenAI-compatible'] },
    routing: {
      title: 'Send one request. OmniHub chooses the account.',
      body: 'Each request goes through a resolver that checks priority, weight, account state, and session stickiness. If an upstream returns 5xx, 429, or drops the connection, OmniHub moves the request to the next available route.',
      points: ['Weighted and priority routing', 'Session stickiness for prompt cache', 'Endpoint failover per account', 'Model redirect before forwarding'],
    },
    reliability: {
      title: 'Keep the pool usable when one account fails.',
      body: 'Circuit breakers remove unstable accounts from rotation, health probes check upstreams outside the request path, and short failover timeouts prevent a dead endpoint from holding traffic for too long.',
      states: [
        { name: 'anthropic-1', state: 'closed', note: 'serving' },
        { name: 'anthropic-2', state: 'half-open', note: 'probing' },
        { name: 'openai-1', state: 'open', note: 'cooling down' },
      ],
    },
    billing: {
      title: 'Meter usage before you expose the endpoint.',
      body: 'Every request stores provider cost and billed amount. You can give users prepaid balances, set a price ratio per user, cap daily key spend, and issue redemption codes for self-serve top-ups.',
      pricingTitle: 'Plans make the effective price visible.',
      pricingBody: 'The public pricing block reads enabled plans and official model prices from OmniHub. Users can see the RMB payment amount, USD credit, and usage multiplier before they sign in.',
      officialLabel: 'Official model price',
      effectiveLabel: 'Plan effective price',
      ratioLabel: 'Usage multiplier',
      planFallback: 'Plan data is not available yet. Configure enabled plans in the admin console to show them here.',
      priceFallback: 'Official model prices are not available yet. Sync or add model prices in the admin console.',
      planPrice: 'plan price',
      included: 'included credit',
      creditValue: 'USD credit',
      modelSaving: 'model saving',
      noModelSaving: 'official rate',
      save: 'save',
      noOverage: 'No wallet overage',
      overage: 'Wallet overage allowed',
      days: 'days',
      input: 'Input',
      output: 'Output',
      perMillion: 'per 1M tokens',
      formula: 'Official price × multiplier',
      officialNote: 'Prices shown here are official model prices. OmniHub applies the selected plan multiplier when calculating usage.',
      model: 'Model',
      unlimited: 'unlimited',
      perDay: 'day',
      cards: [
        { k: 'Prepaid balance', v: 'Stop requests when balance reaches $0.00.' },
        { k: 'Price ratio', v: 'Bill each user at provider cost × your ratio.' },
        { k: 'Redemption codes', v: 'Generate top-up codes and let users redeem them.' },
      ],
    },
    observability: {
      title: 'Inspect traffic and account health from the console.',
      body: 'OmniHub exposes Prometheus metrics and ships an importable Grafana dashboard. When a circuit breaker opens or recovers, it sends a notification to the webhook, Feishu, or DingTalk channel you configure.',
      metrics: ['omnihub_ttfb_seconds', 'omnihub_cost_usd_total', 'omnihub_circuit_state', 'omnihub_upstream_failover_total'],
    },
    footer: { tagline: 'Self-hosted AI gateway.', portal: 'User portal', rights: 'All rights reserved.' },
  },
  zh: {
    nav: { features: '路由', reliability: '可靠性', billing: '计费', observability: '可观测', signin: '登录', start: '开始使用' },
    hero: {
      tag: '自托管 AI 网关',
      title: '一个网关，统一接入多家模型供应商。',
      sub: 'OmniHub 将 Claude、AWS 上的 Claude、OpenAI 和兼容 API 统一成一个稳定入口，自动避开异常账号，并记录每次请求成本。',
      start: '开始使用',
      signin: '登录',
      note: '协议直通：/v1/messages 与 /v1/chat/completions 按原格式转发，不做跨协议重写。',
    },
    providers: { label: '按上游原生 API 格式转发到', items: ['Anthropic · Claude', 'AWS Bedrock 上的 Claude', 'OpenAI', '任意 OpenAI 兼容'] },
    routing: {
      title: '发一个请求，由 OmniHub 选择账号。',
      body: '每次请求都会经过解析器，综合优先级、权重、账号状态和会话粘性来选择路由。遇到 5xx、429 或连接断开时，OmniHub 会把请求切到下一个可用线路。',
      points: ['权重与优先级路由', '会话粘性命中提示缓存', '账号内端点失败转移', '转发前模型重定向'],
    },
    reliability: {
      title: '单个账号异常，账号池仍然可用。',
      body: '熔断器会把不稳定账号移出轮转，健康探测在请求路径之外检查上游，较短的失败转移超时也能避免死端点长时间占住流量。',
      states: [
        { name: 'anthropic-1', state: 'closed', note: '服务中' },
        { name: 'anthropic-2', state: 'half-open', note: '探测中' },
        { name: 'openai-1', state: 'open', note: '冷却中' },
      ],
    },
    billing: {
      title: '先计量，再把端点交给用户。',
      body: '每次请求都会记录供应商成本和计费金额。你可以给用户配置预付费余额、按用户设置价格倍率、限制每个 key 的每日花费，并发放兑换码让用户自助充值。',
      pricingTitle: '套餐优惠和实际扣费一眼看清。',
      pricingBody: '落地页直接读取已启用套餐和官方模型价格。用户能在登录前看清人民币售价、美元额度、使用倍率，以及套餐后的实际扣费。',
      officialLabel: '官方模型价格',
      effectiveLabel: '套餐后价格',
      ratioLabel: '使用倍率',
      planFallback: '暂时没有可展示的套餐。请先在后台启用套餐。',
      priceFallback: '暂时没有可展示的官方模型价格。请先在后台同步或添加模型价格。',
      planPrice: '套餐售价',
      included: '包含额度',
      creditValue: '美元额度',
      modelSaving: '模型扣费优惠',
      noModelSaving: '官方价扣费',
      save: '约省',
      noOverage: '不允许钱包超额',
      overage: '允许钱包超额',
      days: '天',
      input: '输入',
      output: '输出',
      perMillion: '每 100 万 tokens',
      formula: '官方价格 × 使用倍率',
      officialNote: '这里展示的是官方模型价格。OmniHub 会按所选套餐的使用倍率计算实际扣费。',
      model: '模型',
      unlimited: '不限时长',
      perDay: '天',
      cards: [
        { k: '预付费余额', v: '余额到 $0.00 时停止请求。' },
        { k: '价格倍率', v: '按「供应商成本 × 你的倍率」向用户计费。' },
        { k: '兑换码', v: '批量生成充值码，用户在门户里兑换。' },
      ],
    },
    observability: {
      title: '在控制台查看流量和账号健康。',
      body: 'OmniHub 暴露 Prometheus 指标，并附带可导入的 Grafana 看板。熔断器打开或恢复时，它会通知你配置的 webhook、飞书或钉钉渠道。',
      metrics: ['omnihub_ttfb_seconds', 'omnihub_cost_usd_total', 'omnihub_circuit_state', 'omnihub_upstream_failover_total'],
    },
    footer: { tagline: '自托管 AI 网关。', portal: '用户门户', rights: '保留所有权利。' },
  },
}

export function LandingPage() {
  const { lang } = useI18n()
  const c = lang === 'zh' ? COPY.zh : COPY.en
  const pricing = usePublicPricing()

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
              <Link to="/login?mode=signup" className="btn btn-primary h-11 px-5 text-[15px]">{c.hero.start}</Link>
              <Link to="/login" className="btn btn-secondary h-11 px-5 text-[15px]">{c.hero.signin}</Link>
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
          <PublicPricingBlock c={c.billing} pricing={pricing.data} loading={pricing.isLoading} />
        </section>

        {/* Observability */}
        <Feature id="observability" reverse title={c.observability.title} body={c.observability.body} visual={<MetricsVisual metrics={c.observability.metrics} />} />

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
        <a href="#top" className="flex min-h-10 items-center gap-2">
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
          <Link to="/login" className="btn btn-ghost h-10 px-3 text-sm">{c.nav.signin}</Link>
          <Link to="/login?mode=signup" className="btn btn-primary h-10 px-3.5 text-sm">{c.nav.start}</Link>
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
          <Link to="/portal" className="inline-flex min-h-10 items-center transition-colors hover:text-ink">{c.footer.portal}</Link>
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

/* ── Public pricing ──────────────────────────────────────────────── */

function PublicPricingBlock({
  c,
  pricing,
  loading,
}: {
  c: typeof COPY.en.billing
  pricing?: PublicPricing
  loading: boolean
}) {
  const plans = pricing?.plans ?? []
  const prices = pricing?.prices ?? []
  const [selectedPlanID, setSelectedPlanID] = useState<number | null>(null)
  const defaultPlan = useMemo(() => chooseDefaultPlan(plans), [plans])
  const selectedPlan = plans.find((plan) => plan.id === selectedPlanID) ?? defaultPlan

  return (
    <div className="mt-14 rounded-2xl border border-line bg-surface-2/55 p-4 sm:p-5">
      <div className="grid gap-8 lg:grid-cols-[0.94fr_1.06fr] lg:items-start">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <span className="badge badge-brand font-mono text-[11px]">{c.formula}</span>
            {selectedPlan ? <span className="badge badge-neutral text-[11px]">{c.ratioLabel} {formatRatio(selectedPlan.price_ratio)}</span> : null}
          </div>
          <h3 className="mt-4 text-2xl font-semibold tracking-tight">{c.pricingTitle}</h3>
          <p className="mt-3 max-w-xl text-sm leading-relaxed text-muted">{c.pricingBody}</p>

          {loading ? (
            <div className="mt-6 grid gap-3">
              {[0, 1, 2].map((i) => <PricingSkeleton key={i} />)}
            </div>
          ) : plans.length > 0 ? (
            <div className="mt-6 grid gap-3">
              {plans.map((plan) => (
                <PlanPriceCard
                  key={plan.id}
                  c={c}
                  plan={plan}
                  selected={selectedPlan?.id === plan.id}
                  onSelect={() => setSelectedPlanID(plan.id)}
                />
              ))}
            </div>
          ) : (
            <div className="mt-6 rounded-xl border border-line bg-surface px-4 py-3 text-sm text-muted">{c.planFallback}</div>
          )}
        </div>

        <div className="card overflow-hidden p-0">
          <div className="border-b border-line px-4 py-3 sm:px-5">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <div className="text-sm font-semibold">{c.officialLabel}</div>
                <p className="mt-1 text-xs text-muted">{c.officialNote}</p>
              </div>
              <span className="badge badge-neutral font-mono text-[11px]">{c.perMillion}</span>
            </div>
          </div>
          {loading ? (
            <div className="space-y-3 p-4">
              {[0, 1, 2, 3].map((i) => <div key={i} className="h-10 rounded-lg bg-surface-2" />)}
            </div>
          ) : prices.length > 0 ? (
            <OfficialPriceTable c={c} prices={prices} plan={selectedPlan} />
          ) : (
            <div className="p-5 text-sm text-muted">{c.priceFallback}</div>
          )}
        </div>
      </div>
    </div>
  )
}

function PlanPriceCard({
  c,
  plan,
  selected,
  onSelect,
}: {
  c: typeof COPY.en.billing
  plan: Plan
  selected: boolean
  onSelect: () => void
}) {
  const savingPct = plan.price_ratio > 0 && plan.price_ratio < 1 ? (1 - plan.price_ratio) * 100 : 0
  return (
    <button
      type="button"
      onClick={onSelect}
      aria-pressed={selected}
      className={`w-full rounded-xl border bg-surface p-4 text-left transition-all duration-150 hover:border-line-strong hover:bg-bg ${
        selected ? 'border-line-strong shadow-sm ring-2 ring-[var(--ring)]' : 'border-line'
      }`}
    >
      <div className="flex items-start justify-between gap-4">
        <div>
          <div className="text-base font-semibold">{plan.name}</div>
          <p className="mt-1 line-clamp-2 text-xs leading-relaxed text-muted">{plan.description}</p>
        </div>
        <div className="text-right">
          <div className="font-mono text-lg font-semibold">{formatCNY(plan.price_usd)}</div>
          <div className="mt-1 font-mono text-[11px] text-muted">{plan.valid_days ? `${plan.valid_days} ${c.days}` : c.unlimited}</div>
        </div>
      </div>
      <div className="mt-4 grid gap-2 sm:grid-cols-3">
        <PlanMetric label={c.planPrice} value={formatCNY(plan.price_usd)} />
        <PlanMetric label={c.creditValue} value={formatUSD(plan.included_credit_usd)} />
        <PlanMetric
          label={savingPct > 0 ? c.modelSaving : c.noModelSaving}
          value={savingPct > 0 ? `${c.save} ${Math.round(savingPct)}%` : `× ${formatRatio(plan.price_ratio)}`}
        />
      </div>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <span className="badge badge-neutral text-[11px]">{plan.allow_payg_overage ? c.overage : c.noOverage}</span>
        {plan.rpm_limit ? <span className="badge badge-neutral font-mono text-[11px]">{plan.rpm_limit} RPM</span> : null}
        {plan.daily_usd_limit ? <span className="badge badge-neutral font-mono text-[11px]">{formatUSD(plan.daily_usd_limit)} / {c.perDay}</span> : null}
      </div>
    </button>
  )
}

function PlanMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg bg-surface-2 px-3 py-2">
      <div className="text-[11px] text-muted">{label}</div>
      <div className="mt-0.5 font-mono text-sm font-semibold text-ink">{value}</div>
    </div>
  )
}

function OfficialPriceTable({ c, prices, plan }: { c: typeof COPY.en.billing; prices: PublicModelPrice[]; plan?: Plan }) {
  const ratio = plan?.price_ratio ?? 1
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[620px] text-left text-sm">
        <thead>
          <tr className="border-b border-line bg-surface-2/70 text-xs text-muted">
            <th className="px-4 py-3 font-medium sm:px-5">{c.model}</th>
            <th className="px-4 py-3 font-medium">{c.officialLabel}</th>
            <th className="px-4 py-3 font-medium">{c.effectiveLabel}</th>
          </tr>
        </thead>
        <tbody>
          {prices.map((price) => {
            const officialInput = price.input_cost_per_token * PER_MILLION_TOKENS
            const officialOutput = price.output_cost_per_token * PER_MILLION_TOKENS
            return (
              <tr key={price.model} className="border-b border-line last:border-0">
                <td className="px-4 py-3 sm:px-5">
                  <div className="font-mono text-[13px] text-ink">{price.model}</div>
                  <div className="mt-1 font-mono text-[11px] text-muted">{price.source}</div>
                </td>
                <td className="px-4 py-3 font-mono text-[12px] text-muted">
                  <div>{c.input}: <span className="text-ink">{formatUSD(officialInput)}</span></div>
                  <div className="mt-1">{c.output}: <span className="text-ink">{formatUSD(officialOutput)}</span></div>
                </td>
                <td className="px-4 py-3 font-mono text-[12px] text-muted">
                  <div>{c.input}: <span className="text-brand">{formatUSD(officialInput * ratio)}</span></div>
                  <div className="mt-1">{c.output}: <span className="text-brand">{formatUSD(officialOutput * ratio)}</span></div>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

function PricingSkeleton() {
  return (
    <div className="rounded-xl border border-line bg-surface p-4">
      <div className="h-4 w-32 rounded bg-surface-2" />
      <div className="mt-3 h-3 w-full rounded bg-surface-2" />
      <div className="mt-4 grid gap-2 sm:grid-cols-3">
        <div className="h-12 rounded-lg bg-surface-2" />
        <div className="h-12 rounded-lg bg-surface-2" />
        <div className="h-12 rounded-lg bg-surface-2" />
      </div>
    </div>
  )
}

function chooseDefaultPlan(plans: Plan[]) {
  if (plans.length === 0) return undefined
  const paid = plans.filter((plan) => plan.price_usd > 0)
  if (paid.length > 0) return paid[Math.min(1, paid.length - 1)]
  return plans[0]
}

function formatUSD(value: number) {
  const digits = Math.abs(value) > 0 && Math.abs(value) < 1 ? 4 : 2
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: Math.abs(value) >= 100 ? 0 : 2,
    maximumFractionDigits: digits,
  }).format(value)
}

function formatCNY(value: number) {
  return new Intl.NumberFormat('zh-CN', {
    style: 'currency',
    currency: 'CNY',
    minimumFractionDigits: Number.isInteger(value) ? 0 : 2,
    maximumFractionDigits: 2,
  }).format(value)
}

function formatRatio(value: number) {
  return value.toFixed(2)
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
    { n: 'claude', s: 'closed' as const, note: 'primary route' },
    { n: 'bedrock', s: 'closed' as const, note: 'fallback ready' },
    { n: 'openai', s: 'open' as const, note: 'skipped' },
  ]
  return (
    <div className="card overflow-hidden p-0">
      <div className="border-b border-line bg-surface-2/70 px-5 py-3">
        <div className="flex items-center justify-between gap-4">
          <span className="font-mono text-xs text-muted">routing decision</span>
          <span className="font-mono text-[11px] text-muted">health-aware failover</span>
        </div>
      </div>

      <div className="p-5">
        <div className="grid gap-4">
          <div className="grid gap-3 sm:grid-cols-[1fr_auto_1.15fr] sm:items-center">
            <FlowNode eyebrow="source" title="client" body="POST /v1/messages" />
            <FlowArrow />
            <FlowNode eyebrow="gateway" title="OmniHub" body="resolve · meter · guard" accent />
          </div>

          <FlowDownArrow />

          <div className="rounded-2xl border border-line bg-surface p-3">
            <div className="mb-2 flex items-center justify-between gap-3 px-1">
              <span className="font-mono text-[10px] uppercase tracking-[0.16em] text-muted">route pool</span>
              <span className="font-mono text-[11px] text-muted">open circuits are skipped</span>
            </div>
            <div className="grid gap-2">
              {providers.map((p) => (
                <ProviderLane key={p.n} name={p.n} state={p.s} note={p.note} />
              ))}
            </div>
          </div>
        </div>

        <div className="mt-5 grid gap-2 border-t border-line pt-4 sm:grid-cols-3">
          <FlowCaption label="1" text="check account state" />
          <FlowCaption label="2" text="skip open circuits" />
          <FlowCaption label="3" text="send to next healthy route" />
        </div>
      </div>
    </div>
  )
}

function FlowNode({ eyebrow, title, body, accent }: { eyebrow: string; title: string; body: string; accent?: boolean }) {
  return (
    <div className={`rounded-2xl border px-4 py-3 ${accent ? 'border-line-strong bg-brand-subtle' : 'border-line bg-surface'}`}>
      <div className="font-mono text-[10px] uppercase tracking-[0.16em] text-muted">{eyebrow}</div>
      <div className={`mt-1 text-lg font-semibold tracking-tight ${accent ? 'text-brand' : 'text-ink'}`}>{title}</div>
      <div className="mt-1 font-mono text-[11px] text-muted">{body}</div>
    </div>
  )
}

function FlowArrow() {
  return (
    <div className="flex items-center justify-center sm:min-w-8" aria-hidden>
      <div className="hidden h-px w-full bg-line-strong sm:block" />
      <span className="-ml-1 hidden text-brand sm:block">→</span>
      <span className="font-mono text-xs text-brand sm:hidden">↓</span>
    </div>
  )
}

function FlowDownArrow() {
  return (
    <div className="grid sm:grid-cols-[1fr_auto_1.15fr]" aria-hidden>
      <div className="flex h-10 items-center justify-center sm:col-start-3">
        <div className="flex h-full flex-col items-center">
          <div className="min-h-0 flex-1 border-l border-line-strong" />
          <span className="-mt-1 font-mono text-xs text-brand">↓</span>
        </div>
      </div>
    </div>
  )
}

function ProviderLane({ name, state, note }: { name: string; state: 'closed' | 'open'; note: string }) {
  const open = state === 'open'
  return (
    <div className={`rounded-2xl border px-3.5 py-3 ${open ? 'border-danger/30 bg-danger-bg/60 opacity-80' : 'border-line bg-surface-2'}`}>
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <span className={`h-2.5 w-2.5 rounded-full ${open ? 'bg-danger' : 'bg-success'}`} />
          <span className="font-mono text-sm text-ink">{name}</span>
        </div>
        <Pill tone={open ? 'danger' : 'success'}>{state}</Pill>
      </div>
      <div className="mt-2 flex items-center justify-between gap-3 font-mono text-[11px] text-muted">
        <span>{note}</span>
        <span className={open ? 'text-danger' : 'text-success'}>{open ? 'failover →' : 'selected'}</span>
      </div>
    </div>
  )
}

function FlowCaption({ label, text }: { label: string; text: string }) {
  return (
    <div className="flex items-center gap-2 rounded-xl border border-line bg-surface-2 px-3 py-2">
      <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-brand-subtle font-mono text-[11px] text-brand">{label}</span>
      <span className="font-mono text-[11px] text-muted">{text}</span>
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
