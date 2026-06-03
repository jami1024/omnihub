import { Layout } from '../components/Layout'
import { EmptyState, ErrorNotice, LoadingTable, MetricStrip, PageHeader } from '../components/PageChrome'
import { Td, Th } from '../components/Table'
import { ApiError } from '../lib/api'
import {
  useCircuit,
  useCircuitEvents,
  useResetBreaker,
  type CircuitStatus,
  type HealthEvent,
} from '../lib/circuit'
import { useI18n } from '../lib/i18n'

export function HealthPage() {
  const { t } = useI18n()
  const { data: circuit, isLoading, error } = useCircuit()
  const { data: events } = useCircuitEvents(50)
  const reset = useResetBreaker()
  const accounts = circuit?.accounts ?? []
  const closedCount = accounts.filter((s) => s.state === 'closed').length
  const openCount = accounts.filter((s) => s.state === 'open').length
  const halfOpenCount = accounts.filter((s) => s.state === 'half-open').length

  function handleReset(s: CircuitStatus) {
    if (!confirm(t('health.forceClosedConfirm', { name: s.account_name }))) return
    reset.mutate(s.account_id)
  }

  return (
    <Layout>
      <main className="mx-auto w-full max-w-7xl px-6 py-8">
        <PageHeader
          eyebrow={t('health.eyebrow')}
          context={t('health.context')}
          title={t('health.title')}
          description={t('health.description')}
        />

        <MetricStrip
          metrics={[
            { label: t('health.metricAccounts'), value: accounts.length },
            { label: t('health.metricClosed'), value: closedCount },
            { label: t('health.metricHalfOpen'), value: halfOpenCount, tone: halfOpenCount > 0 ? 'warning' : undefined },
            { label: t('health.metricOpen'), value: openCount, tone: openCount > 0 ? 'danger' : undefined },
          ]}
        />

        <div className="mt-6" />

        {isLoading && <LoadingTable />}
        {error && (
          <ErrorNotice>
            {error instanceof ApiError ? error.message : t('health.loadError')}
          </ErrorNotice>
        )}

        {circuit && !circuit.available && (
          <EmptyState
            eyebrow={t('health.emptyEyebrow')}
            title={t('health.emptyTitle')}
            description={t('health.emptyDescription')}
          />
        )}

        {circuit?.available && (
          <section className="space-y-3">
            <div className="overflow-x-auto rounded-xl border border-line bg-surface">
              <table className="w-full text-left text-sm">
                <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
                  <tr>
                    <Th>{t('health.colAccount')}</Th>
                    <Th>{t('health.colState')}</Th>
                    <Th className="text-right">{t('health.colFailures')}</Th>
                    <Th>{t('health.colOpenUntil')}</Th>
                    <Th>{t('health.colLastFailure')}</Th>
                    <Th className="text-right">{t('common.actions')}</Th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-line">
                  {circuit.accounts.map((s) => (
                    <tr key={s.account_id} className="transition-colors hover:bg-surface-2">
                      <Td className="font-medium">
                        {s.account_name}
                        {!s.enabled && <span className="ml-2 text-xs text-muted">{t('health.disabledSuffix')}</span>}
                      </Td>
                      <Td>
                        <StateBadge state={s.state} />
                      </Td>
                      <Td className="text-right tabular-nums">{s.failure_count}</Td>
                      <Td className="text-muted">{fmtTime(s.open_until)}</Td>
                      <Td className="text-muted">{fmtTime(s.last_failure)}</Td>
                      <Td className="text-right">
                        <button
                          onClick={() => handleReset(s)}
                          disabled={reset.isPending || s.state === 'closed'}
                          className="text-muted underline-offset-4 hover:text-ink hover:underline disabled:opacity-40"
                          title={s.state === 'closed' ? t('health.alreadyClosed') : t('health.forceClosed')}
                        >
                          {t('health.reset')}
                        </button>
                      </Td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {reset.error && (
              <ErrorNotice>
                {reset.error instanceof ApiError ? reset.error.message : t('health.resetFailed')}
              </ErrorNotice>
            )}
          </section>
        )}

        <section className="mt-8">
          <h3 className="mb-3 font-mono text-xs font-medium uppercase tracking-[0.16em] text-muted">
            {t('health.recentTransitions')}
          </h3>
          {events && events.length === 0 && (
            <p className="rounded-xl border border-line bg-surface px-4 py-3 text-sm text-muted">
              {t('health.noTransitions')}
            </p>
          )}
          {events && events.length > 0 && (
            <ol className="space-y-1.5">
              {events.map((ev, i) => (
                <EventRow key={i} ev={ev} />
              ))}
            </ol>
          )}
        </section>
      </main>
    </Layout>
  )
}

function EventRow({ ev }: { ev: HealthEvent }) {
  return (
    <li className="flex items-center gap-3 rounded-md border border-line px-3 py-2 text-sm dark:border-line">
      <span className="shrink-0 font-mono text-xs text-muted">{fmtTime(ev.created_at)}</span>
      <span className="font-medium">{ev.account_name}</span>
      <span className="flex items-center gap-1 text-muted">
        <StateBadge state={ev.from_state} small />
        <span aria-hidden>→</span>
        <StateBadge state={ev.to_state} small />
      </span>
      {ev.reason && <span className="truncate text-muted">{ev.reason}</span>}
    </li>
  )
}

function StateBadge({ state, small }: { state: string; small?: boolean }) {
  const { t } = useI18n()
  const styles: Record<string, string> = {
    closed: 'bg-success-bg text-success',
    open: 'bg-danger-bg text-danger',
    'half-open': 'bg-warning-bg text-warning',
  }
  const labels: Record<string, string> = {
    closed: t('health.stateClosed'),
    open: t('health.stateOpen'),
    'half-open': t('health.stateHalfOpen'),
  }
  const cls = styles[state] ?? 'surface-2 text-muted'
  return (
    <span
      className={`inline-flex items-center rounded-full font-medium ${cls} ${
        small ? 'px-1.5 py-0.5 text-[11px]' : 'px-2 py-0.5 text-xs'
      }`}
    >
      {labels[state] ?? state}
    </span>
  )
}

function fmtTime(iso: string | null): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return d.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}
