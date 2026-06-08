import { useState } from 'react'
import { Modal } from '../components/Modal'
import { EmptyState, ErrorNotice, LoadingTable, MetricStrip, PageHeader } from '../components/PageChrome'
import { SettingsLayout } from '../components/SettingsLayout'
import { StatusBadge, Td, Th } from '../components/Table'
import { ApiError } from '../lib/api'
import { useI18n } from '../lib/i18n'
import { useGrantPlanToUser, usePlans, useSavePlan, type Plan, type PlanInput } from '../lib/plans'

type Editing = 'new' | Plan | null

export function PlansPage() {
  const { t } = useI18n()
  const { data, isLoading, error } = usePlans()
  const save = useSavePlan()
  const grant = useGrantPlanToUser()
  const [editing, setEditing] = useState<Editing>(null)

  const enabled = data?.filter((p) => p.enabled).length ?? 0
  const free = data?.filter((p) => p.price_usd === 0).length ?? 0
  const paid = data?.filter((p) => p.price_usd > 0).length ?? 0

  function grantToUser(p: Plan) {
    const raw = prompt(t('plans.grantPrompt', { plan: p.name }))
    if (!raw) return
    const userId = Number(raw)
    if (!Number.isInteger(userId) || userId <= 0) {
      alert(t('plans.grantInvalid'))
      return
    }
    grant.mutate(
      { userId, planId: p.id },
      { onSuccess: (r) => alert(t('plans.grantSuccess', { id: r.id })) },
    )
  }

  return (
    <SettingsLayout>
      <PageHeader
        eyebrow={t('plans.eyebrow')}
        context={t('plans.context')}
        title={t('plans.title')}
        description={t('plans.description')}
        action={
          <button className="btn btn-primary h-10" onClick={() => setEditing('new')}>
            {t('plans.add')}
          </button>
        }
      />

      <MetricStrip
        metrics={[
          { label: t('plans.metricTotal'), value: data?.length ?? 0 },
          { label: t('common.enabled'), value: enabled },
          { label: t('plans.metricFree'), value: free },
          { label: t('plans.metricPaid'), value: paid },
        ]}
      />

      <div className="mt-6" />
      {isLoading && <LoadingTable columns={7} />}
      {error && <ErrorNotice>{error instanceof ApiError ? error.message : t('plans.loadError')}</ErrorNotice>}

      {data && data.length === 0 && (
        <EmptyState
          eyebrow={t('plans.emptyEyebrow')}
          title={t('plans.emptyTitle')}
          description={t('plans.emptyDescription')}
          action={
            <button className="btn btn-primary" onClick={() => setEditing('new')}>
              {t('plans.add')}
            </button>
          }
        />
      )}

      {data && data.length > 0 && (
        <div className="overflow-x-auto rounded-xl border border-line bg-surface">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
              <tr>
                <Th>{t('plans.colName')}</Th>
                <Th className="text-right">{t('plans.colPrice')}</Th>
                <Th className="text-right">{t('plans.colCredit')}</Th>
                <Th className="text-right">{t('plans.colValid')}</Th>
                <Th>{t('plans.colOverage')}</Th>
                <Th>{t('common.status')}</Th>
                <Th className="text-right">{t('common.actions')}</Th>
              </tr>
            </thead>
            <tbody className="divide-y divide-line">
              {data.map((p) => (
                <tr key={p.id} className="hover:bg-surface-2">
                  <Td>
                    <div className="font-medium">{p.name}</div>
                    <div className="mt-1 max-w-md truncate text-xs text-muted">{p.description || '—'}</div>
                  </Td>
                  <Td className="text-right tabular-nums">{cny(p.price_usd)}</Td>
                  <Td className="text-right tabular-nums">{usd(p.included_credit_usd)}</Td>
                  <Td className="text-right tabular-nums">{p.valid_days ?? '—'}</Td>
                  <Td>{p.allow_payg_overage ? t('plans.overageYes') : t('plans.overageNo')}</Td>
                  <Td><StatusBadge enabled={p.enabled} /></Td>
                  <Td className="text-right">
                    <button
                      className="mr-3 inline-flex min-h-10 items-center rounded-md px-2 text-muted hover:bg-surface-2 hover:text-ink"
                      onClick={() => grantToUser(p)}
                    >
                      {t('plans.grant')}
                    </button>
                    <button
                      className="inline-flex min-h-10 items-center rounded-md px-2 text-muted hover:bg-surface-2 hover:text-ink"
                      onClick={() => setEditing(p)}
                    >
                      {t('common.edit')}
                    </button>
                  </Td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {(save.error || grant.error) && (
        <div className="mt-3">
          <ErrorNotice>{((save.error || grant.error) as ApiError)?.message ?? t('common.actionFailed')}</ErrorNotice>
        </div>
      )}

      {editing && (
        <Modal title={editing === 'new' ? t('plans.add') : t('plans.edit')} onClose={() => setEditing(null)}>
          <PlanForm
            row={editing === 'new' ? undefined : editing}
            submitting={save.isPending}
            onCancel={() => setEditing(null)}
            onSubmit={(input) => save.mutate(input, { onSuccess: () => setEditing(null) })}
          />
        </Modal>
      )}
    </SettingsLayout>
  )
}

function PlanForm({
  row,
  submitting,
  onCancel,
  onSubmit,
}: {
  row?: Plan
  submitting: boolean
  onCancel: () => void
  onSubmit: (input: PlanInput) => void
}) {
  const { t } = useI18n()
  const [name, setName] = useState(row?.name ?? '')
  const [description, setDescription] = useState(row?.description ?? '')
  const [price, setPrice] = useState(String(row?.price_usd ?? 0))
  const [credit, setCredit] = useState(String(row?.included_credit_usd ?? 0))
  const [valid, setValid] = useState(row?.valid_days ? String(row.valid_days) : '')
  const [ratio, setRatio] = useState(String(row?.price_ratio ?? 1))
  const [models, setModels] = useState(row?.allowed_models?.join(', ') ?? '')
  const [overage, setOverage] = useState(row?.allow_payg_overage ?? true)
  const [enabled, setEnabled] = useState(row?.enabled ?? true)

  function submit(e: React.FormEvent) {
    e.preventDefault()
    onSubmit({
      id: row?.id,
      name: name.trim(),
      description: description.trim(),
      price_usd: num(price),
      included_credit_usd: num(credit),
      valid_days: valid.trim() ? Math.max(1, Math.floor(num(valid))) : null,
      rpm_limit: row?.rpm_limit ?? null,
      daily_usd_limit: row?.daily_usd_limit ?? null,
      allowed_models: models.split(',').map((m) => m.trim()).filter(Boolean),
      price_ratio: num(ratio),
      allow_payg_overage: overage,
      enabled,
      sort_order: row?.sort_order ?? 0,
    })
  }

  return (
    <form className="space-y-4" onSubmit={submit}>
      <Field label={t('common.name')}>
        <input className="field" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
      </Field>
      <Field label={t('plans.fieldDescription')}>
        <textarea className="field min-h-24" value={description} onChange={(e) => setDescription(e.target.value)} />
      </Field>
      <div className="grid gap-3 sm:grid-cols-3">
        <Field label={t('plans.fieldPrice')}>
          <input className="field" type="number" step="0.0001" value={price} onChange={(e) => setPrice(e.target.value)} />
        </Field>
        <Field label={t('plans.fieldCredit')}>
          <input className="field" type="number" step="0.0001" value={credit} onChange={(e) => setCredit(e.target.value)} />
        </Field>
        <Field label={t('plans.fieldValid')}>
          <input className="field" type="number" placeholder={t('common.none')} value={valid} onChange={(e) => setValid(e.target.value)} />
        </Field>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label={t('plans.fieldRatio')}>
          <input className="field" type="number" step="0.01" value={ratio} onChange={(e) => setRatio(e.target.value)} />
        </Field>
        <Field label={t('plans.fieldModels')}>
          <input className="field" value={models} onChange={(e) => setModels(e.target.value)} placeholder="claude-sonnet-4-5, gpt-4o" />
        </Field>
      </div>
      <div className="flex flex-wrap gap-4 text-sm text-muted">
        <label className="flex items-center gap-2">
          <input type="checkbox" checked={overage} onChange={(e) => setOverage(e.target.checked)} />
          {t('plans.fieldOverage')}
        </label>
        <label className="flex items-center gap-2">
          <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
          {t('common.enabled')}
        </label>
      </div>
      <div className="flex justify-end gap-2">
        <button type="button" className="btn btn-secondary" onClick={onCancel}>{t('common.cancel')}</button>
        <button className="btn btn-primary" disabled={submitting}>{submitting ? t('common.saving') : t('common.save')}</button>
      </div>
    </form>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return <label className="block space-y-1"><span className="text-sm text-muted">{label}</span>{children}</label>
}
function num(v: string) {
  const n = Number(v)
  return Number.isFinite(n) ? n : 0
}
function usd(n: number) {
  return `$${n.toFixed(4)}`
}
function cny(n: number) {
  return new Intl.NumberFormat('zh-CN', {
    style: 'currency',
    currency: 'CNY',
    minimumFractionDigits: Number.isInteger(n) ? 0 : 2,
    maximumFractionDigits: 2,
  }).format(n)
}
