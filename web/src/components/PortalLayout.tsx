import { useState } from 'react'
import { NavLink } from 'react-router-dom'
import { usePortalAuth } from '../lib/portalAuth'
import { useI18n } from '../lib/i18n'
import { getTheme, nextTheme, setTheme, type Theme } from '../lib/theme'
import { LangSwitch } from './LangSwitch'

// PortalLayout is the end-user shell: the same top-header chrome as the
// admin console, but with the portal's own nav (Overview, API keys) and
// the portal session controls.
const NAV = [
  { to: '/portal', labelKey: 'portalNav.overview' },
  { to: '/portal/requests', labelKey: 'portalNav.requests' },
  { to: '/portal/keys', labelKey: 'portalNav.apiKeys' },
  { to: '/portal/wallet', labelKey: 'portalNav.wallet' },
]

export function PortalLayout({ children }: { children: React.ReactNode }) {
  const { me, logout } = usePortalAuth()
  const { t } = useI18n()
  return (
    <div className="min-h-screen bg-bg text-ink">
      <header className="sticky top-0 z-40 border-b border-line bg-surface/80 backdrop-blur supports-[backdrop-filter]:bg-surface/60">
        <div className="mx-auto flex h-16 w-full max-w-5xl items-center justify-between gap-4 px-6">
          <div className="flex min-w-0 items-center gap-4">
            <div className="flex shrink-0 items-center gap-2">
              <BrandMark />
              <span className="text-[15px] font-semibold tracking-tight">OmniHub</span>
            </div>
            <nav className="flex items-center gap-1 rounded-full border border-line bg-bg/60 px-1 py-1">
              {NAV.map((n) => (
                <NavLink
                  key={n.to}
                  to={n.to}
                  end={n.to === '/portal'}
                  className="whitespace-nowrap rounded-full px-3 py-1.5 text-sm font-medium transition-colors"
                  style={({ isActive }) =>
                    ({
                      color: isActive ? 'var(--ink)' : 'var(--muted)',
                      background: isActive ? 'color-mix(in oklch, var(--brand) 12%, transparent)' : 'transparent',
                    }) as React.CSSProperties
                  }
                >
                  {t(n.labelKey)}
                </NavLink>
              ))}
            </nav>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <LangSwitch />
            <ThemeToggle />
            <span className="hidden text-sm text-muted sm:inline">{me?.username}</span>
            <button onClick={logout} className="btn btn-secondary h-8">
              {t('common.signOut')}
            </button>
          </div>
        </div>
      </header>
      {children}
    </div>
  )
}

function BrandMark() {
  return (
    <span
      className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg"
      style={{ background: 'linear-gradient(135deg, var(--brand), var(--brand-2))', boxShadow: '0 4px 12px -4px var(--glow)' }}
    >
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden>
        <circle cx="12" cy="12" r="2.6" fill="white" />
        <circle cx="5" cy="6" r="1.8" fill="white" opacity="0.7" />
        <circle cx="19" cy="6" r="1.8" fill="white" opacity="0.7" />
        <circle cx="5" cy="18" r="1.8" fill="white" opacity="0.7" />
        <circle cx="19" cy="18" r="1.8" fill="white" opacity="0.7" />
        <path d="M12 12 5 6M12 12l7-6M12 12l-7 6M12 12l7 6" stroke="white" strokeWidth="1.2" opacity="0.55" />
      </svg>
    </span>
  )
}

function ThemeToggle() {
  const [theme, setLocal] = useState<Theme>(getTheme)
  function cycle() {
    const next = nextTheme(theme)
    setTheme(next)
    setLocal(next)
  }
  return (
    <button onClick={cycle} className="btn btn-ghost h-8 w-8 px-0" title="Toggle theme" aria-label="Toggle theme">
      {theme === 'system' ? (
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8"><rect x="2" y="4" width="20" height="13" rx="2" /><path d="M8 21h8M12 17v4" /></svg>
      ) : theme === 'light' ? (
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8"><circle cx="12" cy="12" r="4" /><path d="M12 2v2M12 20v2M2 12h2M20 12h2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" /></svg>
      ) : (
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8"><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" /></svg>
      )}
    </button>
  )
}
