import { PortalLayout } from '../../components/PortalLayout'
import { ErrorNotice, LoadingTable } from '../../components/PageChrome'
import { ApiError } from '../../lib/portalApi'
import { useClaimPlan, useCurrentPlan, usePortalPlans, type Plan } from '../../lib/plans'
import { useI18n } from '../../lib/i18n'

export function PortalPlansPage() {
  const { t } = useI18n()
  const { data: plans, isLoading, error } = usePortalPlans()
  const { data: current } = useCurrentPlan()
  const claim = useClaimPlan()

  return (
    <PortalLayout>
      <main className="mx-auto max-w-5xl px-6 py-8">
        <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <h2 className="text-xl font-semibold">{t('portalPlans.title')}</h2>
            <p className="mt-1 max-w-2xl text-sm leading-6 text-muted">{t('portalPlans.subtitle')}</p>
          </div>
          {current?.grant && (
            <div className="rounded-xl border border-line bg-surface px-4 py-3 text-sm">
              <div className="text-xs uppercase tracking-wide text-muted">{t('portalPlans.current')}</div>
              <div className="mt-1 font-semibold">{current.grant.plan_name_snapshot}</div>
              <div className="mt-1 text-muted">{t('portalPlans.remaining', { amount: usd(current.grant.credit_remaining_usd) })}</div>
            </div>
          )}
        </div>
        {isLoading && <LoadingTable columns={3} />}
        {error && <ErrorNotice>{error instanceof ApiError ? error.message : t('portalPlans.loadError')}</ErrorNotice>}
        {plans && plans.length === 0 && <p className="rounded-xl border border-line bg-surface px-4 py-8 text-center text-sm text-muted">{t('portalPlans.empty')}</p>}
        {plans && plans.length > 0 && (
          <div className="grid gap-4 md:grid-cols-2">
            {plans.map((p) => (
              <PlanCard
                key={p.id}
                plan={p}
                claiming={claim.isPending}
                onClaim={() => claim.mutate(p.id)}
              />
            ))}
          </div>
        )}
        {claim.error && <div className="mt-4"><ErrorNotice>{claim.error instanceof ApiError ? claim.error.message : t('portalPlans.claimFailed')}</ErrorNotice></div>}
      </main>
    </PortalLayout>
  )
}

function PlanCard({ plan, claiming, onClaim }: { plan: Plan; claiming: boolean; onClaim: () => void }) {
  const { t } = useI18n()
  const free = plan.price_usd === 0
  return (
    <section className="rounded-xl border border-line bg-surface p-5">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h3 className="text-lg font-semibold">{plan.name}</h3>
          <p className="mt-2 text-sm leading-6 text-muted">{plan.description || t('portalPlans.noDescription')}</p>
        </div>
        <div className="shrink-0 rounded-lg bg-surface-2 px-3 py-2 text-right">
          <div className="text-lg font-semibold tabular-nums">{cny(plan.price_usd)}</div>
          <div className="text-xs text-muted">{t('portalPlans.price')}</div>
        </div>
      </div>
      <dl className="mt-5 grid grid-cols-2 gap-3 text-sm">
        <Fact label={t('portalPlans.credit')} value={usd(plan.included_credit_usd)} />
        <Fact label={t('portalPlans.validDays')} value={plan.valid_days ? `${plan.valid_days}d` : t('common.none')} />
        <Fact label={t('portalPlans.ratio')} value={`${plan.price_ratio.toFixed(2)}×`} />
        <Fact label={t('portalPlans.overage')} value={plan.allow_payg_overage ? t('plans.overageYes') : t('plans.overageNo')} />
      </dl>
      <div className="mt-5">
        {free ? (
          <button className="btn btn-primary w-full" disabled={claiming} onClick={onClaim}>{claiming ? t('common.saving') : t('portalPlans.claim')}</button>
        ) : (
          <button className="btn btn-secondary w-full" disabled>{t('portalPlans.contactAdmin')}</button>
        )}
      </div>
    </section>
  )
}
function Fact({ label, value }: { label: string; value: string }) { return <div className="rounded-lg bg-surface-2 px-3 py-2"><dt className="text-xs text-muted">{label}</dt><dd className="mt-1 font-medium tabular-nums">{value}</dd></div> }
function usd(n: number) { return `$${n.toFixed(4)}` }
function cny(n: number) {
  return new Intl.NumberFormat('zh-CN', {
    style: 'currency',
    currency: 'CNY',
    minimumFractionDigits: Number.isInteger(n) ? 0 : 2,
    maximumFractionDigits: 2,
  }).format(n)
}
