import { useState } from 'react'
import { NavLink } from 'react-router-dom'
import { useAuth } from '../lib/auth'
import { getTheme, nextTheme, setTheme, type Theme } from '../lib/theme'

// Layout is the shared app shell: a themed left sidebar (light in light
// mode, dark in dark, per shadcn) with the brand, icon nav, and the
// account/theme footer, plus the scrolling content area. On narrow
// screens it collapses to a top bar.
const NAV = [
  { to: '/', label: 'Dashboard', icon: IconGrid },
  { to: '/accounts', label: 'Accounts', icon: IconServer },
  { to: '/keys', label: 'Keys', icon: IconKey },
  { to: '/blocked-ips', label: 'Blocked IPs', icon: IconShield },
  { to: '/health', label: 'Health', icon: IconPulse },
  { to: '/prices', label: 'Prices', icon: IconTag },
]

export function Layout({ children }: { children: React.ReactNode }) {
  return (
    <div className="min-h-screen bg-bg text-ink lg:flex">
      <Sidebar />
      <MobileBar />
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  )
}

function Sidebar() {
  const { me, logout } = useAuth()
  return (
    <aside
      className="sticky top-0 hidden h-screen w-60 shrink-0 flex-col border-r border-line lg:flex"
      style={{ background: 'var(--sidebar)' }}
    >
      <div className="flex items-center gap-2.5 px-5 py-4">
        <BrandMark />
        <span className="text-[15px] font-semibold tracking-tight" style={{ color: 'var(--sidebar-ink)' }}>
          OmniHub
        </span>
      </div>
      <nav className="flex-1 space-y-0.5 px-3 py-2">
        {NAV.map((n) => (
          <RailItem key={n.to} {...n} />
        ))}
      </nav>
      <div className="border-t border-line px-3 py-3">
        <div className="mb-2 flex items-center justify-between px-2">
          <span className="truncate text-sm" style={{ color: 'var(--sidebar-ink)' }}>
            {me?.username}
          </span>
          <ThemeToggle />
        </div>
        <button
          onClick={logout}
          className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-sm transition-colors"
          style={{ color: 'var(--sidebar-muted)' }}
          onMouseEnter={(e) => (e.currentTarget.style.background = 'var(--sidebar-hover)')}
          onMouseLeave={(e) => (e.currentTarget.style.background = 'transparent')}
        >
          <IconLogout />
          Sign out
        </button>
      </div>
    </aside>
  )
}

function RailItem({ to, label, icon: Icon }: (typeof NAV)[number]) {
  return (
    <NavLink to={to} end={to === '/'} className="group block">
      {({ isActive }) => (
        <span
          className="relative flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm font-medium transition-colors"
          style={{
            color: isActive ? 'var(--brand)' : 'var(--sidebar-muted)',
            background: isActive ? 'var(--brand-subtle)' : 'transparent',
          }}
          onMouseEnter={(e) => {
            if (!isActive) {
              e.currentTarget.style.background = 'var(--sidebar-hover)'
              e.currentTarget.style.color = 'var(--sidebar-ink)'
            }
          }}
          onMouseLeave={(e) => {
            if (!isActive) {
              e.currentTarget.style.background = 'transparent'
              e.currentTarget.style.color = 'var(--sidebar-muted)'
            }
          }}
        >
          {isActive && (
            <span
              className="absolute left-0 top-1/2 h-5 w-[3px] -translate-y-1/2 rounded-r-full"
              style={{ background: 'var(--brand)' }}
            />
          )}
          <span className="shrink-0">
            <Icon />
          </span>
          {label}
        </span>
      )}
    </NavLink>
  )
}

function MobileBar() {
  return (
    <header
      className="sticky top-0 z-20 flex items-center gap-3 overflow-x-auto border-b border-line px-4 py-2 lg:hidden"
      style={{ background: 'var(--sidebar)' }}
    >
      <div className="flex shrink-0 items-center gap-2">
        <BrandMark />
        <span className="text-sm font-semibold" style={{ color: 'var(--sidebar-ink)' }}>
          OmniHub
        </span>
      </div>
      <nav className="flex items-center gap-1">
        {NAV.map((n) => (
          <NavLink
            key={n.to}
            to={n.to}
            end={n.to === '/'}
            className="whitespace-nowrap rounded-lg px-2.5 py-1 text-sm font-medium"
            style={({ isActive }) =>
              ({
                color: isActive ? 'var(--brand)' : 'var(--sidebar-muted)',
                background: isActive ? 'var(--brand-subtle)' : 'transparent',
              }) as React.CSSProperties
            }
          >
            {n.label}
          </NavLink>
        ))}
      </nav>
      <div className="ml-auto shrink-0">
        <ThemeToggle />
      </div>
    </header>
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
    <button
      onClick={cycle}
      className="flex h-7 w-7 items-center justify-center rounded-lg transition-colors"
      style={{ color: 'var(--sidebar-muted)' }}
      onMouseEnter={(e) => {
        e.currentTarget.style.background = 'var(--sidebar-hover)'
        e.currentTarget.style.color = 'var(--sidebar-ink)'
      }}
      onMouseLeave={(e) => {
        e.currentTarget.style.background = 'transparent'
        e.currentTarget.style.color = 'var(--sidebar-muted)'
      }}
      title={`${label} (click to change)`}
      aria-label={label}
    >
      {theme === 'system' ? <IconMonitor /> : theme === 'light' ? <IconSun /> : <IconMoon />}
    </button>
  )
}

/* ── Icons (18px line) ─────────────────────────────────────────── */
const ic = { width: 18, height: 18, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 1.8, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const }
function IconGrid() { return (<svg {...ic}><rect x="3" y="3" width="7" height="7" rx="1.5" /><rect x="14" y="3" width="7" height="7" rx="1.5" /><rect x="3" y="14" width="7" height="7" rx="1.5" /><rect x="14" y="14" width="7" height="7" rx="1.5" /></svg>) }
function IconServer() { return (<svg {...ic}><rect x="3" y="4" width="18" height="7" rx="2" /><rect x="3" y="13" width="18" height="7" rx="2" /><path d="M7 7.5h.01M7 16.5h.01" /></svg>) }
function IconKey() { return (<svg {...ic}><circle cx="8" cy="8" r="4" /><path d="m11 11 8 8M16 16l2-2M19 19l2-2" /></svg>) }
function IconShield() { return (<svg {...ic}><path d="M12 3l7 3v5c0 4.5-3 7.5-7 9-4-1.5-7-4.5-7-9V6l7-3z" /><path d="m9 12 2 2 4-4" /></svg>) }
function IconPulse() { return (<svg {...ic}><path d="M3 12h4l2 6 4-13 2 7h6" /></svg>) }
function IconTag() { return (<svg {...ic}><path d="M3 12V5a2 2 0 0 1 2-2h7l9 9-9 9-9-9z" /><circle cx="8" cy="8" r="1.3" /></svg>) }
function IconLogout() { return (<svg {...ic} width="16" height="16"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9" /></svg>) }
function IconSun() { return (<svg {...ic} width="16" height="16"><circle cx="12" cy="12" r="4" /><path d="M12 2v2M12 20v2M2 12h2M20 12h2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" /></svg>) }
function IconMoon() { return (<svg {...ic} width="16" height="16"><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" /></svg>) }
function IconMonitor() { return (<svg {...ic} width="16" height="16"><rect x="2" y="4" width="20" height="13" rx="2" /><path d="M8 21h8M12 17v4" /></svg>) }
