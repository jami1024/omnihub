import { useState } from 'react'
import { PortalLayout } from '../../components/PortalLayout'
import { Td, Th } from '../../components/Table'
import { ApiError } from '../../lib/portalApi'
import { usePortalUsage } from '../../lib/portalData'

const WINDOWS = [7, 14, 30, 90]

export function PortalOverviewPage() {
  const [days, setDays] = useState(7)
  const { data, isLoading, error } = usePortalUsage(days)

  return (
    <PortalLayout>
      <main className="mx-auto max-w-5xl px-6 py-8">
        <div className="mb-6 flex items-center justify-between">
          <div>
            <h2 className="text-xl font-semibold">Your usage</h2>
            <p className="text-sm text-muted">Requests and spend across your API keys.</p>
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

        {isLoading && <p className="text-sm text-muted">Loading…</p>}
        {error && (
          <p className="text-sm text-danger">
            {error instanceof ApiError ? error.message : 'Could not load usage.'}
          </p>
        )}

        {data && (
          <div className="space-y-6">
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
              <Stat label="Requests" value={fmtInt(data.summary.requests)} />
              <Stat label="Spend" value={fmtUSD(data.summary.cost_usd)} />
              <Stat
                label="Tokens (in / out)"
                value={`${fmtTokens(data.summary.input_tokens)} / ${fmtTokens(data.summary.output_tokens)}`}
              />
              <Stat
                label="Errors"
                value={fmtInt(data.summary.errors)}
                accent={data.summary.errors > 0 ? 'text-danger' : undefined}
              />
            </div>

            <section className="card p-4">
              <h3 className="mb-3 text-sm font-medium text-muted">By model</h3>
              {data.by_model.length === 0 ? (
                <p className="py-6 text-center text-sm text-muted">
                  No traffic yet. Create a key and start sending requests.
                </p>
              ) : (
                <div className="overflow-x-auto rounded-xl border border-line bg-surface">
                  <table className="w-full text-left text-sm">
                    <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
                      <tr>
                        <Th>Model</Th>
                        <Th className="text-right">Requests</Th>
                        <Th className="text-right">Spend</Th>
                        <Th className="text-right">In</Th>
                        <Th className="text-right">Out</Th>
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
