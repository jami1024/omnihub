import { useState } from 'react'
import { Layout } from '../components/Layout'
import { Modal } from '../components/Modal'
import { EmptyState, ErrorNotice, LoadingTable, MetricStrip, PageHeader } from '../components/PageChrome'
import { Td, Th } from '../components/Table'
import { BlockedIPForm } from '../components/BlockedIPForm'
import { ApiError } from '../lib/api'
import { useI18n } from '../lib/i18n'
import {
  useBlockedIPs,
  useCreateBlockedIP,
  useDeleteBlockedIP,
  useUpdateBlockedIP,
  type BlockedIP,
  type BlockedIPInput,
} from '../lib/blockedIps'

type Editing = 'new' | BlockedIP | null

export function BlockedIPsPage() {
  const { t } = useI18n()
  const { data: rows, isLoading, error } = useBlockedIPs()
  const create = useCreateBlockedIP()
  const update = useUpdateBlockedIP()
  const del = useDeleteBlockedIP()
  const [editing, setEditing] = useState<Editing>(null)
  const [formErr, setFormErr] = useState<string | null>(null)
  const rowCount = rows?.length ?? 0
  const hardBlockCount = rows?.filter((r) => r.blocked).length ?? 0
  const rateCapCount = rows?.filter((r) => !r.blocked).length ?? 0
  const concurrentCount = rows?.filter((r) => r.concurrent_limit != null).length ?? 0

  function openNew() {
    setFormErr(null)
    setEditing('new')
  }

  function close() {
    setEditing(null)
    setFormErr(null)
  }

  function handleSubmit(input: BlockedIPInput) {
    setFormErr(null)
    const onError = (err: unknown) =>
      setFormErr(err instanceof ApiError ? err.message : t('blockedIps.requestFailed'))
    if (editing === 'new') {
      create.mutate(input, { onSuccess: close, onError })
    } else if (editing) {
      update.mutate({ ip: editing.ip, input }, { onSuccess: close, onError })
    }
  }

  function handleDelete(row: BlockedIP) {
    if (!confirm(t('blockedIps.unblockConfirm', { ip: row.ip }))) return
    del.mutate(row.ip)
  }

  return (
    <Layout>
      <main className="mx-auto w-full max-w-7xl px-6 py-8">
        <PageHeader
          eyebrow={t('blockedIps.eyebrow')}
          context={t('blockedIps.context')}
          title={t('blockedIps.title')}
          description={t('blockedIps.description')}
          action={
            <button onClick={openNew} className="btn btn-primary h-10">
              {t('blockedIps.blockAnIp')}
            </button>
          }
        />

        <MetricStrip
          metrics={[
            { label: t('blockedIps.total'), value: rowCount },
            { label: t('blockedIps.hardBlocks'), value: hardBlockCount, tone: hardBlockCount > 0 ? 'danger' : undefined },
            { label: t('blockedIps.rateCaps'), value: rateCapCount },
            { label: t('blockedIps.concurrentCaps'), value: concurrentCount },
          ]}
        />

        <div className="mt-6" />

        {isLoading && <LoadingTable />}
        {error && (
          <ErrorNotice>
            {error instanceof ApiError ? error.message : t('blockedIps.loadError')}
          </ErrorNotice>
        )}

        {rows && rows.length === 0 && (
          <EmptyState
            eyebrow={t('blockedIps.emptyEyebrow')}
            title={t('blockedIps.emptyTitle')}
            description={t('blockedIps.emptyDescription')}
            action={
              <button onClick={openNew} className="btn btn-primary h-10">
                {t('blockedIps.blockAnIp')}
              </button>
            }
          />
        )}

        {rows && rows.length > 0 && (
          <div className="overflow-x-auto rounded-xl border border-line bg-surface">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
                <tr>
                  <Th>{t('blockedIps.colIp')}</Th>
                  <Th>{t('blockedIps.colPolicy')}</Th>
                  <Th className="text-right">{t('blockedIps.colRpm')}</Th>
                  <Th className="text-right">{t('blockedIps.colTpm')}</Th>
                  <Th className="text-right">{t('blockedIps.colConcurrent')}</Th>
                  <Th>{t('blockedIps.colReason')}</Th>
                  <Th>{t('blockedIps.colAddedBy')}</Th>
                  <Th className="text-right">{t('common.actions')}</Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {rows.map((row) => (
                  <tr key={row.ip} className="transition-colors hover:bg-surface-2">
                    <Td className="font-mono text-xs">{row.ip}</Td>
                    <Td>
                      <PolicyBadge blocked={row.blocked} />
                    </Td>
                    <Td className="text-right tabular-nums">{row.rpm_limit ?? '—'}</Td>
                    <Td className="text-right tabular-nums">{row.tpm_limit ?? '—'}</Td>
                    <Td className="text-right tabular-nums">{row.concurrent_limit ?? '—'}</Td>
                    <Td className="text-muted">{row.reason || '—'}</Td>
                    <Td className="text-muted">{row.created_by || '—'}</Td>
                    <Td className="text-right">
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
                        {t('blockedIps.unblock')}
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
              {del.error instanceof ApiError ? del.error.message : t('blockedIps.unblockFailed')}
            </ErrorNotice>
          </div>
        )}
      </main>

      {editing && (
        <Modal title={editing === 'new' ? t('blockedIps.blockAnIp') : t('blockedIps.editTitle', { ip: editing.ip })} onClose={close}>
          <BlockedIPForm
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

function PolicyBadge({ blocked }: { blocked: boolean }) {
  const { t } = useI18n()
  return blocked ? (
    <span className="badge badge-danger">
      {t('blockedIps.policyHardBlock')}
    </span>
  ) : (
    <span className="badge badge-warning">
      {t('blockedIps.policyRateCap')}
    </span>
  )
}
