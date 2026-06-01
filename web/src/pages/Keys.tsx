import { useState } from 'react'
import { Layout } from '../components/Layout'
import { Modal } from '../components/Modal'
import { EmptyState, ErrorNotice, LoadingTable, MetricStrip, PageHeader } from '../components/PageChrome'
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
  const keyCount = keys?.length ?? 0
  const enabledCount = keys?.filter((k) => k.enabled).length ?? 0
  const budgetedCount = keys?.filter((k) => k.daily_usd_limit != null || k.rpm_limit != null).length ?? 0
  const modelScopedCount = keys?.filter((k) => k.allowed_models.length > 0).length ?? 0

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
      <main className="mx-auto w-full max-w-7xl px-6 py-8">
        <PageHeader
          eyebrow="ACCESS"
          context="Client authentication"
          title="API keys"
          description="Issue virtual keys for clients, then constrain spend, request rate, and model access without exposing upstream credentials."
          action={
            <button onClick={openNew} className="btn btn-primary h-10">
              New key
            </button>
          }
        />

        <MetricStrip
          metrics={[
            { label: 'Total', value: keyCount },
            { label: 'Enabled', value: enabledCount },
            { label: 'Budgeted', value: budgetedCount },
            { label: 'Model scoped', value: modelScopedCount },
          ]}
        />

        <div className="mt-6" />

        {isLoading && <LoadingTable />}
        {error && (
          <ErrorNotice>
            {error instanceof ApiError ? error.message : 'Could not load keys.'}
          </ErrorNotice>
        )}

        {keys && keys.length === 0 && (
          <EmptyState
            eyebrow="No client keys"
            title="Create a virtual key before clients connect."
            description="Client keys authenticate gateway traffic while keeping upstream account credentials private and operator-controlled."
            action={
              <button onClick={openNew} className="btn btn-primary h-10">
                New key
              </button>
            }
          />
        )}

        {keys && keys.length > 0 && (
          <div className="overflow-x-auto rounded-xl border border-line bg-surface">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
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
              <tbody className="divide-y divide-line">
                {keys.map((k) => (
                  <tr key={k.id} className="transition-colors hover:bg-surface-2">
                    <Td className="font-medium">{k.name}</Td>
                    <Td className="text-muted">{k.label || '—'}</Td>
                    <Td>
                      <StatusBadge enabled={k.enabled} />
                    </Td>
                    <Td className="text-right tabular-nums">
                      {k.daily_usd_limit == null ? '—' : k.daily_usd_limit.toFixed(2)}
                    </Td>
                    <Td className="text-right tabular-nums">{k.rpm_limit ?? '—'}</Td>
                    <Td className="text-muted">
                      {k.allowed_models.length > 0 ? k.allowed_models.join(', ') : 'all'}
                    </Td>
                    <Td className="text-right">
                      <button
                        onClick={() => openEdit(k)}
                        className="mr-3 text-muted underline-offset-4 hover:text-ink hover:underline"
                      >
                        Edit
                      </button>
                      <button
                        onClick={() => handleDelete(k)}
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
      <p className="text-sm text-muted">
        Copy this key now — it is shown only once and cannot be retrieved again.
      </p>
      <div className="mt-3 flex items-center gap-2">
        <code className="flex-1 break-all rounded-lg border border-line bg-surface-2 px-3 py-2 font-mono text-sm">
          {result.key}
        </code>
        <button
          onClick={copy}
          className="btn btn-secondary shrink-0"
        >
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
      <div className="mt-5 flex justify-end">
        <button
          onClick={onClose}
          className="btn btn-primary"
        >
          Done
        </button>
      </div>
    </Modal>
  )
}
