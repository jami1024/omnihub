import { useMemo, useState } from 'react'
import { Layout } from '../components/Layout'
import { Modal } from '../components/Modal'
import { Td, Th } from '../components/Table'
import { PriceForm } from '../components/PriceForm'
import { ApiError } from '../lib/api'
import {
  toPerMillion,
  useCreatePrice,
  useDeletePrice,
  usePrices,
  useSyncPrices,
  useUpdatePrice,
  type ModelPrice,
  type PriceInput,
  type SyncResult,
} from '../lib/prices'

type Editing = 'new' | ModelPrice | null

export function PricesPage() {
  const { data: prices, isLoading, error } = usePrices()
  const create = useCreatePrice()
  const update = useUpdatePrice()
  const del = useDeletePrice()
  const sync = useSyncPrices()
  const [editing, setEditing] = useState<Editing>(null)
  const [formErr, setFormErr] = useState<string | null>(null)
  const [filter, setFilter] = useState('')
  const [syncMsg, setSyncMsg] = useState<string | null>(null)

  // A LiteLLM sync lands hundreds of rows; filter + cap the render so
  // the table stays usable instead of painting 1000 rows.
  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase()
    const rows = prices ?? []
    return q ? rows.filter((p) => p.model.toLowerCase().includes(q)) : rows
  }, [prices, filter])
  const CAP = 200
  const shown = filtered.slice(0, CAP)

  function close() {
    setEditing(null)
    setFormErr(null)
  }
  function handleSubmit(input: PriceInput) {
    setFormErr(null)
    const onError = (err: unknown) =>
      setFormErr(err instanceof ApiError ? err.message : 'Request failed.')
    if (editing === 'new') create.mutate(input, { onSuccess: close, onError })
    else if (editing) update.mutate({ id: editing.id, input }, { onSuccess: close, onError })
  }
  function handleDelete(p: ModelPrice) {
    if (!confirm(`Delete the price for "${p.model}"?`)) return
    del.mutate(p.id)
  }
  function handleSync() {
    setSyncMsg(null)
    sync.mutate(undefined, {
      onSuccess: (r: SyncResult) =>
        setSyncMsg(`Synced: ${r.added} added, ${r.updated} updated, ${r.skipped} manual kept.`),
      onError: (e) => setSyncMsg(e instanceof ApiError ? e.message : 'Sync failed.'),
    })
  }

  return (
    <Layout>
      <main className="mx-auto max-w-7xl px-6 py-8">
        <div className="mb-4 flex items-center justify-between">
          <div>
            <h2 className="text-xl font-semibold">Model prices</h2>
            <p className="text-sm text-muted">
              USD per million tokens. Synced from LiteLLM; manual rows override and survive re-sync.
            </p>
          </div>
          <div className="flex gap-2">
            <button
              onClick={handleSync}
              disabled={sync.isPending}
              className="btn btn-secondary"
            >
              {sync.isPending ? 'Syncing…' : 'Sync from LiteLLM'}
            </button>
            <button
              onClick={() => {
                setFormErr(null)
                setEditing('new')
              }}
              className="btn btn-primary"
            >
              Add price
            </button>
          </div>
        </div>

        {syncMsg && <p className="mb-3 text-sm text-muted">{syncMsg}</p>}

        <input
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Filter by model…"
          className="field mb-4 max-w-sm"
        />

        {isLoading && <p className="text-sm text-muted">Loading…</p>}
        {error && (
          <p className="text-sm text-danger">
            {error instanceof ApiError ? error.message : 'Could not load prices.'}
          </p>
        )}

        {prices && (
          <>
            <div className="overflow-x-auto rounded-xl border border-line bg-surface">
              <table className="w-full text-left text-sm">
                <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
                  <tr>
                    <Th>Model</Th>
                    <Th>Source</Th>
                    <Th className="text-right">Input /M</Th>
                    <Th className="text-right">Output /M</Th>
                    <Th className="text-right">Cache read /M</Th>
                    <Th className="text-right">Actions</Th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-line">
                  {shown.map((p) => (
                    <tr key={p.id} className="hover:bg-surface-2">
                      <Td className="font-mono text-xs">{p.model}</Td>
                      <Td>
                        <SourceBadge source={p.source} />
                      </Td>
                      <Td className="text-right tabular-nums">{money(p.input_cost_per_token)}</Td>
                      <Td className="text-right tabular-nums">{money(p.output_cost_per_token)}</Td>
                      <Td className="text-right tabular-nums">
                        {money(p.cache_read_input_token_cost)}
                      </Td>
                      <Td className="text-right">
                        <button
                          onClick={() => {
                            setFormErr(null)
                            setEditing(p)
                          }}
                          className="mr-3 text-muted hover:text-ink hover:underline"
                        >
                          Edit
                        </button>
                        <button
                          onClick={() => handleDelete(p)}
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
            <p className="mt-3 text-xs text-muted">
              Showing {shown.length} of {filtered.length}
              {filtered.length !== (prices?.length ?? 0) && ` (filtered from ${prices.length})`}
              {filtered.length > CAP && ` — refine the filter to see the rest`}.
            </p>
          </>
        )}

        {del.error && (
          <p className="mt-3 text-sm text-danger">
            {del.error instanceof ApiError ? del.error.message : 'Delete failed.'}
          </p>
        )}
      </main>

      {editing && (
        <Modal title={editing === 'new' ? 'Add price' : `Edit ${editing.model}`} onClose={close}>
          <PriceForm
            price={editing === 'new' ? undefined : editing}
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

function SourceBadge({ source }: { source: string }) {
  const manual = source === 'manual'
  return (
    <span
      className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${
        manual
          ? 'bg-brand-subtle text-brand'
          : 'surface-2 text-muted'
      }`}
    >
      {source}
    </span>
  )
}

// money renders a per-token rate as a per-million-token dollar figure.
function money(perToken: number): string {
  if (!perToken) return '—'
  const perM = toPerMillion(perToken)
  return `$${perM.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 4 })}`
}
