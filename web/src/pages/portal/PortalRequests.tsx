import { Fragment, useState } from 'react'
import { PortalLayout } from '../../components/PortalLayout'
import { Td, Th } from '../../components/Table'
import { ApiError } from '../../lib/portalApi'
import { usePortalRequests, type PortalRequestRow } from '../../lib/portalData'
import { useI18n } from '../../lib/i18n'

const WINDOWS = [7, 14, 30, 90]

export function PortalRequestsPage() {
  const { t, lang } = useI18n()
  const [days, setDays] = useState(7)
  const [page, setPage] = useState(1)
  const [expanded, setExpanded] = useState<number | null>(null)
  const { data, isLoading, error } = usePortalRequests(days, page)

  const locale = lang === 'zh' ? 'zh-CN' : 'en-US'
  const pageCount = data ? Math.max(1, Math.ceil(data.total / data.page_size)) : 1

  function setWindow(w: number) {
    setDays(w)
    setPage(1)
  }

  return (
    <PortalLayout>
      <main className="mx-auto max-w-5xl px-6 py-8">
        <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h2 className="text-xl font-semibold">{t('portalRequests.title')}</h2>
            <p className="text-sm text-muted">{t('portalRequests.subtitle')}</p>
          </div>
          <div className="flex gap-1 rounded-full border border-line p-0.5 text-sm">
            {WINDOWS.map((w) => (
              <button
                key={w}
                onClick={() => setWindow(w)}
                className="min-h-10 rounded-full px-3 transition-colors"
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
            {error instanceof ApiError ? error.message : t('portalRequests.loadError')}
          </p>
        )}

        {data && data.requests.length === 0 && (
          <p className="rounded-lg border border-line bg-surface px-4 py-8 text-center text-sm text-muted">
            {t('portalRequests.empty')}
          </p>
        )}

        {data && data.requests.length > 0 && (
          <div className="overflow-x-auto rounded-xl border border-line bg-surface">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
                <tr>
                  <Th>{t('portalRequests.colTime')}</Th>
                  <Th>{t('portalRequests.colKey')}</Th>
                  <Th>{t('portalRequests.colModel')}</Th>
                  <Th>{t('portalRequests.colStatus')}</Th>
                  <Th className="text-right">{t('portalRequests.colTokens')}</Th>
                  <Th className="text-right">{t('portalRequests.colCost')}</Th>
                  <Th className="text-right">{t('portalRequests.colLatency')}</Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {data.requests.map((r, i) => {
                  const failed = r.status_code == null || r.status_code >= 400
                  const isExpanded = expanded === i
                  return (
                    <Fragment key={`${r.created_at}-${i}`}>
                      <tr className="transition-colors hover:bg-surface-2">
                        <Td className="whitespace-nowrap text-muted">
                          {new Date(r.created_at).toLocaleString(locale)}
                        </Td>
                        <Td className="font-medium">{r.key_name || '—'}</Td>
                        <Td className="text-muted">{r.model || '—'}</Td>
                        <Td>
                          <span
                            className={
                              'rounded px-1.5 py-0.5 text-xs ' +
                              (failed ? 'bg-danger/10 text-danger' : 'text-muted')
                            }
                            title={r.error || undefined}
                          >
                            {r.status_code ?? t('portalRequests.statusError')}
                          </span>
                        </Td>
                        <Td className="text-right tabular-nums">
                          {r.input_tokens} / {r.output_tokens}
                          {r.cache_read_input_tokens > 0 && (
                            <span className="ml-1 text-xs text-muted">
                              ·{' '}
                              {t('portalRequests.cacheReadInline', {
                                n: r.cache_read_input_tokens.toLocaleString(),
                              })}
                            </span>
                          )}
                        </Td>
                        <Td className="text-right tabular-nums">
                          <button
                            type="button"
                            className="min-h-10 rounded-md px-2 font-mono text-xs text-brand hover:bg-surface-2 hover:underline"
                            aria-expanded={isExpanded}
                            onClick={() => setExpanded(isExpanded ? null : i)}
                          >
                            {money(r.billed_usd ?? r.cost_usd)}
                          </button>
                        </Td>
                        <Td className="text-right tabular-nums text-muted">
                          {r.duration_ms != null ? `${r.duration_ms}ms` : '—'}
                        </Td>
                      </tr>
                      {isExpanded && (
                        <tr className="bg-surface-2/55">
                          <td colSpan={7} className="px-4 py-4">
                            <CostBreakdown row={r} />
                          </td>
                        </tr>
                      )}
                    </Fragment>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}

        {data && data.total > data.page_size && (
          <div className="mt-4 flex items-center justify-between text-sm text-muted">
            <span>{t('portalRequests.pageOf', { page: data.page, pages: pageCount, total: data.total })}</span>
            <div className="flex gap-2">
              <button
                onClick={() => setPage((p) => Math.max(1, p - 1))}
                disabled={page <= 1}
                className="btn btn-secondary min-h-10 disabled:opacity-40"
              >
                {t('portalRequests.prev')}
              </button>
              <button
                onClick={() => setPage((p) => Math.min(pageCount, p + 1))}
                disabled={page >= pageCount}
                className="btn btn-secondary min-h-10 disabled:opacity-40"
              >
                {t('portalRequests.next')}
              </button>
            </div>
          </div>
        )}
      </main>
    </PortalLayout>
  )
}

function CostBreakdown({ row }: { row: PortalRequestRow }) {
  const { t } = useI18n()
  const b = row.cost_breakdown
  if (!b) {
    return (
      <div className="rounded-lg border border-line bg-surface px-3 py-3 text-sm text-muted">
        {t('portalRequests.noCostBreakdown')}
      </div>
    )
  }
  const billed = row.billed_usd ?? row.cost_usd
  const billingRatio = row.cost_usd > 0 ? billed / row.cost_usd : null
  const rows = [
    {
      label: t('portalRequests.costInput'),
      tokens: row.input_tokens,
      amount: b.input,
    },
    {
      label: t('portalRequests.costOutput'),
      tokens: row.output_tokens,
      amount: b.output,
    },
    {
      label: t('portalRequests.costCacheWrite'),
      tokens: row.cache_creation_input_tokens,
      amount: (b.cache_creation_5m ?? 0) + (b.cache_creation_1h ?? 0),
    },
    {
      label: t('portalRequests.costCacheRead'),
      tokens: row.cache_read_input_tokens,
      amount: b.cache_read ?? 0,
    },
  ]

  return (
    <div className="rounded-lg border border-line bg-surface p-3">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <div>
          <p className="text-sm font-medium">{t('portalRequests.costBreakdown')}</p>
          <p className="text-xs text-muted">{t('portalRequests.costBreakdownHint')}</p>
        </div>
        <div className="text-right text-xs text-muted">
          <div>{t('portalRequests.upstreamCost')}: <span className="font-mono text-ink">{money(row.cost_usd)}</span></div>
          <div>{t('portalRequests.billedCost')}: <span className="font-mono text-ink">{money(billed)}</span></div>
        </div>
      </div>
      <div className="grid gap-2">
        {rows.map((item) => (
          <div key={item.label} className="grid gap-2 rounded-md bg-surface-2 px-3 py-2 text-xs sm:grid-cols-[8rem_1fr_auto] sm:items-center">
            <span className="font-medium text-ink">{item.label}</span>
            <span className="font-mono text-muted">
              {item.tokens.toLocaleString()} × {ratePerMTok(item.amount, item.tokens)}
            </span>
            <span className="font-mono text-ink">{money(item.amount)}</span>
          </div>
        ))}
      </div>
      <div className="mt-3 grid gap-2 text-xs text-muted sm:grid-cols-2">
        <div>
          {t('portalRequests.costMultiplier')}: <span className="font-mono text-ink">{formatRatio(b.multiplier ?? 1)}</span>
        </div>
        <div>
          {t('portalRequests.billingRatio')}: <span className="font-mono text-ink">{billingRatio == null ? '—' : formatRatio(billingRatio)}</span>
        </div>
      </div>
    </div>
  )
}

function money(n: number) {
  return `$${n.toFixed(6).replace(/0+$/, '').replace(/\.$/, '.00')}`
}

function ratePerMTok(amount: number, tokens: number) {
  if (tokens <= 0) return '$0 / MTok'
  return `${money((amount / tokens) * 1_000_000)} / MTok`
}

function formatRatio(n: number) {
  return `${Number.isFinite(n) ? n.toFixed(4).replace(/0+$/, '').replace(/\.$/, '') : '—'}×`
}
