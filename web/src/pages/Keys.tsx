import { useState } from 'react'
import { Layout } from '../components/Layout'
import { Modal } from '../components/Modal'
import { EmptyState, ErrorNotice, LoadingTable, MetricStrip, PageHeader } from '../components/PageChrome'
import { StatusBadge, Td, Th } from '../components/Table'
import { KeyForm } from '../components/KeyForm'
import { ApiError } from '../lib/api'
import { useI18n } from '../lib/i18n'
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
  const { t } = useI18n()
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
      setFormErr(err instanceof ApiError ? err.message : t('keys.requestFailed'))
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
    if (!confirm(t('keys.deleteConfirm', { name: k.name }))) return
    del.mutate(k.id)
  }

  return (
    <Layout>
      <main className="mx-auto w-full max-w-7xl px-6 py-8">
        <PageHeader
          eyebrow={t('keys.eyebrow')}
          context={t('keys.context')}
          title={t('keys.title')}
          description={t('keys.description')}
          action={
            <button onClick={openNew} className="btn btn-primary h-10">
              {t('keys.newKey')}
            </button>
          }
        />

        <MetricStrip
          metrics={[
            { label: t('keys.metricTotal'), value: keyCount },
            { label: t('common.enabled'), value: enabledCount },
            { label: t('keys.metricBudgeted'), value: budgetedCount },
            { label: t('keys.metricModelScoped'), value: modelScopedCount },
          ]}
        />

        <div className="mt-6" />

        {isLoading && <LoadingTable />}
        {error && (
          <ErrorNotice>
            {error instanceof ApiError ? error.message : t('keys.loadError')}
          </ErrorNotice>
        )}

        {keys && keys.length === 0 && (
          <EmptyState
            eyebrow={t('keys.emptyEyebrow')}
            title={t('keys.emptyTitle')}
            description={t('keys.emptyDescription')}
            action={
              <button onClick={openNew} className="btn btn-primary h-10">
                {t('keys.newKey')}
              </button>
            }
          />
        )}

        {keys && keys.length > 0 && (
          <div className="overflow-x-auto rounded-xl border border-line bg-surface">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
                <tr>
                  <Th>{t('common.name')}</Th>
                  <Th>{t('keys.colLabel')}</Th>
                  <Th>{t('common.status')}</Th>
                  <Th className="text-right">{t('keys.colDailyUsd')}</Th>
                  <Th className="text-right">{t('keys.colRpm')}</Th>
                  <Th>{t('keys.colModels')}</Th>
                  <Th className="text-right">{t('common.actions')}</Th>
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
                      {k.allowed_models.length > 0 ? k.allowed_models.join(', ') : t('keys.allModels')}
                    </Td>
                    <Td className="text-right">
                      <button
                        onClick={() => openEdit(k)}
                        className="mr-3 text-muted underline-offset-4 hover:text-ink hover:underline"
                      >
                        {t('common.edit')}
                      </button>
                      <button
                        onClick={() => handleDelete(k)}
                        disabled={del.isPending}
                        className="btn-danger hover:underline disabled:opacity-50"
                      >
                        {t('common.delete')}
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
              {del.error instanceof ApiError ? del.error.message : t('keys.deleteError')}
            </ErrorNotice>
          </div>
        )}
      </main>

      {editing && (
        <Modal title={editing === 'new' ? t('keys.newKey') : t('keys.editKey', { name: editing.name })} onClose={close}>
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
  const { t } = useI18n()
  const [copied, setCopied] = useState(false)
  function copy() {
    navigator.clipboard?.writeText(result.key).then(
      () => setCopied(true),
      () => setCopied(false),
    )
  }
  return (
    <Modal title={t('keys.createdTitle', { name: result.name })} onClose={onClose}>
      <p className="text-sm text-muted">
        {t('keys.revealHint')}
      </p>
      <div className="mt-3 flex items-center gap-2">
        <code className="flex-1 break-all rounded-lg border border-line bg-surface-2 px-3 py-2 font-mono text-sm">
          {result.key}
        </code>
        <button
          onClick={copy}
          className="btn btn-secondary shrink-0"
        >
          {copied ? t('keys.copied') : t('keys.copy')}
        </button>
      </div>
      <div className="mt-5 flex justify-end">
        <button
          onClick={onClose}
          className="btn btn-primary"
        >
          {t('keys.done')}
        </button>
      </div>
    </Modal>
  )
}
