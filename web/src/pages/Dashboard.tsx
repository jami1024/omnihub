import { useEffect, useState } from 'react'
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { Layout } from '../components/Layout'
import { ErrorNotice, LoadingTable, MetricStrip, PageHeader } from '../components/PageChrome'
import { Td, Th } from '../components/Table'
import { ApiError } from '../lib/api'
import { useI18n } from '../lib/i18n'
import { useUsage, type ModelUsage } from '../lib/usage'

const WINDOWS = [7, 14, 30, 90]

// recharts colors are SVG attributes, not CSS, so they can't read the
// theme tokens directly. Resolve them from the live CSS variables and
// re-resolve when the theme class flips.
function useChartColors() {
  const read = () => {
    const cs = getComputedStyle(document.documentElement)
    return {
      brand: cs.getPropertyValue('--brand').trim() || '#6366f1',
      grid: cs.getPropertyValue('--border').trim() || '#e4e4e7',
      axis: cs.getPropertyValue('--muted').trim() || '#a1a1aa',
    }
  }
  const [colors, setColors] = useState(read)
  useEffect(() => {
    const update = () => setColors(read())
    const obs = new MutationObserver(update)
    obs.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    const mq = matchMedia('(prefers-color-scheme: dark)')
    mq.addEventListener('change', update)
    return () => {
      obs.disconnect()
      mq.removeEventListener('change', update)
    }
  }, [])
  return colors
}

