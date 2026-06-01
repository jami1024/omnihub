import { useState } from 'react'
import { PortalLayout } from '../../components/PortalLayout'
import { Modal } from '../../components/Modal'
import { StatusBadge, Td, Th } from '../../components/Table'
import { ApiError } from '../../lib/portalApi'
import {
  useCreatePortalKey,
  useDeletePortalKey,
  usePortalKeys,
  type CreatePortalKeyResult,
  type PortalKey,
} from '../../lib/portalData'

export function PortalKeysPage() {
  const { data: keys, isLoading, error } = usePortalKeys()
  const create = useCreatePortalKey()
  const del = useDeletePortalKey()
  const [showForm, setShowForm] = useState(false)
  const [revealed, setRevealed] = useState<CreatePortalKeyResult | null>(null)

  function handleDelete(k: PortalKey) {
    if (!confirm(`Delete key "${k.name}"? Anything using it stops working immediately.`)) return
    del.mutate(k.id)
  }

  return (
    <PortalLayout>
      <main className="mx-auto max-w-5xl px-6 py-8">
        <div className="mb-6 flex items-center justify-between">
          <div>
            <h2 className="text-xl font-semibold">API keys</h2>
            <p className="text-sm text-muted">Use a key as the Bearer token against the gateway.</p>
          </div>
          <button onClick={() => setShowForm(true)} className="btn btn-primary">
            New key
          </button>
        </div>

        {isLoading && <p className="text-sm text-muted">Loading…</p>}
        {error && (
          <p className="text-sm text-danger">
            {error instanceof ApiError ? error.message : 'Could not load keys.'}
          </p>
        )}

        {keys && keys.length === 0 && (
          <div className="rounded-xl border border-dashed border-line-strong p-10 text-center text-sm text-muted">
            No keys yet. Create one to start calling the gateway.
          </div>
        )}

        {keys && keys.length > 0 && (
          <div className="overflow-x-auto rounded-xl border border-line bg-surface">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
                <tr>
                  <Th>Name</Th>
                  <Th>Status</Th>
                  <Th className="text-right">Daily USD</Th>
                  <Th className="text-right">RPM</Th>
                  <Th className="text-right">Spent (24h)</Th>
                  <Th className="text-right">Actions</Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {keys.map((k) => (
                  <tr key={k.id} className="hover:bg-surface-2">
                    <Td className="font-mono text-xs">{k.name}</Td>
                    <Td>
                      <StatusBadge enabled={k.enabled} />
                    </Td>
                    <Td className="text-right tabular-nums">
                      {k.daily_usd_limit == null ? '—' : `$${k.daily_usd_limit.toFixed(2)}`}
                    </Td>
                    <Td className="text-right tabular-nums">{k.rpm_limit ?? '—'}</Td>
                    <Td className="text-right tabular-nums">${k.spend_24h.toFixed(4)}</Td>
                    <Td className="text-right">
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
      </main>

      {showForm && (
        <CreateKeyModal
          submitting={create.isPending}
          onClose={() => setShowForm(false)}
          onCreate={(input) =>
            create.mutate(input, {
              onSuccess: (res) => {
                setShowForm(false)
                setRevealed(res)
              },
            })
          }
          error={create.error instanceof ApiError ? create.error.message : null}
        />
      )}

      {revealed && <RevealKey result={revealed} onClose={() => setRevealed(null)} />}
    </PortalLayout>
  )
}

function CreateKeyModal({
  submitting,
  error,
  onClose,
  onCreate,
}: {
  submitting: boolean
  error: string | null
  onClose: () => void
  onCreate: (input: { name: string; daily_usd_limit: number | null; rpm_limit: number | null; allowed_models: string[] }) => void
}) {
  const [name, setName] = useState('')
  const [daily, setDaily] = useState('')
  const [rpm, setRpm] = useState('')
  const [localErr, setLocalErr] = useState<string | null>(null)

  function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!name.trim()) {
      setLocalErr('Name is required.')
      return
    }
    onCreate({
      name: name.trim(),
      daily_usd_limit: daily.trim() ? Number(daily) : null,
      rpm_limit: rpm.trim() ? Number(rpm) : null,
      allowed_models: [],
    })
  }

  return (
    <Modal title="New API key" onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        <label className="block space-y-1">
          <span className="text-sm text-muted">Name</span>
          <input className="field" value={name} onChange={(e) => setName(e.target.value)} autoFocus placeholder="my-app" />
        </label>
        <div className="grid grid-cols-2 gap-4">
          <label className="block space-y-1">
            <span className="text-sm text-muted">Daily USD limit (optional)</span>
            <input className="field" type="number" step="0.01" value={daily} onChange={(e) => setDaily(e.target.value)} placeholder="no limit" />
          </label>
          <label className="block space-y-1">
            <span className="text-sm text-muted">RPM limit (optional)</span>
            <input className="field" type="number" value={rpm} onChange={(e) => setRpm(e.target.value)} placeholder="no limit" />
          </label>
        </div>
        {(localErr || error) && <p className="text-sm text-danger">{localErr ?? error}</p>}
        <div className="flex justify-end gap-2">
          <button type="button" onClick={onClose} className="btn btn-secondary">
            Cancel
          </button>
          <button type="submit" disabled={submitting} className="btn btn-primary">
            {submitting ? 'Creating…' : 'Create key'}
          </button>
        </div>
      </form>
    </Modal>
  )
}

function RevealKey({ result, onClose }: { result: CreatePortalKeyResult; onClose: () => void }) {
  const [copied, setCopied] = useState(false)
  return (
    <Modal title={`Key "${result.name}" created`} onClose={onClose}>
      <p className="text-sm text-muted">Copy it now — it's shown only once and can't be retrieved again.</p>
      <div className="mt-3 flex items-center gap-2">
        <code className="flex-1 break-all rounded-lg border border-line bg-surface-2 px-3 py-2 font-mono text-sm">
          {result.key}
        </code>
        <button
          onClick={() => navigator.clipboard?.writeText(result.key).then(() => setCopied(true))}
          className="btn btn-secondary shrink-0"
        >
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
      <div className="mt-5 flex justify-end">
        <button onClick={onClose} className="btn btn-primary">
          Done
        </button>
      </div>
    </Modal>
  )
}
