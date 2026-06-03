// Shared table primitives: header/data cells with consistent padding,
// and the enabled/disabled status pill. Tables stay legible dense — the
// price table renders 2,000+ rows.

import { useI18n } from '../lib/i18n'

export function Th({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return (
    <th className={`px-4 py-2.5 text-xs font-medium uppercase tracking-wide text-muted ${className}`}>
      {children}
    </th>
  )
}

export function Td({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return <td className={`px-4 py-2.5 ${className}`}>{children}</td>
}

export function StatusBadge({ enabled }: { enabled: boolean }) {
  const { t } = useI18n()
  return (
    <span className={`badge ${enabled ? 'badge-success' : 'badge-neutral'}`}>
      <span
        className="mr-1.5 inline-block h-1.5 w-1.5 rounded-full"
        style={{ background: 'currentColor' }}
        aria-hidden
      />
      {enabled ? t('common.enabled') : t('common.disabled')}
    </span>
  )
}

// Table wraps the standard chrome: rounded bordered surface, header on
// surface-2, hairline row dividers. Children are the <thead>/<tbody>.
export function Table({ children }: { children: React.ReactNode }) {
  return (
    <div className="overflow-x-auto rounded-xl border border-line bg-surface">
      <table className="w-full text-left text-sm">{children}</table>
    </div>
  )
}

export function THead({ children }: { children: React.ReactNode }) {
  return (
    <thead className="border-b border-line bg-surface-2">
      <tr>{children}</tr>
    </thead>
  )
}

export function TBody({ children }: { children: React.ReactNode }) {
  return <tbody className="divide-y divide-line">{children}</tbody>
}

export function Tr({ children }: { children: React.ReactNode }) {
  return <tr className="transition-colors hover:bg-surface-2">{children}</tr>
}
