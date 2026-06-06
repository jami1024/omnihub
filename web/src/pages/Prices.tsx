import { useMemo, useState } from 'react'
import { Layout } from '../components/Layout'
import { Modal } from '../components/Modal'
import { EmptyState, ErrorNotice, LoadingTable, MetricStrip, PageHeader } from '../components/PageChrome'
import { Td, Th } from '../components/Table'
import { PriceForm } from '../components/PriceForm'
import { ApiError } from '../lib/api'
import { useI18n } from '../lib/i18n'
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
  const { t } = useI18n()
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
  const priceCount = prices?.length ?? 0
  const manualCount = prices?.filter((p) => p.source === 'manual').length ?? 0
  const syncedCount = prices?.filter((p) => p.source !== 'manual').length ?? 0

  function close() {
    setEditing(null)
    setFormErr(null)
  }
  function handleSubmit(input: PriceInput) {
    setFormErr(null)
    const onError = (err: unknown) =>
      setFormErr(err instanceof ApiError ? err.message : t('prices.requestFailed'))
    if (editing === 'new') create.mutate(input, { onSuccess: close, onError })
    else if (editing) update.mutate({ id: editing.id, input }, { onSuccess: close, onError })
  }
  function handleDelete(p: ModelPrice) {
    if (!confirm(t('prices.deleteConfirm', { model: p.model }))) return
    del.mutate(p.id)
  }
  function handleSync() {
    setSyncMsg(null)
    sync.mutate(undefined, {
      onSuccess: (r: SyncResult) =>
        setSyncMsg(t('prices.syncResult', { added: r.added, updated: r.updated, skipped: r.skipped })),
      onError: (e) => setSyncMsg(e instanceof ApiError ? e.message : t('prices.syncFailed')),
    })
  }

  return (
    <Layout>
      <main className="mx-auto w-full max-w-7xl px-6 py-8">
        <PageHeader
          eyebrow={t('prices.eyebrow')}
          context={t('prices.context')}
          title={t('prices.title')}
          description={t('prices.description')}
          action={
            <div className="flex flex-wrap gap-2">
            <button
              onClick={handleSync}
              disabled={sync.isPending}
              className="btn btn-secondary h-10"
            >
              {sync.isPending ? t('prices.syncing') : t('prices.syncFromLiteLLM')}
            </button>
            <button
              onClick={() => {
                setFormErr(null)
                setEditing('new')
              }}
              className="btn btn-primary h-10"
            >
              {t('prices.addPrice')}
            </button>
          </div>
          }
        />

        <MetricStrip
          metrics={[
            { label: t('prices.metricTotal'), value: priceCount },
            { label: t('prices.metricManual'), value: manualCount },
            { label: t('prices.metricSynced'), value: syncedCount },
            { label: t('prices.metricShowing'), value: shown.length },
          ]}
        />

        {syncMsg && (
          <p className="mt-6 rounded-xl border border-line bg-surface px-4 py-3 text-sm text-muted">
            {syncMsg}
          </p>
        )}

        <input
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder={t('prices.filterPlaceholder')}
          className="field my-4 h-10 max-w-sm"
        />

        {isLoading && <LoadingTable columns={5} />}
        {error && (
          <ErrorNotice>
            {error instanceof ApiError ? error.message : t('prices.loadError')}
          </ErrorNotice>
        )}

        {prices && prices.length === 0 && (
          <EmptyState
            eyebrow={t('prices.emptyEyebrow')}
            title={t('prices.emptyTitle')}
            description={t('prices.emptyDescription')}
            action={
              <div className="flex flex-wrap gap-2">
                <button onClick={handleSync} disabled={sync.isPending} className="btn btn-secondary h-10">
                  {sync.isPending ? t('prices.syncing') : t('prices.syncFromLiteLLM')}
                </button>
                <button
                  onClick={() => {
                    setFormErr(null)
                    setEditing('new')
                  }}
                  className="btn btn-primary h-10"
                >
                  {t('prices.addPrice')}
                </button>
              </div>
            }
          />
        )}

        {prices && prices.length > 0 && (
          <>
            <div className="overflow-x-auto rounded-xl border border-line bg-surface">
              <table className="w-full text-left text-sm">
                <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
                  <tr>
                    <Th>{t('prices.colModel')}</Th>
                    <Th>{t('prices.colSource')}</Th>
                    <Th className="text-right">{t('prices.colInputPerM')}</Th>
                    <Th className="text-right">{t('prices.colOutputPerM')}</Th>
                    <Th className="text-right">{t('prices.colCacheReadPerM')}</Th>
                    <Th className="text-right">{t('common.actions')}</Th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-line">
                  {shown.map((p) => (
                    <tr key={p.id} className="transition-colors hover:bg-surface-2">
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
                          className="mr-1 inline-flex min-h-10 items-center rounded-md px-2 text-muted underline-offset-4 hover:bg-surface-2 hover:text-ink hover:underline sm:mr-3 sm:px-1"
                        >
                          {t('common.edit')}
                        </button>
                        <button
                          onClick={() => handleDelete(p)}
                          disabled={del.isPending}
                          className="inline-flex min-h-10 items-center rounded-md px-2 btn-danger hover:underline disabled:opacity-50"
                        >
                          {t('common.delete')}
                        </button>
                      </Td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <p className="mt-3 text-xs text-muted">
              {t('prices.showingOf', { shown: shown.length, total: filtered.length })}
              {filtered.length !== (prices?.length ?? 0) && ` ${t('prices.filteredFrom', { count: prices.length })}`}
              {filtered.length > CAP && ` ${t('prices.refineFilter')}`}.
            </p>
          </>
        )}

        {del.error && (
          <div className="mt-3">
            <ErrorNotice>
              {del.error instanceof ApiError ? del.error.message : t('prices.deleteFailed')}
            </ErrorNotice>
          </div>
        )}
      </main>

      {editing && (
        <Modal title={editing === 'new' ? t('prices.addPrice') : t('prices.editModel', { model: editing.model })} onClose={close}>
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
