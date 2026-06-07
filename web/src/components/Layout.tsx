import { useState } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { useAuth } from '../lib/auth'
import { useI18n } from '../lib/i18n'
import { getTheme, nextTheme, setTheme, type Theme } from '../lib/theme'
import { LangSwitch } from './LangSwitch'

// Layout is the shared chrome, matching claude-code-hub: a sticky top
// header (blurred card surface) with a rounded-pill nav, centered to
// max-w-7xl, and right-side theme + identity controls. Pages render
// their own <main> inside {children}.
const SETTINGS_ROUTES = ['/admin/settings', '/admin/blocked-ips', '/admin/alert-channels', '/admin/prices', '/admin/redemptions']

type NavItem = { to: string; labelKey: string; activeWhen?: string[] }

const NAV: NavItem[] = [
  { to: '/admin', labelKey: 'nav.dashboard' },
  { to: '/admin/accounts', labelKey: 'nav.accounts' },
  { to: '/admin/groups', labelKey: 'nav.groups' },
  { to: '/admin/keys', labelKey: 'nav.keys' },
  { to: '/admin/health', labelKey: 'nav.health' },
  { to: '/admin/users', labelKey: 'nav.users' },
  { to: '/admin/settings', labelKey: 'nav.settings', activeWhen: SETTINGS_ROUTES },
]

export function Layout({ children }: { children: React.ReactNode }) {
  const { me, logout } = useAuth()
  const { t } = useI18n()
  return (
    <div className="min-h-screen bg-bg text-ink">
      <header className="sticky top-0 z-40 border-b border-line bg-surface/86 backdrop-blur supports-[backdrop-filter]:bg-surface/72">
        <div className="mx-auto flex h-16 w-full max-w-[96rem] items-center gap-4 px-4 sm:px-6">
          <div className="flex min-w-0 flex-1 items-center gap-4">
            <div className="flex shrink-0 items-center gap-2.5">
              <BrandMark />
              <span className="hidden text-[15px] font-semibold tracking-tight sm:inline">OmniHub</span>
            </div>
            <nav className="admin-shell-nav flex min-w-0 flex-1 items-center gap-0.5 overflow-x-auto rounded-[1.35rem] border border-line bg-bg/60 p-1">
              {NAV.map((n) => (
                <PillLink key={n.to} to={n.to} label={t(n.labelKey)} activeWhen={n.activeWhen} />
              ))}
            </nav>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <LangSwitch />
            <ThemeToggle />
            <span className="hidden max-w-[14rem] truncate text-sm text-muted xl:inline">{me?.username}</span>
            <button onClick={logout} className="btn btn-secondary min-h-10">
              {t('common.signOut')}
            </button>
          </div>
        </div>
      </header>
      {children}
    </div>
  )
}

function PillLink({ to, label, activeWhen = [] }: { to: string; label: string; activeWhen?: string[] }) {
  const { pathname } = useLocation()
  const active = to === '/admin' ? pathname === '/admin' : pathname === to || activeWhen.includes(pathname)
  return (
    <Link
      to={to}
      aria-current={active ? 'page' : undefined}
      data-active={active ? 'true' : undefined}
      className="admin-shell-nav-link inline-flex min-h-9 flex-1 basis-0 items-center justify-center whitespace-nowrap rounded-full px-3 text-sm font-medium transition-colors"
    >
      {label}
    </Link>
  )
}

function BrandMark() {
  return (
    <span
      className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg"
      style={{
        background: 'linear-gradient(135deg, var(--brand), var(--brand-2))',
        boxShadow: '0 4px 12px -4px var(--glow)',
      }}
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
  const label = theme === 'system' ? 'System theme' : theme === 'light' ? 'Light theme' : 'Dark theme'
  return (
    <button onClick={cycle} className="btn btn-ghost h-10 w-10 px-0" title={`${label} (click to change)`} aria-label={label}>
      {theme === 'system' ? <IconMonitor /> : theme === 'light' ? <IconSun /> : <IconMoon />}
    </button>
  )
}

const ic = { width: 16, height: 16, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 1.8, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const }
function IconSun() { return (<svg {...ic}><circle cx="12" cy="12" r="4" /><path d="M12 2v2M12 20v2M2 12h2M20 12h2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" /></svg>) }
function IconMoon() { return (<svg {...ic}><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" /></svg>) }
function IconMonitor() { return (<svg {...ic}><rect x="2" y="4" width="20" height="13" rx="2" /><path d="M8 21h8M12 17v4" /></svg>) }
