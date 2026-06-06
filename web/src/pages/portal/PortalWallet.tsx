import { useState } from 'react'
import { PortalLayout } from '../../components/PortalLayout'
import { Td, Th } from '../../components/Table'
import { ApiError } from '../../lib/portalApi'
import { usePortalWallet, useRedeemCode } from '../../lib/portalData'
import { useI18n } from '../../lib/i18n'

export function PortalWalletPage() {
  const { t, lang } = useI18n()
  const { data, isLoading, error } = usePortalWallet()
  const locale = lang === 'zh' ? 'zh-CN' : 'en-US'
  const fmt = (n: number) => `$${n.toFixed(4)}`

  return (
    <PortalLayout>
      <main className="mx-auto max-w-5xl px-6 py-8">
        <div className="mb-6">
          <h2 className="text-xl font-semibold">{t('portalWallet.title')}</h2>
          <p className="text-sm text-muted">{t('portalWallet.subtitle')}</p>
        </div>

        {isLoading && <p className="text-sm text-muted">{t('common.loading')}</p>}
        {error && (
          <p className="text-sm text-danger">
            {error instanceof ApiError ? error.message : t('portalWallet.loadError')}
          </p>
        )}

        {data && (
          <>
            <div className="mb-8 grid grid-cols-1 gap-4 sm:grid-cols-3">
              <Stat label={t('portalWallet.balance')} value={fmt(data.balance)} highlight={data.balance <= 0} />
              <Stat label={t('portalWallet.credits')} value={fmt(data.credits)} />
              <Stat label={t('portalWallet.spent')} value={fmt(data.spent)} />
            </div>

            {data.balance <= 0 && (
              <p className="mb-6 rounded-lg border border-line bg-danger/10 px-4 py-3 text-sm text-danger">
                {t('portalWallet.depleted')}
              </p>
            )}

            <RedeemBox />

            <h3 className="mb-3 text-sm font-semibold text-muted">{t('portalWallet.history')}</h3>
            {data.entries.length === 0 ? (
              <p className="rounded-lg border border-line bg-surface px-4 py-8 text-center text-sm text-muted">
                {t('portalWallet.noEntries')}
              </p>
            ) : (
              <div className="overflow-x-auto rounded-xl border border-line bg-surface">
                <table className="w-full text-left text-sm">
                  <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
                    <tr>
                      <Th>{t('portalWallet.colTime')}</Th>
                      <Th>{t('portalWallet.colKind')}</Th>
                      <Th className="text-right">{t('portalWallet.colAmount')}</Th>
                      <Th>{t('portalWallet.colNote')}</Th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-line">
                    {data.entries.map((e, i) => (
                      <tr key={i} className="transition-colors hover:bg-surface-2">
                        <Td className="whitespace-nowrap text-muted">
                          {new Date(e.created_at).toLocaleString(locale)}
                        </Td>
                        <Td className="font-medium">{e.kind}</Td>
                        <Td className="text-right tabular-nums">{fmt(e.amount_usd)}</Td>
                        <Td className="text-muted">{e.note || '—'}</Td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </>
        )}
      </main>
    </PortalLayout>
  )
}

function RedeemBox() {
  const { t } = useI18n()
  const redeem = useRedeemCode()
  const [code, setCode] = useState('')
  const [msg, setMsg] = useState<string | null>(null)

  function submit(e: React.FormEvent) {
    e.preventDefault()
    setMsg(null)
    if (!code.trim()) return
    redeem.mutate(code.trim(), {
      onSuccess: (r) => {
        setMsg(t('portalWallet.redeemed', { amount: `$${r.credited.toFixed(4)}` }))
        setCode('')
      },
      onError: (err) => setMsg(err instanceof ApiError ? err.message : t('portalWallet.redeemFailed')),
    })
  }

  return (
    <div className="mb-8 rounded-xl border border-line bg-surface px-5 py-4">
      <h3 className="mb-2 text-sm font-semibold">{t('portalWallet.redeemTitle')}</h3>
      <form onSubmit={submit} className="flex flex-wrap items-center gap-2">
        <input
          className="field h-10 min-w-0 flex-1 sm:max-w-xs"
          value={code}
          onChange={(e) => setCode(e.target.value)}
          placeholder="OMNI-XXXX-XXXX-XXXX-XXXX-XXXX-XXXX"
        />
        <button type="submit" disabled={redeem.isPending} className="btn btn-primary h-10 disabled:opacity-50">
          {redeem.isPending ? t('common.saving') : t('portalWallet.redeemButton')}
        </button>
      </form>
      {msg && <p className="mt-2 text-sm text-muted">{msg}</p>}
    </div>
  )
}

function Stat({ label, value, highlight }: { label: string; value: string; highlight?: boolean }) {
  return (
    <div className="rounded-xl border border-line bg-surface px-5 py-4">
      <div className="text-xs uppercase tracking-wide text-muted">{label}</div>
      <div className={'mt-1 text-2xl font-semibold tabular-nums ' + (highlight ? 'text-danger' : '')}>{value}</div>
    </div>
  )
}
