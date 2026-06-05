import { useState } from 'react'
import { Layout } from '../components/Layout'
import { Modal } from '../components/Modal'
import { EmptyState, ErrorNotice, LoadingTable, MetricStrip, PageHeader } from '../components/PageChrome'
import { Td, Th } from '../components/Table'
import { ApiError } from '../lib/api'
import { useI18n } from '../lib/i18n'
import { useGenerateRedemptions, useRedemptions, type GenerateResult } from '../lib/redemptions'

export function RedemptionsPage() {
  const { t } = useI18n()
  const { data: batches, isLoading, error } = useRedemptions()
  const generate = useGenerateRedemptions()
  const [open, setOpen] = useState(false)
  const [result, setResult] = useState<GenerateResult | null>(null)
  const [formErr, setFormErr] = useState<string | null>(null)

  const totalCodes = batches?.reduce((s, b) => s + b.total, 0) ?? 0
  const redeemed = batches?.reduce((s, b) => s + b.redeemed, 0) ?? 0
  const outstanding = totalCodes - redeemed

  function submit(count: number, amount: number, days: number) {
    setFormErr(null)
    generate.mutate(
      { count, amount_usd: amount, expires_in_days: days || undefined },
      {
        onSuccess: (r) => {
          setResult(r)
          setOpen(false)
        },
        onError: (err) => setFormErr(err instanceof ApiError ? err.message : t('redemptions.requestFailed')),
      },
    )
  }

  return (
    <Layout>
      <main className="mx-auto w-full max-w-7xl px-6 py-8">
        <PageHeader
          eyebrow={t('redemptions.eyebrow')}
          context={t('redemptions.context')}
          title={t('redemptions.title')}
          description={t('redemptions.description')}
          action={
            <button onClick={() => { setFormErr(null); setOpen(true) }} className="btn btn-primary h-10">
              {t('redemptions.generate')}
            </button>
          }
        />

        {batches && batches.length > 0 && (
          <MetricStrip
            metrics={[
              { label: t('redemptions.metricCodes'), value: totalCodes },
              { label: t('redemptions.metricRedeemed'), value: redeemed },
              { label: t('redemptions.metricOutstanding'), value: outstanding },
            ]}
          />
        )}

        <div className="mt-6" />

        {isLoading && <LoadingTable columns={5} />}
        {error && <ErrorNotice>{error instanceof ApiError ? error.message : t('redemptions.loadError')}</ErrorNotice>}

        {batches && batches.length === 0 && (
          <EmptyState
            eyebrow={t('redemptions.emptyEyebrow')}
            title={t('redemptions.emptyTitle')}
            description={t('redemptions.emptyDescription')}
            action={
              <button onClick={() => { setFormErr(null); setOpen(true) }} className="btn btn-primary h-10">
                {t('redemptions.generate')}
              </button>
            }
          />
        )}

        {batches && batches.length > 0 && (
          <div className="overflow-x-auto rounded-xl border border-line bg-surface">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
                <tr>
                  <Th>{t('redemptions.colBatch')}</Th>
                  <Th className="text-right">{t('redemptions.colAmount')}</Th>
                  <Th className="text-right">{t('redemptions.colCount')}</Th>
                  <Th>{t('redemptions.colExpires')}</Th>
                  <Th>{t('redemptions.colCreated')}</Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {batches.map((b) => (
                  <tr key={b.batch_id} className="transition-colors hover:bg-surface-2">
                    <Td className="font-mono text-xs">{b.batch_id || '—'}</Td>
                    <Td className="text-right tabular-nums">${b.amount_usd.toFixed(2)}</Td>
                    <Td className="text-right tabular-nums">
                      {b.redeemed} / {b.total}
                    </Td>
                    <Td className="text-muted">{b.expires_at ? new Date(b.expires_at).toLocaleDateString() : '—'}</Td>
                    <Td className="text-muted">{new Date(b.created_at).toLocaleDateString()}</Td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </main>

      {open && (
        <Modal title={t('redemptions.generate')} onClose={() => setOpen(false)}>
          <GenerateForm submitting={generate.isPending} error={formErr} onCancel={() => setOpen(false)} onSubmit={submit} />
        </Modal>
      )}

      {result && (
        <Modal title={t('redemptions.codesTitle', { count: result.codes.length })} onClose={() => setResult(null)}>
          <div className="space-y-3">
            <p className="text-sm text-muted">{t('redemptions.codesHint')}</p>
            <textarea
              readOnly
              className="field h-48 w-full font-mono text-xs"
              value={result.codes.join('\n')}
              onFocus={(e) => e.currentTarget.select()}
            />
            <div className="flex justify-end gap-2">
              <button
                onClick={() => navigator.clipboard?.writeText(result.codes.join('\n'))}
                className="btn btn-secondary"
              >
                {t('redemptions.copyAll')}
              </button>
              <button onClick={() => setResult(null)} className="btn btn-primary">
                {t('common.done')}
              </button>
            </div>
          </div>
        </Modal>
      )}
    </Layout>
  )
}

function GenerateForm({
  submitting,
  error,
  onCancel,
  onSubmit,
}: {
  submitting: boolean
  error: string | null
  onCancel: () => void
  onSubmit: (count: number, amount: number, days: number) => void
}) {
  const { t } = useI18n()
  const [count, setCount] = useState('10')
  const [amount, setAmount] = useState('5')
  const [days, setDays] = useState('')
  const [localErr, setLocalErr] = useState<string | null>(null)

  function submit(e: React.FormEvent) {
    e.preventDefault()
    setLocalErr(null)
    const n = Number(count.trim())
    const a = Number(amount.trim())
    if (!Number.isInteger(n) || n < 1 || n > 1000) {
      setLocalErr(t('redemptions.countInvalid'))
      return
    }
    if (!Number.isFinite(a) || a <= 0) {
      setLocalErr(t('redemptions.amountInvalid'))
      return
    }
    onSubmit(n, a, Number(days.trim()) || 0)
  }

  return (
    <form onSubmit={submit} className="space-y-4">
      <label className="block space-y-1">
        <span className="text-sm text-muted">{t('redemptions.fieldCount')}</span>
        <input className="field" type="number" value={count} onChange={(e) => setCount(e.target.value)} autoFocus />
      </label>
      <label className="block space-y-1">
        <span className="text-sm text-muted">{t('redemptions.fieldAmount')}</span>
        <input className="field" type="number" step="0.01" value={amount} onChange={(e) => setAmount(e.target.value)} />
      </label>
      <label className="block space-y-1">
        <span className="text-sm text-muted">{t('redemptions.fieldExpires')}</span>
        <input className="field" type="number" value={days} onChange={(e) => setDays(e.target.value)} placeholder={t('redemptions.fieldExpiresPlaceholder')} />
      </label>
      {(localErr || error) && <p className="text-sm text-danger">{localErr ?? error}</p>}
      <div className="flex justify-end gap-2">
        <button type="button" onClick={onCancel} className="btn btn-secondary">
          {t('common.cancel')}
        </button>
        <button type="submit" disabled={submitting} className="btn btn-primary">
          {submitting ? t('common.saving') : t('redemptions.generate')}
        </button>
      </div>
    </form>
  )
}
