import type React from 'react'
import { useI18n } from '../lib/i18n'

// Shared page chrome for admin surfaces. The goal is a compact control
// plane rhythm: clear page title, one primary action, optional summary
// strip, and purposeful empty/loading/error states.

export function PageHeader({
  eyebrow,
  context,
  title,
  description,
  action,
}: {
  eyebrow: string
  context: string
  title: string
  description: string
  action?: React.ReactNode
}) {
  return (
    <section className="mb-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <div className="mb-3 flex flex-wrap items-center gap-2">
            <span className="badge badge-neutral font-mono tracking-[0.14em]">
              {eyebrow}
            </span>
            <span className="text-sm text-muted">{context}</span>
          </div>
          <h1 className="text-2xl font-semibold tracking-tight text-ink">{title}</h1>
          <p className="mt-2 max-w-2xl text-sm leading-6 text-muted">{description}</p>
        </div>
        {action && <div className="shrink-0">{action}</div>}
      </div>
    </section>
  )
}

export function MetricStrip({
  metrics,
}: {
  metrics: Array<{ label: string; value: React.ReactNode; tone?: 'danger' | 'warning' }>
}) {
  return (
    <div className="mt-6 grid grid-cols-2 gap-px overflow-hidden rounded-xl border border-line bg-line sm:grid-cols-4">
      {metrics.map((m) => (
        <div key={m.label} className="bg-surface px-4 py-3">
          <p className="font-mono text-[11px] uppercase tracking-[0.14em] text-muted">
            {m.label}
          </p>
          <p
            className={`mt-1 text-2xl font-semibold tabular-nums tracking-tight ${
              m.tone === 'danger'
                ? 'text-danger'
                : m.tone === 'warning'
                  ? 'text-warning'
                  : ''
            }`}
          >
            {m.value}
          </p>
        </div>
      ))}
    </div>
  )
}

export function LoadingTable({ rows = 3, columns = 4 }: { rows?: number; columns?: number }) {
  const { t } = useI18n()
  return (
    <div className="overflow-hidden rounded-xl border border-line bg-surface" aria-label={t('common.loading')}>
      <div className="border-b border-line bg-surface-2 px-4 py-3">
        <div className="h-3 w-44 animate-pulse rounded-full bg-line-strong" />
      </div>
      <div className="divide-y divide-line">
        {Array.from({ length: rows }).map((_, row) => (
          <div
            key={row}
            className="grid gap-4 px-4 py-4"
            style={{ gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))` }}
          >
            {Array.from({ length: columns }).map((_, col) => (
              <div key={col} className="h-3 animate-pulse rounded-full bg-line" />
            ))}
          </div>
        ))}
      </div>
    </div>
  )
}

export function ErrorNotice({ children }: { children: React.ReactNode }) {
  return (
    <p className="rounded-xl border border-danger/20 bg-danger-bg px-4 py-3 text-sm text-danger">
      {children}
    </p>
  )
}

export function EmptyState({
  eyebrow,
  title,
  description,
  action,
  visual,
}: {
  eyebrow: string
  title: string
  description: string
  action?: React.ReactNode
  visual?: React.ReactNode
}) {
  return (
    <section className="grid gap-6 rounded-xl border border-dashed border-line-strong bg-surface p-6 sm:grid-cols-[1fr_18rem] sm:items-center sm:p-8">
      <div>
        <p className="font-mono text-xs font-medium uppercase tracking-[0.16em] text-muted">
          {eyebrow}
        </p>
        <h3 className="mt-3 text-2xl font-semibold tracking-tight">{title}</h3>
        <p className="mt-3 max-w-xl text-sm leading-6 text-muted">{description}</p>
        {action && <div className="mt-6">{action}</div>}
      </div>
      {visual}
    </section>
  )
}
