import { useState } from 'react'
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
import { Td, Th } from '../components/Table'
import { ApiError } from '../lib/api'
import { useUsage, type ModelUsage } from '../lib/usage'

const WINDOWS = [7, 14, 30, 90]

export function DashboardPage() {
  const [days, setDays] = useState(7)
  const { data, isLoading, error } = useUsage(days)

  return (
    <Layout>
      <main className="mx-auto max-w-6xl px-6 py-10">
        <div className="mb-6 flex items-center justify-between">
          <div>
            <h2 className="text-xl font-semibold">Dashboard</h2>
            <p className="text-sm text-zinc-500">Usage and spend across the gateway.</p>
          </div>
          <div className="flex gap-1 rounded-md border border-zinc-200 p-0.5 text-sm dark:border-zinc-800">
            {WINDOWS.map((w) => (
              <button
                key={w}
                onClick={() => setDays(w)}
                className={`rounded px-2.5 py-1 ${
                  days === w
                    ? 'bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900'
                    : 'text-zinc-500 hover:bg-zinc-100 dark:hover:bg-zinc-800'
                }`}
              >
                {w}d
              </button>
            ))}
          </div>
        </div>

        {isLoading && <p className="text-sm text-zinc-500">Loading…</p>}
        {error && (
          <p className="text-sm text-red-600 dark:text-red-400">
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
                accent={data.summary.errors > 0 ? 'text-red-600 dark:text-red-400' : undefined}
              />
            </div>

            <Card title="Daily spend (USD)">
              <ResponsiveContainer width="100%" height={240}>
                <AreaChart data={data.daily} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
                  <defs>
                    <linearGradient id="spend" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor="#6366f1" stopOpacity={0.4} />
                      <stop offset="100%" stopColor="#6366f1" stopOpacity={0} />
                    </linearGradient>
                  </defs>
                  <CartesianGrid strokeDasharray="3 3" stroke="#e4e4e7" vertical={false} />
                  <XAxis dataKey="day" tickFormatter={fmtDay} fontSize={11} stroke="#a1a1aa" />
                  <YAxis tickFormatter={(v) => `$${v}`} fontSize={11} stroke="#a1a1aa" width={48} />
                  <Tooltip
                    formatter={(v: number) => [fmtUSD(v), 'Spend']}
                    labelFormatter={(l) => fmtDay(l as string)}
                  />
                  <Area type="monotone" dataKey="cost_usd" stroke="#6366f1" fill="url(#spend)" strokeWidth={2} />
                </AreaChart>
              </ResponsiveContainer>
            </Card>

            <Card title="Daily requests">
              <ResponsiveContainer width="100%" height={200}>
                <BarChart data={data.daily} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#e4e4e7" vertical={false} />
                  <XAxis dataKey="day" tickFormatter={fmtDay} fontSize={11} stroke="#a1a1aa" />
                  <YAxis fontSize={11} stroke="#a1a1aa" width={48} allowDecimals={false} />
                  <Tooltip
                    formatter={(v: number) => [fmtInt(v), 'Requests']}
                    labelFormatter={(l) => fmtDay(l as string)}
                  />
                  <Bar dataKey="requests" fill="#6366f1" radius={[2, 2, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            </Card>

            <Card title="Spend by model">
              {data.by_model.length === 0 ? (
                <p className="py-8 text-center text-sm text-zinc-500">No traffic in this window.</p>
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
  // Top 8 by cost for the chart; the table below lists them all.
  const top = models.slice(0, 8)
  return (
    <div className="space-y-4">
      <ResponsiveContainer width="100%" height={Math.max(120, top.length * 34)}>
        <BarChart data={top} layout="vertical" margin={{ top: 0, right: 16, left: 0, bottom: 0 }}>
          <XAxis type="number" tickFormatter={(v) => `$${v}`} fontSize={11} stroke="#a1a1aa" />
          <YAxis type="category" dataKey="model" width={150} fontSize={11} stroke="#a1a1aa" />
          <Tooltip formatter={(v: number) => [fmtUSD(v), 'Spend']} />
          <Bar dataKey="cost_usd" radius={[0, 2, 2, 0]}>
            {top.map((_, i) => (
              <Cell key={i} fill="#6366f1" />
            ))}
          </Bar>
        </BarChart>
      </ResponsiveContainer>

      <div className="overflow-x-auto rounded-lg border border-zinc-200 dark:border-zinc-800">
        <table className="w-full text-left text-sm">
          <thead className="border-b border-zinc-200 bg-zinc-50 text-xs uppercase tracking-wide text-zinc-500 dark:border-zinc-800 dark:bg-zinc-900">
            <tr>
              <Th>Model</Th>
              <Th className="text-right">Requests</Th>
              <Th className="text-right">Spend</Th>
              <Th className="text-right">In</Th>
              <Th className="text-right">Out</Th>
            </tr>
          </thead>
          <tbody className="divide-y divide-zinc-100 dark:divide-zinc-800">
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

function Stat({ label, value, accent }: { label: string; value: string; accent?: string }) {
  return (
    <div className="rounded-lg border border-zinc-200 bg-white p-4 dark:border-zinc-800 dark:bg-zinc-900">
      <div className="text-xs uppercase tracking-wide text-zinc-500">{label}</div>
      <div className={`mt-1 text-2xl font-semibold tabular-nums ${accent ?? ''}`}>{value}</div>
    </div>
  )
}

function Card({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="rounded-lg border border-zinc-200 bg-white p-4 dark:border-zinc-800 dark:bg-zinc-900">
      <h3 className="mb-3 text-sm font-medium text-zinc-600 dark:text-zinc-400">{title}</h3>
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