export function DashboardPage() {
  const { t } = useI18n()
  const [days, setDays] = useState(7)
  const { data, isLoading, error } = useUsage(days)
  const cc = useChartColors()

  return (
    <Layout>
      <main className="mx-auto w-full max-w-7xl px-6 py-8">
        <PageHeader
          eyebrow={t('dashboard.eyebrow')}
          context={t('dashboard.context')}
          title={t('dashboard.title')}
          description={t('dashboard.description')}
          action={
            <div className="flex gap-1 rounded-lg border border-line bg-surface p-0.5 text-sm">
            {WINDOWS.map((w) => (
              <button
                key={w}
                onClick={() => setDays(w)}
                className={`rounded px-2.5 py-1 ${
                  days === w
                    ? 'bg-brand text-brand-ink'
                    : 'text-muted hover:bg-surface-2 hover:text-ink'
                }`}
              >
                {t('dashboard.daysShort', { n: w })}
              </button>
            ))}
          </div>
          }
        />

        {isLoading && <LoadingTable />}
        {error && (
          <ErrorNotice>
            {error instanceof ApiError ? error.message : t('dashboard.loadError')}
          </ErrorNotice>
        )}

        {data && (
          <div className="space-y-6">
            <MetricStrip
              metrics={[
                { label: t('dashboard.requests'), value: fmtInt(data.summary.requests) },
                { label: t('dashboard.spend'), value: fmtUSD(data.summary.cost_usd) },
                {
                  label: t('dashboard.tokensInOut'),
                  value: `${fmtTokens(data.summary.input_tokens)} / ${fmtTokens(data.summary.output_tokens)}`,
                },
                {
                  label: t('dashboard.errors'),
                  value: fmtInt(data.summary.errors),
                  tone: data.summary.errors > 0 ? 'danger' : undefined,
                },
              ]}
            />

            <Card title={t('dashboard.dailySpend')}>
              <ResponsiveContainer width="100%" height={240}>
                <AreaChart data={data.daily} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
                  <defs>
                    <linearGradient id="spend" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor={cc.brand} stopOpacity={0.4} />
                      <stop offset="100%" stopColor={cc.brand} stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <CartesianGrid strokeDasharray="3 3" stroke={cc.grid} vertical={false} />
                  <XAxis dataKey="day" tickFormatter={fmtDay} fontSize={11} stroke={cc.axis} />
                  <YAxis tickFormatter={(v) => `$${v}`} fontSize={11} stroke={cc.axis} width={48} />
                  <Tooltip
                    formatter={(v: number) => [fmtUSD(v), t('dashboard.spend')]}
                    labelFormatter={(l) => fmtDay(l as string)}
                  />
                  <Area type="monotone" dataKey="cost_usd" stroke={cc.brand} fill="url(#spend)" strokeWidth={2} />
                </AreaChart>
              </ResponsiveContainer>
            </Card>

            <Card title={t('dashboard.dailyRequests')}>
              <ResponsiveContainer width="100%" height={200}>
                <BarChart data={data.daily} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
                  <CartesianGrid strokeDasharray="3 3" stroke={cc.grid} vertical={false} />
                  <XAxis dataKey="day" tickFormatter={fmtDay} fontSize={11} stroke={cc.axis} />
                  <YAxis fontSize={11} stroke={cc.axis} width={48} allowDecimals={false} />
                  <Tooltip
                    formatter={(v: number) => [fmtInt(v), t('dashboard.requests')]}
                    labelFormatter={(l) => fmtDay(l as string)}
                  />
                  <Bar dataKey="requests" fill={cc.brand} radius={[2, 2, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            </Card>

            <Card title={t('dashboard.spendByModel')}>
              {data.by_model.length === 0 ? (
                <p className="py-8 text-center text-sm text-muted">{t('dashboard.noTraffic')}</p>
              ) : (
                <ModelBreakdown models={data.by_model} />
              )}
            </Card>
          </div>
        )}
      </main>
    </Layout>
  )
}

function ModelBreakdown({ models }: { models: ModelUsage[] }) {
  const { t } = useI18n()
  const cc = useChartColors()
  // Top 8 by cost for the chart; the table below lists them all.
  const top = models.slice(0, 8)
  return (
    <div className="space-y-4">
      <ResponsiveContainer width="100%" height={Math.max(120, top.length * 34)}>
        <BarChart data={top} layout="vertical" margin={{ top: 0, right: 16, left: 0, bottom: 0 }}>
          <XAxis type="number" tickFormatter={(v) => `$${v}`} fontSize={11} stroke={cc.axis} />
          <YAxis type="category" dataKey="model" width={150} fontSize={11} stroke={cc.axis} />
          <Tooltip formatter={(v: number) => [fmtUSD(v), t('dashboard.spend')]} />
          <Bar dataKey="cost_usd" radius={[0, 2, 2, 0]}>
            {top.map((_, i) => (
              <Cell key={i} fill={cc.brand} />
            ))}
          </Bar>
        </BarChart>
      </ResponsiveContainer>

      <div className="overflow-x-auto rounded-xl border border-line bg-surface">
        <table className="w-full text-left text-sm">
          <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
            <tr>
              <Th>{t('dashboard.model')}</Th>
              <Th className="text-right">{t('dashboard.requests')}</Th>
              <Th className="text-right">{t('dashboard.spend')}</Th>
              <Th className="text-right">{t('dashboard.in')}</Th>
              <Th className="text-right">{t('dashboard.out')}</Th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line">
            {models.map((m) => (
              <tr key={m.model}>
                <Td className="font-mono text-xs">{m.model}</Td>
                <Td className="text-right tabular-nums">{fmtInt(m.requests)}</Td>
                <Td className="text-right tabular-nums">{fmtUSD(m.cost_usd)}</Td>
                <Td className="text-right tabular-nums">{fmtTokens(m.input_tokens)}</Td>
                <Td className="text-right tabular-nums">{fmtTokens(m.output_tokens)}</Td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function Card({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="card p-4">
      <h3 className="mb-3 font-mono text-xs font-medium uppercase tracking-[0.16em] text-muted">{title}</h3>
      {children}
    </section>
  )
}

function fmtUSD(n: number): string {
  return `$${n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
}
function fmtInt(n: number): string {
  return n.toLocaleString()
}
function fmtTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`
  return String(n)
}
function fmtDay(iso: string): string {
  // iso is a UTC-midnight timestamp; show MM-DD.
  return iso.slice(5, 10)
}
