import { useState } from 'react'
import { Layout } from '../components/Layout'
import { Modal } from '../components/Modal'
import { EmptyState, ErrorNotice, LoadingTable, MetricStrip, PageHeader } from '../components/PageChrome'
import { StatusBadge, Td, Th } from '../components/Table'
import { AccountForm } from '../components/AccountForm'
import { ApiError } from '../lib/api'
import {
  useAccounts,
  useCreateAccount,
  useDeleteAccount,
  useTestAccountById,
  useUpdateAccount,
  type Account,
  type AccountInput,
} from '../lib/accounts'

// editing tracks which dialog (if any) is open: 'new' for the create
// form, an Account for an edit, or null for the table view.
type Editing = 'new' | Account | null

export function AccountsPage() {
  const { data: accounts, isLoading, error } = useAccounts()
  const create = useCreateAccount()
  const update = useUpdateAccount()
  const del = useDeleteAccount()
  const [editing, setEditing] = useState<Editing>(null)
  const [formErr, setFormErr] = useState<string | null>(null)
  const accountCount = accounts?.length ?? 0
  const enabledCount = accounts?.filter((a) => a.enabled).length ?? 0
  const providerCount = accounts ? new Set(accounts.map((a) => a.provider)).size : 0
  const credentialCount = accounts?.reduce((sum, a) => sum + a.credential_keys.length, 0) ?? 0

  function openNew() {
    setFormErr(null)
    setEditing('new')
  }
  function openEdit(a: Account) {
    setFormErr(null)
    setEditing(a)
  }
  function close() {
    setEditing(null)
    setFormErr(null)
  }

  function handleSubmit(input: AccountInput) {
    setFormErr(null)
    const onError = (err: unknown) =>
      setFormErr(err instanceof ApiError ? err.message : 'Request failed.')
    if (editing === 'new') {
      create.mutate(input, { onSuccess: close, onError })
    } else if (editing) {
      update.mutate({ id: editing.id, input }, { onSuccess: close, onError })
    }
  }

  function handleDelete(a: Account) {
    if (!confirm(`Delete account "${a.name}"? This cannot be undone.`)) return
    del.mutate(a.id)
  }

  return (
    <Layout>
      <main className="mx-auto w-full max-w-7xl px-6 py-8">
        <PageHeader
          eyebrow="UPSTREAM"
          context="Provider account routing"
          title="Accounts"
          description="Manage provider credentials, routing weight, priority, and circuit behavior for upstream model traffic."
          action={
            <button onClick={openNew} className="btn btn-primary h-10">
              New account
            </button>
          }
        />

        <MetricStrip
          metrics={[
            { label: 'Total', value: accountCount },
            { label: 'Enabled', value: enabledCount },
            { label: 'Providers', value: providerCount },
            { label: 'Secrets', value: credentialCount },
          ]}
        />

        <div className="mt-6" />

        {isLoading && <LoadingTable />}
        {error && (
          <ErrorNotice>
            {error instanceof ApiError ? error.message : 'Could not load accounts.'}
          </ErrorNotice>
        )}

        {accounts && accounts.length === 0 && <EmptyAccounts onCreate={openNew} />}

        {accounts && accounts.length > 0 && (
          <div className="overflow-x-auto rounded-xl border border-line bg-surface">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
                <tr>
                  <Th>Name</Th>
                  <Th>Provider</Th>
                  <Th>Status</Th>
                  <Th className="text-right">Weight</Th>
                  <Th className="text-right">Priority</Th>
                  <Th className="text-right">Cost ×</Th>
                  <Th>Credentials</Th>
                  <Th className="text-right">Actions</Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {accounts.map((a) => (
                  <tr key={a.id} className="transition-colors hover:bg-surface-2">
                    <Td className="font-medium">{a.name}</Td>
                    <Td className="text-muted">{a.provider}</Td>
                    <Td>
                      <StatusBadge enabled={a.enabled} />
                    </Td>
                    <Td className="text-right tabular-nums">{a.weight}</Td>
                    <Td className="text-right tabular-nums">{a.priority}</Td>
                    <Td className="text-right tabular-nums">{a.cost_multiplier}</Td>
                    <Td className="text-muted">
                      {a.credential_keys.length > 0 ? a.credential_keys.join(', ') : '—'}
                    </Td>
                    <Td className="text-right">
                      <RowTest id={a.id} />
                      <button
                        onClick={() => openEdit(a)}
                        className="mr-3 text-muted underline-offset-4 hover:text-ink hover:underline"
                      >
                        Edit
                      </button>
                      <button
                        onClick={() => handleDelete(a)}
                        disabled={del.isPending}
                        className="btn-danger hover:underline disabled:opacity-50"
                      >
                        Delete
                      </button>
                    </Td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {del.error && (
          <div className="mt-3">
            <ErrorNotice>
            {del.error instanceof ApiError ? del.error.message : 'Delete failed.'}
            </ErrorNotice>
          </div>
        )}
      </main>

      {editing && (
        <Modal title={editing === 'new' ? 'New account' : `Edit ${editing.name}`} onClose={close}>
          <AccountForm
            account={editing === 'new' ? undefined : editing}
            submitting={create.isPending || update.isPending}
            error={formErr}
            onCancel={close}
            onSubmit={handleSubmit}
          />
        </Modal>
      )}
    </Layout>
  )
}

function AccountsGlyph() {
  return (
    <div className="relative overflow-hidden rounded-lg bg-surface-2 p-5" aria-hidden>
      <svg viewBox="0 0 300 128" className="h-32 w-full text-ink">
        <path
          d="M44 64h58M102 64c22 0 22-34 44-34h24M102 64c22 0 22 34 44 34h24M204 30h54M204 98h54M170 30h34M170 98h34"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.4"
          strokeLinecap="round"
          opacity="0.42"
        />
        <circle cx="44" cy="64" r="13" fill="var(--surface)" stroke="currentColor" strokeWidth="1.4" />
        <circle cx="170" cy="30" r="13" fill="var(--surface)" stroke="currentColor" strokeWidth="1.4" />
        <circle cx="170" cy="98" r="13" fill="var(--surface)" stroke="currentColor" strokeWidth="1.4" />
        <rect x="216" y="17" width="42" height="26" rx="8" fill="var(--surface)" stroke="currentColor" strokeWidth="1.4" />
        <rect x="216" y="85" width="42" height="26" rx="8" fill="var(--surface)" stroke="currentColor" strokeWidth="1.4" />
        <path d="M39 64h10M165 30h10M165 98h10" stroke="var(--brand)" strokeWidth="2" strokeLinecap="round" />
        <circle cx="237" cy="30" r="3" fill="var(--success)" />
        <circle cx="237" cy="98" r="3" fill="var(--brand)" />
      </svg>
      <div className="absolute left-4 top-4 font-mono text-[10px] uppercase tracking-[0.16em] text-muted">
        route map
      </div>
    </div>
  )
}

function EmptyAccounts({ onCreate }: { onCreate: () => void }) {
  return (
    <EmptyState
      eyebrow="No upstreams configured"
      title="Add the first provider account to start routing traffic."
      description="Store the account credential once, then tune routing weight, priority, and circuit-breaker overrides without touching client keys."
      action={
        <button onClick={onCreate} className="btn btn-primary h-10">
          New account
        </button>
      }
      visual={<AccountsGlyph />}
    />
  )
}

// RowTest is an inline per-row connectivity probe. It tests the account's
// stored credentials and shows a traffic-light dot with the latency /
// message on hover.
function RowTest({ id }: { id: number }) {
  const test = useTestAccountById()
  const r = test.data
  const tone = !r
    ? 'bg-line'
    : r.status === 'green'
      ? 'bg-emerald-500'
      : r.status === 'yellow'
        ? 'bg-amber-500'
        : 'bg-danger'
  const title = test.error
    ? test.error instanceof ApiError
      ? test.error.message
      : 'Test failed'
    : r
      ? `${r.message}${r.http_status ? ` · HTTP ${r.http_status}` : ''} · ${r.latency_ms}ms`
      : 'Test connectivity'
  return (
    <button
      onClick={() => test.mutate(id)}
      disabled={test.isPending}
      title={title}
      className="mr-3 inline-flex items-center gap-1.5 text-muted underline-offset-4 hover:text-ink hover:underline disabled:opacity-50"
    >
      <span className={`inline-block h-2 w-2 rounded-full ${test.error ? 'bg-danger' : tone}`} />
      {test.isPending ? 'Testing…' : 'Test'}
    </button>
  )
}
