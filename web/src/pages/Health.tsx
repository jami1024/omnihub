import { Layout } from '../components/Layout'
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

  function handleReset(s: CircuitStatus) {
    if (!confirm(`Force account "${s.account_name}" back to closed?`)) return
    reset.mutate(s.account_id)
  }

  return (
    <Layout>
      <main className="mx-auto max-w-6xl px-6 py-10">
        <div className="mb-6">
          <h2 className="text-xl font-semibold">Circuit breakers</h2>
          <p className="text-sm text-muted">
            Live per-account breaker state and the recent transition feed. Refreshes every 10s.
          </p>
        </div>

        {isLoading && <p className="text-sm text-muted">Loading…</p>}
        {error && (
          <p className="text-sm text-danger">
            {error instanceof ApiError ? error.message : 'Could not load circuit state.'}
          </p>
        )}

        {circuit && !circuit.available && (
          <div className="rounded-xl border border-dashed border-line-strong p-10 text-center text-sm text-muted">
            The gateway isn't running (no accounts configured), so there's no live breaker state.
          </div>
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
                    <tr key={s.account_id} className="hover:bg-surface-2">
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
                          className="text-muted hover:text-ink hover:underline disabled:opacity-40"
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
              <p className="text-sm text-danger">
                {reset.error instanceof ApiError ? reset.error.message : 'Reset failed.'}
              </p>
            )}
          </section>
        )}

        <section className="mt-8">
          <h3 className="mb-3 text-sm font-medium text-muted">
            Recent transitions
          </h3>
          {events && events.length === 0 && (
            <p className="text-sm text-muted">No transitions recorded yet.</p>
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
