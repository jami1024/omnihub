import { useState } from 'react'
import { Layout } from '../components/Layout'
import { Modal } from '../components/Modal'
import { StatusBadge, Td, Th } from '../components/Table'
import { KeyForm } from '../components/KeyForm'
import { ApiError } from '../lib/api'
import {
  useCreateKey,
  useDeleteKey,
  useKeys,
  useUpdateKey,
  type CreateKeyResult,
  type Key,
  type KeyInput,
} from '../lib/keys'

type Editing = 'new' | Key | null

export function KeysPage() {
  const { data: keys, isLoading, error } = useKeys()
  const create = useCreateKey()
  const update = useUpdateKey()
  const del = useDeleteKey()
  const [editing, setEditing] = useState<Editing>(null)
  const [formErr, setFormErr] = useState<string | null>(null)
  // Holds the freshly minted cleartext so it can be shown once, in its
  // own dialog, after the create form closes.
  const [revealed, setRevealed] = useState<CreateKeyResult | null>(null)

  function openNew() {
    setFormErr(null)
    setEditing('new')
  }
  function openEdit(k: Key) {
    setFormErr(null)
    setEditing(k)
  }
  function close() {
    setEditing(null)
    setFormErr(null)
  }

  function handleSubmit(input: KeyInput) {
    setFormErr(null)
    const onError = (err: unknown) =>
      setFormErr(err instanceof ApiError ? err.message : 'Request failed.')
    if (editing === 'new') {
      create.mutate(input, {
        onSuccess: (result) => {
          close()
          setRevealed(result) // pop the one-time reveal
        },
        onError,
      })
    } else if (editing) {
      update.mutate({ id: editing.id, input }, { onSuccess: close, onError })
    }
  }

  function handleDelete(k: Key) {
    if (!confirm(`Delete key "${k.name}"? Any client using it will stop working immediately.`))
      return
    del.mutate(k.id)
  }

  return (
    <Layout>
      <main className="mx-auto max-w-6xl px-6 py-10">
        <div className="mb-6 flex items-center justify-between">
          <div>
            <h2 className="text-xl font-semibold">API keys</h2>
            <p className="text-sm text-zinc-500">Virtual keys clients use to authenticate to the gateway.</p>
          </div>
          <button
            onClick={openNew}
            className="rounded-md bg-zinc-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-zinc-700 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-300"
          >
            New key
          </button>
        </div>

        {isLoading && <p className="text-sm text-zinc-500">Loading…</p>}
        {error && (
          <p className="text-sm text-red-600 dark:text-red-400">
            {error instanceof ApiError ? error.message : 'Could not load keys.'}
          </p>
        )}

        {keys && keys.length === 0 && (
          <div className="rounded-lg border border-dashed border-zinc-300 p-10 text-center text-sm text-zinc-500 dark:border-zinc-700">
            No keys yet. Create one to let a client authenticate.
          </div>
        )}

        {keys && keys.length > 0 && (
          <div className="overflow-x-auto rounded-lg border border-zinc-200 dark:border-zinc-800">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-zinc-200 bg-zinc-50 text-xs uppercase tracking-wide text-zinc-500 dark:border-zinc-800 dark:bg-zinc-900">
                <tr>
                  <Th>Name</Th>
                  <Th>Label</Th>
                  <Th>Status</Th>
                  <Th className="text-right">Daily USD</Th>
                  <Th className="text-right">RPM</Th>
                  <Th>Models</Th>
                  <Th className="text-right">Actions</Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-zinc-100 dark:divide-zinc-800">
                {keys.map((k) => (
                  <tr key={k.id} className="hover:bg-zinc-50 dark:hover:bg-zinc-900/50">
                    <Td className="font-medium">{k.name}</Td>
                    <Td className="text-zinc-500">{k.label || '—'}</Td>
                    <Td>
                      <StatusBadge enabled={k.enabled} />
                    </Td>
                    <Td className="text-right tabular-nums">
                      {k.daily_usd_limit == null ? '—' : k.daily_usd_limit.toFixed(2)}
                    </Td>
                    <Td className="text-right tabular-nums">{k.rpm_limit ?? '—'}</Td>
                    <Td className="text-zinc-500">
                      {k.allowed_models.length > 0 ? k.allowed_models.join(', ') : 'all'}
                    </Td>
                    <Td className="text-right">
                      <button
                        onClick={() => openEdit(k)}
                        className="mr-3 text-zinc-600 hover:underline dark:text-zinc-300"
                      >
                        Edit
                      </button>
                      <button
                        onClick={() => handleDelete(k)}
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
        <Modal title={editing === 'new' ? 'New key' : `Edit ${editing.name}`} onClose={close}>
          <KeyForm
            apiKey={editing === 'new' ? undefined : editing}
            submitting={create.isPending || update.isPending}
            error={formErr}
            onCancel={close}
            onSubmit={handleSubmit}
          />
        </Modal>
      )}

      {revealed && <RevealKey result={revealed} onClose={() => setRevealed(null)} />}
    </Layout>
  )
}

// RevealKey shows the cleartext exactly once. It cannot be recovered
// after the operator dismisses this dialog.
function RevealKey({ result, onClose }: { result: CreateKeyResult; onClose: () => void }) {
  const [copied, setCopied] = useState(false)
  function copy() {
    navigator.clipboard?.writeText(result.key).then(
      () => setCopied(true),
      () => setCopied(false),
    )
  }
  return (
    <Modal title={`Key "${result.name}" created`} onClose={onClose}>
      <p className="text-sm text-zinc-600 dark:text-zinc-400">
        Copy this key now — it is shown only once and cannot be retrieved again.
      </p>
      <div className="mt-3 flex items-center gap-2">
        <code className="flex-1 break-all rounded-md border border-zinc-200 bg-zinc-50 px-3 py-2 font-mono text-sm dark:border-zinc-700 dark:bg-zinc-950">
          {result.key}
        </code>
        <button
          onClick={copy}
          className="shrink-0 rounded-md border border-zinc-300 px-3 py-2 text-sm hover:bg-zinc-100 dark:border-zinc-700 dark:hover:bg-zinc-800"
        >
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
      <div className="mt-5 flex justify-end">
        <button
          onClick={onClose}
          className="rounded-md bg-zinc-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-zinc-700 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-300"
        >
          Done
        </button>
      </div>
    </Modal>
  )
}
