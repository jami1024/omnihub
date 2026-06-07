import { Link, useLocation } from 'react-router-dom'
import { Layout } from './Layout'
import { useI18n } from '../lib/i18n'

const SETTINGS_NAV = [
  {
    to: '/admin/settings',
    labelKey: 'settings.navPortal',
    descriptionKey: 'settings.navPortalHint',
  },
  {
    to: '/admin/blocked-ips',
    labelKey: 'nav.blockedIps',
    descriptionKey: 'settings.toolBlockedIpsHint',
  },
  {
    to: '/admin/alert-channels',
    labelKey: 'nav.alertChannels',
    descriptionKey: 'settings.toolAlertsHint',
  },
  {
    to: '/admin/prices',
    labelKey: 'nav.prices',
    descriptionKey: 'settings.toolPricesHint',
  },
  {
    to: '/admin/redemptions',
    labelKey: 'nav.redemptions',
    descriptionKey: 'settings.toolCodesHint',
  },
]

export function SettingsLayout({ children }: { children: React.ReactNode }) {
  const { t } = useI18n()
  const { pathname } = useLocation()

  return (
    <Layout>
      <main className="mx-auto grid w-full max-w-7xl gap-6 px-6 py-8 lg:grid-cols-[16rem_minmax(0,1fr)] lg:items-start">
        <aside className="lg:sticky lg:top-24">
          <div className="rounded-2xl border border-line bg-surface p-2 shadow-sm">
            <div className="px-3 py-2">
              <p className="font-mono text-[11px] font-medium uppercase tracking-[0.16em] text-muted">
                {t('settings.navEyebrow')}
              </p>
              <p className="mt-1 text-sm font-semibold text-ink">{t('nav.settings')}</p>
            </div>
            <nav className="mt-1 flex gap-1 overflow-x-auto lg:flex-col lg:overflow-visible">
              {SETTINGS_NAV.map((item) => {
                const active = pathname === item.to
                return (
                  <Link
                    key={item.to}
                    to={item.to}
                    aria-current={active ? 'page' : undefined}
                    className="settings-side-link min-w-[12rem] rounded-xl px-3 py-3 transition-colors lg:min-w-0"
                    style={{
                      color: active ? 'var(--ink)' : 'var(--muted)',
                      background: active ? 'color-mix(in oklch, var(--brand) 10%, var(--surface-2))' : 'transparent',
                      border: active ? '1px solid color-mix(in oklch, var(--brand) 16%, var(--border))' : '1px solid transparent',
                    }}
                  >
                    <span className="block text-sm font-semibold">{t(item.labelKey)}</span>
                    <span className="mt-1 hidden text-xs leading-5 text-muted lg:block">
                      {t(item.descriptionKey)}
                    </span>
                  </Link>
                )
              })}
            </nav>
          </div>
        </aside>
        <section className="min-w-0">{children}</section>
      </main>
    </Layout>
  )
}
