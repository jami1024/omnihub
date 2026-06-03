import { useState } from 'react'
import { PortalLayout } from '../../components/PortalLayout'
import { Td, Th } from '../../components/Table'
import { ApiError } from '../../lib/portalApi'
import { usePortalUsage } from '../../lib/portalData'
import { useI18n } from '../../lib/i18n'

const WINDOWS = [7, 14, 30, 90]

export function PortalOverviewPage() {
  const { t } = useI18n()
  const [days, setDays] = useState(7)
  const { data, isLoading, error } = usePortalUsage(days)

  return (
    <PortalLayout>
      <main className="mx-auto max-w-5xl px-6 py-8">
        <div className="mb-6 flex items-center justify-between">
          <div>
            <h2 className="text-xl font-semibold">{t('portalOverview.title')}</h2>
            <p className="text-sm text-muted">{t('portalOverview.subtitle')}</p>
          </div>
          <div className="flex gap-1 rounded-full border border-line p-0.5 text-sm">
            {WINDOWS.map((w) => (
              <button
                key={w}
                onClick={() => setDays(w)}
                className="rounded-full px-2.5 py-1 transition-colors"
                style={{
                  color: days === w ? 'var(--brand-ink)' : 'var(--muted)',
                  background: days === w ? 'var(--brand)' : 'transparent',
                }}
              >
                {w}d
              </button>
            ))}
          </div>
        </div>

        {isLoading && <p className="text-sm text-muted">{t('common.loading')}</p>}
        {error && (
          <p className="text-sm text-danger">
            {error instanceof ApiError ? error.message : t('portalOverview.loadError')}
          </p>
        )}

        {data && (
          <div className="space-y-6">
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
              <Stat label={t('portalOverview.requests')} value={fmtInt(data.summary.requests)} />
              <Stat label={t('portalOverview.spend')} value={fmtUSD(data.summary.cost_usd)} />
              <Stat
                label={t('portalOverview.tokensInOut')}
                value={`${fmtTokens(data.summary.input_tokens)} / ${fmtTokens(data.summary.output_tokens)}`}
              />
              <Stat
                label={t('portalOverview.errors')}
                value={fmtInt(data.summary.errors)}
                accent={data.summary.errors > 0 ? 'text-danger' : undefined}
              />
            </div>

            <section className="card p-4">
              <h3 className="mb-3 text-sm font-medium text-muted">{t('portalOverview.byModel')}</h3>
              {data.by_model.length === 0 ? (
                <p className="py-6 text-center text-sm text-muted">
                  {t('portalOverview.noTraffic')}
                </p>
              ) : (
                <div className="overflow-x-auto rounded-xl border border-line bg-surface">
                  <table className="w-full text-left text-sm">
                    <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
                      <tr>
                        <Th>{t('portalOverview.model')}</Th>
                        <Th className="text-right">{t('portalOverview.requests')}</Th>
                        <Th className="text-right">{t('portalOverview.spend')}</Th>
                        <Th className="text-right">{t('portalOverview.in')}</Th>
                        <Th className="text-right">{t('portalOverview.out')}</Th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-line">
                      {data.by_model.map((m) => (
                        <tr key={m.model} className="hover:bg-surface-2">
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
              )}
            </section>
          </div>
        )}
      </main>
    </PortalLayout>
  )
}

function Stat({ label, value, accent }: { label: string; value: string; accent?: string }) {
  return (
    <div className="stat">
      <div className="text-xs font-medium uppercase tracking-wide text-muted">{label}</div>
      <div className={`mt-1.5 text-2xl font-semibold tabular-nums ${accent ?? ''}`}>{value}</div>
    </div>
  )
}

function fmtUSD(n: number) {
  return `$${n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 4 })}`
}
function fmtInt(n: number) {
  return n.toLocaleString()
}
function fmtTokens(n: number) {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`
  return String(n)
}
