import { useState } from 'react'
import { Layout } from '../components/Layout'
import { Modal } from '../components/Modal'
import { EmptyState, ErrorNotice, LoadingTable, MetricStrip, PageHeader } from '../components/PageChrome'
import { StatusBadge, Td, Th } from '../components/Table'
import { ApiError } from '../lib/api'
import { useI18n } from '../lib/i18n'
import { useDeleteUser, useRechargeUser, useSetUserEnabled, useSetUserRatio, useUsers, type AdminUser } from '../lib/users'
import { useUserPlanGrants } from '../lib/plans'

export function UsersPage() {
  const { t } = useI18n()
  const { data: users, isLoading, error } = useUsers()
  const setEnabled = useSetUserEnabled()
  const setRatio = useSetUserRatio()
  const del = useDeleteUser()
  const recharge = useRechargeUser()
  const [recharging, setRecharging] = useState<AdminUser | null>(null)
  const [viewingGrants, setViewingGrants] = useState<AdminUser | null>(null)

  function editRatio(u: AdminUser) {
    const raw = window.prompt(t('users.ratioPrompt', { name: u.username }), String(u.price_ratio))
    if (raw == null) return
    const n = Number(raw.trim())
    if (!Number.isFinite(n) || n < 0) {
      alert(t('users.ratioInvalid'))
      return
    }
    setRatio.mutate({ id: u.id, price_ratio: n })
  }

  const total = users?.length ?? 0
  const enabled = users?.filter((u) => u.enabled).length ?? 0
  const keyCount = users?.reduce((s, u) => s + u.key_count, 0) ?? 0
  const spend = users?.reduce((s, u) => s + u.spend_30d, 0) ?? 0

  function toggle(u: AdminUser) {
    setEnabled.mutate({ id: u.id, enabled: !u.enabled })
  }
  function remove(u: AdminUser) {
    if (!confirm(t('users.deleteConfirm', { name: u.username }))) return
    del.mutate(u.id)
  }

  return (
    <Layout>
      <main className="mx-auto w-full max-w-7xl px-6 py-8">
        <PageHeader
          eyebrow={t('users.eyebrow')}
          context={t('users.context')}
          title={t('users.title')}
          description={t('users.description')}
        />

        {users && users.length > 0 && (
          <MetricStrip
            metrics={[
              { label: t('users.metricUsers'), value: total },
              { label: t('common.enabled'), value: enabled },
              { label: t('users.metricKeys'), value: keyCount },
              { label: t('users.metricSpend30d'), value: `$${spend.toFixed(2)}` },
            ]}
          />
        )}

        <div className="mt-6" />

        {isLoading && <LoadingTable columns={8} />}
        {error && <ErrorNotice>{error instanceof ApiError ? error.message : t('users.loadError')}</ErrorNotice>}

        {users && users.length === 0 && (
          <EmptyState
            eyebrow={t('users.emptyEyebrow')}
            title={t('users.emptyTitle')}
            description={t('users.emptyDescription')}
          />
        )}

        {users && users.length > 0 && (
          <div className="overflow-x-auto rounded-xl border border-line bg-surface">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
                <tr>
                  <Th>{t('users.colUsername')}</Th>
                  <Th>{t('common.email')}</Th>
                  <Th className="text-right">{t('users.colKeys')}</Th>
                  <Th className="text-right">{t('users.colSpent30d')}</Th>
                  <Th className="text-right">{t('users.colRatio')}</Th>
                  <Th>{t('common.status')}</Th>
                  <Th>{t('users.colJoined')}</Th>
                  <Th className="text-right">{t('common.actions')}</Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {users.map((u) => (
                  <tr key={u.id} className="transition-colors hover:bg-surface-2">
                    <Td className="font-medium">{u.username}</Td>
                    <Td className="text-muted">{u.email || '—'}</Td>
                    <Td className="text-right tabular-nums">{u.key_count}</Td>
                    <Td className="text-right tabular-nums">${u.spend_30d.toFixed(4)}</Td>
                    <Td className="text-right tabular-nums">{u.price_ratio.toFixed(2)}×</Td>
                    <Td>
                      <StatusBadge enabled={u.enabled} />
                    </Td>
                    <Td className="text-muted">{new Date(u.created_at).toLocaleDateString()}</Td>
                    <Td className="text-right">
                      <button
                        onClick={() => setRecharging(u)}
                        className="mr-1 inline-flex min-h-10 items-center rounded-md px-2 text-muted underline-offset-4 hover:bg-surface-2 hover:text-ink hover:underline sm:mr-3 sm:px-1"
                      >
                        {t('users.recharge')}
                      </button>
                      <button
                        onClick={() => setViewingGrants(u)}
                        className="mr-1 inline-flex min-h-10 items-center rounded-md px-2 text-muted underline-offset-4 hover:bg-surface-2 hover:text-ink hover:underline sm:mr-3 sm:px-1"
                      >
                        {t('users.plans')}
                      </button>
                      <button
                        onClick={() => editRatio(u)}
                        disabled={setRatio.isPending}
                        className="mr-1 inline-flex min-h-10 items-center rounded-md px-2 text-muted underline-offset-4 hover:bg-surface-2 hover:text-ink hover:underline disabled:opacity-50 sm:mr-3 sm:px-1"
                      >
                        {t('users.ratio')}
                      </button>
                      <button
                        onClick={() => toggle(u)}
                        disabled={setEnabled.isPending}
                        className="mr-1 inline-flex min-h-10 items-center rounded-md px-2 text-muted underline-offset-4 hover:bg-surface-2 hover:text-ink hover:underline disabled:opacity-50 sm:mr-3 sm:px-1"
                      >
                        {u.enabled ? t('common.disable') : t('common.enable')}
                      </button>
                      <button
                        onClick={() => remove(u)}
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

        {(setEnabled.error || del.error) && (
          <div className="mt-3">
            <ErrorNotice>
              {(setEnabled.error || del.error) instanceof ApiError
                ? ((setEnabled.error || del.error) as ApiError).message
                : t('common.actionFailed')}
            </ErrorNotice>
          </div>
        )}
      </main>

      {recharging && (
        <Modal title={t('users.rechargeTitle', { name: recharging.username })} onClose={() => setRecharging(null)}>
          <RechargeForm
            submitting={recharge.isPending}
            error={recharge.error instanceof ApiError ? recharge.error.message : null}
            onCancel={() => setRecharging(null)}
            onSubmit={(amount, note) =>
              recharge.mutate(
                { id: recharging.id, amount_usd: amount, note },
                { onSuccess: () => setRecharging(null) },
              )
            }
          />
        </Modal>
      )}

      {viewingGrants && (
        <Modal title={t('users.grantsTitle', { name: viewingGrants.username })} onClose={() => setViewingGrants(null)}>
          <GrantsList userId={viewingGrants.id} />
        </Modal>
      )}
    </Layout>
  )
}

function GrantsList({ userId }: { userId: number }) {
  const { t } = useI18n()
  const { data: grants, isLoading, error } = useUserPlanGrants(userId)

  if (isLoading) return <p className="text-sm text-muted">{t('common.loading')}</p>
  if (error) return <ErrorNotice>{error instanceof ApiError ? error.message : t('common.actionFailed')}</ErrorNotice>
  if (!grants || grants.length === 0) return <p className="text-sm text-muted">{t('users.grantsEmpty')}</p>

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-left text-sm">
        <thead className="border-b border-line text-xs uppercase tracking-wide text-muted">
          <tr>
            <Th>{t('users.grantsColPlan')}</Th>
            <Th className="text-right">{t('users.grantsColRemaining')}</Th>
            <Th>{t('users.grantsColExpires')}</Th>
            <Th>{t('common.status')}</Th>
          </tr>
        </thead>
        <tbody className="divide-y divide-line">
          {grants.map((g) => (
            <tr key={g.id}>
              <Td className="font-medium">{g.plan_name_snapshot}</Td>
              <Td className="text-right tabular-nums">
                ${g.credit_remaining_usd.toFixed(2)} / ${g.credit_granted_usd.toFixed(2)}
              </Td>
              <Td className="text-muted">{g.expires_at ? new Date(g.expires_at).toLocaleDateString() : '—'}</Td>
              <Td className="text-muted">{g.status}</Td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function RechargeForm({
  submitting,
  error,
  onCancel,
  onSubmit,
}: {
  submitting: boolean
  error: string | null
  onCancel: () => void
  onSubmit: (amount: number, note: string) => void
}) {
  const { t } = useI18n()
  const [amount, setAmount] = useState('')
  const [note, setNote] = useState('')
  const [localErr, setLocalErr] = useState<string | null>(null)

  function submit(e: React.FormEvent) {
    e.preventDefault()
    setLocalErr(null)
    const n = Number(amount.trim())
    if (!Number.isFinite(n) || n <= 0) {
      setLocalErr(t('users.rechargeAmountInvalid'))
      return
    }
    onSubmit(n, note.trim())
  }

  return (
    <form onSubmit={submit} className="space-y-4">
      <label className="block space-y-1">
        <span className="text-sm text-muted">{t('users.rechargeAmount')}</span>
        <input
          className="field"
          type="number"
          step="0.0001"
          value={amount}
          onChange={(e) => setAmount(e.target.value)}
          autoFocus
          placeholder="10.00"
        />
      </label>
      <label className="block space-y-1">
        <span className="text-sm text-muted">{t('users.rechargeNote')}</span>
        <input className="field" value={note} onChange={(e) => setNote(e.target.value)} placeholder={t('users.rechargeNotePlaceholder')} />
      </label>
      {(localErr || error) && <p className="text-sm text-danger">{localErr ?? error}</p>}
      <div className="flex justify-end gap-2">
        <button type="button" onClick={onCancel} className="btn btn-secondary">
          {t('common.cancel')}
        </button>
        <button type="submit" disabled={submitting} className="btn btn-primary">
          {submitting ? t('common.saving') : t('users.rechargeSubmit')}
        </button>
      </div>
    </form>
  )
}
