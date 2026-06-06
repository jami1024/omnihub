import { useState } from 'react'
import { PortalLayout } from '../../components/PortalLayout'
import { Td, Th } from '../../components/Table'
import { ApiError } from '../../lib/portalApi'
import { usePortalRequests } from '../../lib/portalData'
import { useI18n } from '../../lib/i18n'

const WINDOWS = [7, 14, 30, 90]

export function PortalRequestsPage() {
  const { t, lang } = useI18n()
  const [days, setDays] = useState(7)
  const [page, setPage] = useState(1)
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
                  return (
                    <tr key={i} className="transition-colors hover:bg-surface-2">
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
                      </Td>
                      <Td className="text-right tabular-nums">${r.cost_usd.toFixed(4)}</Td>
                      <Td className="text-right tabular-nums text-muted">
                        {r.duration_ms != null ? `${r.duration_ms}ms` : '—'}
                      </Td>
                    </tr>
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
