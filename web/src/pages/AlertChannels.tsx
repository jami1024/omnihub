import { useState } from 'react'
import { Layout } from '../components/Layout'
import { Modal } from '../components/Modal'
import { EmptyState, ErrorNotice, LoadingTable, MetricStrip, PageHeader } from '../components/PageChrome'
import { Td, Th } from '../components/Table'
import { ApiError } from '../lib/api'
import { useI18n } from '../lib/i18n'
import {
  useAlertChannels,
  useCreateAlertChannel,
  useDeleteAlertChannel,
  useTestAlertChannel,
  useUpdateAlertChannel,
  type AlertChannel,
  type AlertChannelInput,
  type AlertChannelType,
} from '../lib/alertChannels'

type Editing = 'new' | AlertChannel | null

const TYPES: AlertChannelType[] = ['webhook', 'feishu', 'dingtalk']

export function AlertChannelsPage() {
  const { t } = useI18n()
  const { data: rows, isLoading, error } = useAlertChannels()
  const create = useCreateAlertChannel()
  const update = useUpdateAlertChannel()
  const del = useDeleteAlertChannel()
  const test = useTestAlertChannel()
  const [editing, setEditing] = useState<Editing>(null)
  const [formErr, setFormErr] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)

  const rowCount = rows?.length ?? 0
  const enabledCount = rows?.filter((r) => r.enabled).length ?? 0

  function close() {
    setEditing(null)
    setFormErr(null)
  }

  function handleSubmit(input: AlertChannelInput) {
    setFormErr(null)
    const onError = (err: unknown) =>
      setFormErr(err instanceof ApiError ? err.message : t('alertChannels.requestFailed'))
    if (editing === 'new') {
      create.mutate(input, { onSuccess: close, onError })
    } else if (editing) {
      update.mutate({ id: editing.id, input }, { onSuccess: close, onError })
    }
  }

  function handleDelete(row: AlertChannel) {
    if (!confirm(t('alertChannels.deleteConfirm', { name: row.name }))) return
    del.mutate(row.id)
  }

  function handleTest(row: AlertChannel) {
    setNotice(null)
    test.mutate(row.id, {
      onSuccess: () => setNotice(t('alertChannels.testSent', { name: row.name })),
      onError: (err) =>
        setNotice(err instanceof ApiError ? err.message : t('alertChannels.testFailed')),
    })
  }

  return (
    <Layout>
      <main className="mx-auto w-full max-w-7xl px-6 py-8">
        <PageHeader
          eyebrow={t('alertChannels.eyebrow')}
          context={t('alertChannels.context')}
          title={t('alertChannels.title')}
          description={t('alertChannels.description')}
          action={
            <button
              onClick={() => {
                setFormErr(null)
                setEditing('new')
              }}
              className="btn btn-primary h-10"
            >
              {t('alertChannels.addChannel')}
            </button>
          }
        />

        <MetricStrip
          metrics={[
            { label: t('alertChannels.total'), value: rowCount },
            { label: t('alertChannels.enabled'), value: enabledCount },
          ]}
        />

        <div className="mt-6" />

        {isLoading && <LoadingTable />}
        {error && (
          <ErrorNotice>
            {error instanceof ApiError ? error.message : t('alertChannels.loadError')}
          </ErrorNotice>
        )}

        {notice && (
          <div className="mb-4 rounded-md border border-line bg-surface-2 px-4 py-2 text-sm text-muted">
            {notice}
          </div>
        )}

        {rows && rows.length === 0 && (
          <EmptyState
            eyebrow={t('alertChannels.emptyEyebrow')}
            title={t('alertChannels.emptyTitle')}
            description={t('alertChannels.emptyDescription')}
            action={
              <button
                onClick={() => {
                  setFormErr(null)
                  setEditing('new')
                }}
                className="btn btn-primary h-10"
              >
                {t('alertChannels.addChannel')}
              </button>
            }
          />
        )}

        {rows && rows.length > 0 && (
          <div className="overflow-x-auto rounded-xl border border-line bg-surface">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
                <tr>
                  <Th>{t('alertChannels.colName')}</Th>
                  <Th>{t('alertChannels.colType')}</Th>
                  <Th>{t('alertChannels.colStatus')}</Th>
                  <Th>{t('alertChannels.colAddedBy')}</Th>
                  <Th className="text-right">{t('common.actions')}</Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {rows.map((row) => (
                  <tr key={row.id} className="transition-colors hover:bg-surface-2">
                    <Td className="font-medium">{row.name}</Td>
                    <Td className="text-muted">{row.type}</Td>
                    <Td>
                      <span className={row.enabled ? 'badge badge-success' : 'badge'}>
                        {row.enabled ? t('alertChannels.statusEnabled') : t('alertChannels.statusDisabled')}
                      </span>
                    </Td>
                    <Td className="text-muted">{row.created_by || '—'}</Td>
                    <Td className="text-right">
                      <button
                        onClick={() => handleTest(row)}
                        disabled={test.isPending}
                        className="mr-1 inline-flex min-h-10 items-center rounded-md px-2 text-muted underline-offset-4 hover:bg-surface-2 hover:text-ink hover:underline disabled:opacity-50 sm:mr-3 sm:px-1"
                      >
                        {t('alertChannels.test')}
                      </button>
                      <button
                        onClick={() => {
                          setFormErr(null)
                          setEditing(row)
                        }}
                        className="mr-1 inline-flex min-h-10 items-center rounded-md px-2 text-muted underline-offset-4 hover:bg-surface-2 hover:text-ink hover:underline sm:mr-3 sm:px-1"
                      >
                        {t('common.edit')}
                      </button>
                      <button
                        onClick={() => handleDelete(row)}
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
        )}

        {del.error && (
          <div className="mt-3">
            <ErrorNotice>
              {del.error instanceof ApiError ? del.error.message : t('alertChannels.deleteFailed')}
            </ErrorNotice>
          </div>
        )}
      </main>

      {editing && (
        <Modal
          title={editing === 'new' ? t('alertChannels.addChannel') : t('alertChannels.editTitle', { name: editing.name })}
          onClose={close}
        >
          <AlertChannelForm
            entry={editing === 'new' ? undefined : editing}
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

interface FormProps {
  entry?: AlertChannel
  submitting: boolean
  error?: string | null
  onCancel: () => void
  onSubmit: (input: AlertChannelInput) => void
}

function AlertChannelForm({ entry, submitting, error, onCancel, onSubmit }: FormProps) {
  const { t } = useI18n()
  const isEdit = entry != null
  const [type, setType] = useState<AlertChannelType>(entry?.type ?? 'webhook')
  const [name, setName] = useState(entry?.name ?? '')
  const [url, setUrl] = useState('')
  const [enabled, setEnabled] = useState(entry?.enabled ?? true)
  const [localErr, setLocalErr] = useState<string | null>(null)

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setLocalErr(null)
    if (!name.trim()) {
      setLocalErr(t('alertChannelForm.nameRequired'))
      return
    }
    if (!isEdit && !url.trim()) {
      setLocalErr(t('alertChannelForm.urlRequired'))
      return
    }
    if (url.trim() && !/^https?:\/\//.test(url.trim())) {
      setLocalErr(t('alertChannelForm.urlInvalid'))
      return
    }
    onSubmit({ type, name: name.trim(), url: url.trim(), enabled })
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <label className="block space-y-1">
        <span className="text-sm text-muted">{t('alertChannelForm.type')}</span>
        <select className="field" value={type} onChange={(e) => setType(e.target.value as AlertChannelType)}>
          {TYPES.map((tp) => (
            <option key={tp} value={tp}>
              {tp}
            </option>
          ))}
        </select>
      </label>

      <label className="block space-y-1">
        <span className="text-sm text-muted">{t('alertChannelForm.name')}</span>
        <input
          className="field"
          value={name}
          onChange={(e) => setName(e.target.value)}
          autoFocus
          placeholder={t('alertChannelForm.namePlaceholder')}
        />
      </label>

      <label className="block space-y-1">
        <span className="text-sm text-muted">{t('alertChannelForm.url')}</span>
        <input
          className="field"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder={isEdit ? t('alertChannelForm.urlKeep') : 'https://...'}
        />
        {isEdit && <span className="text-xs text-muted">{t('alertChannelForm.urlKeepHint')}</span>}
      </label>

      <label className="flex items-center gap-2 text-sm">
        <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
        <span>{t('alertChannelForm.enabled')}</span>
      </label>

      {(localErr || error) && <p className="text-sm text-danger">{localErr ?? error}</p>}

      <div className="flex justify-end gap-2">
        <button type="button" onClick={onCancel} className="btn btn-secondary">
          {t('common.cancel')}
        </button>
        <button type="submit" disabled={submitting} className="btn btn-primary">
          {submitting ? t('common.saving') : isEdit ? t('common.saveChanges') : t('alertChannels.addChannel')}
        </button>
      </div>
    </form>
  )
}
