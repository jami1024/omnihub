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

export function HealthPage() {
  const { data: circuit, isLoading, error } = useCircuit()
  const { data: events } = useCircuitEvents(50)
  const reset = useResetBreaker()
  const accounts = circuit?.accounts ?? []
  const closedCount = accounts.filter((s) => s.state === 'closed').length
  const openCount = accounts.filter((s) => s.state === 'open').length
  const halfOpenCount = accounts.filter((s) => s.state === 'half-open').length

  function handleReset(s: CircuitStatus) {
    if (!confirm(`Force account "${s.account_name}" back to closed?`)) return
    reset.mutate(s.account_id)
  }

  return (
    <Layout>
      <main className="mx-auto w-full max-w-7xl px-6 py-8">
        <PageHeader
          eyebrow="HEALTH"
          context="Circuit breaker state"
          title="Circuit breakers"
          description="Watch per-account breaker state and recent transitions. The live table refreshes every 10 seconds."
        />

        <MetricStrip
          metrics={[
            { label: 'Accounts', value: accounts.length },
            { label: 'Closed', value: closedCount },
            { label: 'Half-open', value: halfOpenCount, tone: halfOpenCount > 0 ? 'warning' : undefined },
            { label: 'Open', value: openCount, tone: openCount > 0 ? 'danger' : undefined },
          ]}
        />

        <div className="mt-6" />

        {isLoading && <LoadingTable />}
        {error && (
          <ErrorNotice>
            {error instanceof ApiError ? error.message : 'Could not load circuit state.'}
          </ErrorNotice>
        )}

        {circuit && !circuit.available && (
          <EmptyState
            eyebrow="No live breaker state"
            title="Configure an upstream account before monitoring health."
            description="Circuit breaker state appears once the gateway has at least one routable account to watch."
          />
        )}

        {circuit?.available && (
          <section className="space-y-3">
            <div className="overflow-x-auto rounded-xl border border-line bg-surface">
              <table className="w-full text-left text-sm">
                <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
                  <tr>
                    <Th>Account</Th>
                    <Th>State</Th>
                    <Th className="text-right">Failures</Th>
                    <Th>Open until</Th>
                    <Th>Last failure</Th>
                    <Th className="text-right">Actions</Th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-line">
                  {circuit.accounts.map((s) => (
                    <tr key={s.account_id} className="transition-colors hover:bg-surface-2">
                      <Td className="font-medium">
                        {s.account_name}
                        {!s.enabled && <span className="ml-2 text-xs text-muted">(disabled)</span>}
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
                          title={s.state === 'closed' ? 'Already closed' : 'Force closed'}
                        >
                          Reset
                        </button>
                      </Td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {reset.error && (
              <ErrorNotice>
                {reset.error instanceof ApiError ? reset.error.message : 'Reset failed.'}
              </ErrorNotice>
            )}
          </section>
        )}

        <section className="mt-8">
          <h3 className="mb-3 font-mono text-xs font-medium uppercase tracking-[0.16em] text-muted">
            Recent transitions
          </h3>
          {events && events.length === 0 && (
            <p className="rounded-xl border border-line bg-surface px-4 py-3 text-sm text-muted">
              No transitions recorded yet.
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
  const styles: Record<string, string> = {
    closed: 'bg-success-bg text-success',
    open: 'bg-danger-bg text-danger',
    'half-open': 'bg-warning-bg text-warning',
  }
  const cls = styles[state] ?? 'surface-2 text-muted'
  return (
    <span
      className={`inline-flex items-center rounded-full font-medium ${cls} ${
        small ? 'px-1.5 py-0.5 text-[11px]' : 'px-2 py-0.5 text-xs'
      }`}
    >
      {state}
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
