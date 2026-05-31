import { useState } from 'react'
import { Layout } from '../components/Layout'
import { AccountForm } from '../components/AccountForm'
import { ApiError } from '../lib/api'
import {
  useAccounts,
  useCreateAccount,
  useDeleteAccount,
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
      <main className="mx-auto max-w-6xl px-6 py-10">
        <div className="mb-6 flex items-center justify-between">
          <div>
            <h2 className="text-xl font-semibold">Accounts</h2>
            <p className="text-sm text-zinc-500">Upstream provider accounts the gateway routes through.</p>
          </div>
          <button
            onClick={openNew}
            className="rounded-md bg-zinc-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-zinc-700 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-300"
          >
            New account
          </button>
        </div>

        {isLoading && <p className="text-sm text-zinc-500">Loading…</p>}
        {error && (
          <p className="text-sm text-red-600 dark:text-red-400">
            {error instanceof ApiError ? error.message : 'Could not load accounts.'}
          </p>
        )}

        {accounts && accounts.length === 0 && (
          <div className="rounded-lg border border-dashed border-zinc-300 p-10 text-center text-sm text-zinc-500 dark:border-zinc-700">
            No accounts yet. Create one to start routing traffic.
          </div>
        )}

        {accounts && accounts.length > 0 && (
          <div className="overflow-x-auto rounded-lg border border-zinc-200 dark:border-zinc-800">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-zinc-200 bg-zinc-50 text-xs uppercase tracking-wide text-zinc-500 dark:border-zinc-800 dark:bg-zinc-900">
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
              <tbody className="divide-y divide-zinc-100 dark:divide-zinc-800">
                {accounts.map((a) => (
                  <tr key={a.id} className="hover:bg-zinc-50 dark:hover:bg-zinc-900/50">
                    <Td className="font-medium">{a.name}</Td>
                    <Td className="text-zinc-500">{a.provider}</Td>
                    <Td>
                      <StatusBadge enabled={a.enabled} />
                    </Td>
                    <Td className="text-right tabular-nums">{a.weight}</Td>
                    <Td className="text-right tabular-nums">{a.priority}</Td>
                    <Td className="text-right tabular-nums">{a.cost_multiplier}</Td>
                    <Td className="text-zinc-500">
                      {a.credential_keys.length > 0 ? a.credential_keys.join(', ') : '—'}
                    </Td>
                    <Td className="text-right">
                      <button
                        onClick={() => openEdit(a)}
                        className="mr-3 text-zinc-600 hover:underline dark:text-zinc-300"
                      >
                        Edit
                      </button>
                      <button
                        onClick={() => handleDelete(a)}
                        disabled={del.isPending}
                        className="text-red-600 hover:underline disabled:opacity-50 dark:text-red-400"
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
          <p className="mt-3 text-sm text-red-600 dark:text-red-400">
            {del.error instanceof ApiError ? del.error.message : 'Delete failed.'}
          </p>
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

function Modal({
  title,
  onClose,
  children,
}: {
  title: string
  onClose: () => void
  children: React.ReactNode
}) {
  return (
    <div
      className="fixed inset-0 z-10 flex items-start justify-center overflow-y-auto bg-black/40 p-6"
      onClick={onClose}
    >
      <div
        className="mt-10 w-full max-w-2xl rounded-lg border border-zinc-200 bg-white p-6 shadow-xl dark:border-zinc-800 dark:bg-zinc-900"
        onClick={(e) => e.stopPropagation()}
      >
        <h3 className="mb-4 text-lg font-semibold">{title}</h3>
        {children}
      </div>
    </div>
  )
}

function StatusBadge({ enabled }: { enabled: boolean }) {
  return enabled ? (
    <span className="inline-flex items-center rounded-full bg-green-100 px-2 py-0.5 text-xs font-medium text-green-700 dark:bg-green-900/40 dark:text-green-400">
      Enabled
    </span>
  ) : (
    <span className="inline-flex items-center rounded-full bg-zinc-100 px-2 py-0.5 text-xs font-medium text-zinc-500 dark:bg-zinc-800">
      Disabled
    </span>
  )
}

function Th({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return <th className={`px-4 py-2 font-medium ${className}`}>{children}</th>
}
function Td({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return <td className={`px-4 py-2.5 ${className}`}>{children}</td>
}
