import { useState } from 'react'
import { PortalLayout } from '../../components/PortalLayout'
import { Modal } from '../../components/Modal'
import { StatusBadge, Td, Th } from '../../components/Table'
import { ApiError } from '../../lib/portalApi'
import { useI18n } from '../../lib/i18n'
import {
  useCreatePortalKey,
  useDeletePortalKey,
  usePortalKeys,
  type BillingMode,
  type CreatePortalKeyResult,
  type PortalKey,
} from '../../lib/portalData'

export function PortalKeysPage() {
  const { t } = useI18n()
  const { data: keys, isLoading, error } = usePortalKeys()
  const create = useCreatePortalKey()
  const del = useDeletePortalKey()
  const [showForm, setShowForm] = useState(false)
  const [revealed, setRevealed] = useState<CreatePortalKeyResult | null>(null)

  function handleDelete(k: PortalKey) {
    if (!confirm(t('portalKeys.deleteConfirm', { name: k.name }))) return
    del.mutate(k.id)
  }

  return (
    <PortalLayout>
      <main className="mx-auto max-w-5xl px-6 py-8">
        <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h2 className="text-xl font-semibold">{t('portalKeys.title')}</h2>
            <p className="text-sm text-muted">{t('portalKeys.subtitle')}</p>
          </div>
          <button onClick={() => setShowForm(true)} className="btn btn-primary min-h-10">
            {t('portalKeys.newKey')}
          </button>
        </div>

        {isLoading && <p className="text-sm text-muted">{t('common.loading')}</p>}
        {error && (
          <p className="text-sm text-danger">
            {error instanceof ApiError ? error.message : t('portalKeys.loadFailed')}
          </p>
        )}

        {keys && keys.length === 0 && (
          <div className="rounded-xl border border-dashed border-line-strong p-10 text-center text-sm text-muted">
            {t('portalKeys.emptyState')}
          </div>
        )}

        {keys && keys.length > 0 && (
          <div className="overflow-x-auto rounded-xl border border-line bg-surface">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
                <tr>
                  <Th>{t('common.name')}</Th>
                  <Th>{t('common.status')}</Th>
                  <Th className="text-right">{t('portalKeys.dailyUsd')}</Th>
                  <Th className="text-right">{t('portalKeys.rpm')}</Th>
                  <Th className="text-right">{t('portalKeys.spent24h')}</Th>
                  <Th className="text-right">{t('common.actions')}</Th>
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
  onCreate: (input: { name: string; daily_usd_limit: number | null; rpm_limit: number | null; allowed_models: string[]; billing_mode: BillingMode }) => void
}) {
  const { t } = useI18n()
  const [name, setName] = useState('')
  const [daily, setDaily] = useState('')
  const [rpm, setRpm] = useState('')
  const [billingMode, setBillingMode] = useState<BillingMode>('payg')
  const [localErr, setLocalErr] = useState<string | null>(null)

  function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!name.trim()) {
      setLocalErr(t('portalKeys.nameRequired'))
      return
    }
    onCreate({
      name: name.trim(),
      daily_usd_limit: daily.trim() ? Number(daily) : null,
      rpm_limit: rpm.trim() ? Number(rpm) : null,
      allowed_models: [],
      billing_mode: billingMode,
    })
  }

  return (
    <Modal title={t('portalKeys.newKeyTitle')} onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        <label className="block space-y-1">
          <span className="text-sm text-muted">{t('common.name')}</span>
          <input className="field" value={name} onChange={(e) => setName(e.target.value)} autoFocus placeholder="my-app" />
        </label>
        <div className="grid grid-cols-2 gap-4">
          <label className="block space-y-1">
            <span className="text-sm text-muted">{t('portalKeys.dailyUsdLimit')}</span>
            <input className="field" type="number" step="0.01" value={daily} onChange={(e) => setDaily(e.target.value)} placeholder={t('portalKeys.noLimit')} />
          </label>
          <label className="block space-y-1">
            <span className="text-sm text-muted">{t('portalKeys.rpmLimit')}</span>
            <input className="field" type="number" value={rpm} onChange={(e) => setRpm(e.target.value)} placeholder={t('portalKeys.noLimit')} />
          </label>
        </div>
        <label className="block space-y-1">
          <span className="text-sm text-muted">{t('portalKeys.billingMode')}</span>
          <select className="field" value={billingMode} onChange={(e) => setBillingMode(e.target.value as BillingMode)}>
            <option value="payg">{t('portalKeys.billingModePayg')}</option>
            <option value="plan">{t('portalKeys.billingModePlan')}</option>
          </select>
          <span className="text-xs text-muted">{t('portalKeys.billingModeHint')}</span>
        </label>
        {(localErr || error) && <p className="text-sm text-danger">{localErr ?? error}</p>}
        <div className="flex justify-end gap-2">
          <button type="button" onClick={onClose} className="btn btn-secondary">
            {t('common.cancel')}
          </button>
          <button type="submit" disabled={submitting} className="btn btn-primary min-h-10">
            {submitting ? t('portalKeys.creating') : t('portalKeys.createKey')}
          </button>
        </div>
      </form>
    </Modal>
  )
}

function RevealKey({ result, onClose }: { result: CreatePortalKeyResult; onClose: () => void }) {
  const { t } = useI18n()
  const [copied, setCopied] = useState(false)
  return (
    <Modal title={t('portalKeys.createdTitle', { name: result.name })} onClose={onClose}>
      <p className="text-sm text-muted">{t('portalKeys.copyNotice')}</p>
      <div className="mt-3 flex items-center gap-2">
        <code className="flex-1 break-all rounded-lg border border-line bg-surface-2 px-3 py-2 font-mono text-sm">
          {result.key}
        </code>
        <button
          onClick={() => navigator.clipboard?.writeText(result.key).then(() => setCopied(true))}
          className="btn btn-secondary shrink-0"
        >
          {copied ? t('portalKeys.copied') : t('portalKeys.copy')}
        </button>
      </div>
      <div className="mt-5 flex justify-end">
        <button onClick={onClose} className="btn btn-primary min-h-10">
          {t('portalKeys.done')}
        </button>
      </div>
    </Modal>
  )
}
