import { useState } from 'react'
import { NavLink } from 'react-router-dom'
import { useAuth } from '../lib/auth'
import { getTheme, nextTheme, setTheme, type Theme } from '../lib/theme'

// Layout is the shared chrome, matching claude-code-hub: a sticky top
// header (blurred card surface) with a rounded-pill nav, centered to
// max-w-7xl, and right-side theme + identity controls. Pages render
// their own <main> inside {children}.
const NAV = [
  { to: '/', label: 'Dashboard' },
  { to: '/accounts', label: 'Accounts' },
  { to: '/keys', label: 'Keys' },
  { to: '/blocked-ips', label: 'Blocked IPs' },
  { to: '/health', label: 'Health' },
  { to: '/prices', label: 'Prices' },
]

export function Layout({ children }: { children: React.ReactNode }) {
  const { me, logout } = useAuth()
  return (
    <div className="min-h-screen bg-bg text-ink">
      <header className="sticky top-0 z-40 border-b border-line bg-surface/80 backdrop-blur supports-[backdrop-filter]:bg-surface/60">
        <div className="mx-auto flex h-16 w-full max-w-7xl items-center justify-between gap-4 px-6">
          <div className="flex min-w-0 items-center gap-4">
            <div className="flex shrink-0 items-center gap-2">
              <BrandMark />
              <span className="hidden text-[15px] font-semibold tracking-tight sm:inline">OmniHub</span>
            </div>
            <nav className="hidden items-center gap-1 overflow-x-auto rounded-full border border-line bg-bg/60 px-1 py-1 md:flex">
              {NAV.map((n) => (
                <PillLink key={n.to} {...n} />
              ))}
            </nav>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <ThemeToggle />
            <span className="hidden text-sm text-muted sm:inline">{me?.username}</span>
            <button onClick={logout} className="btn btn-secondary h-8">
              Sign out
            </button>
          </div>
        </div>
        {/* Mobile: the pill nav scrolls under the header on narrow screens. */}
        <nav className="flex items-center gap-1 overflow-x-auto border-t border-line px-4 py-1.5 md:hidden">
          {NAV.map((n) => (
            <PillLink key={n.to} {...n} />
          ))}
        </nav>
      </header>
      {children}
    </div>
  )
}

function PillLink({ to, label }: { to: string; label: string }) {
  return (
    <NavLink
      to={to}
      end={to === '/'}
      className="whitespace-nowrap rounded-full px-3 py-1.5 text-sm font-medium transition-colors"
      style={({ isActive }) =>
        ({
          color: isActive ? 'var(--ink)' : 'var(--muted)',
          background: isActive ? 'color-mix(in oklch, var(--brand) 12%, transparent)' : 'transparent',
        }) as React.CSSProperties
      }
    >
      {label}
    </NavLink>
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
    <button onClick={cycle} className="btn btn-ghost h-8 w-8 px-0" title={`${label} (click to change)`} aria-label={label}>
      {theme === 'system' ? <IconMonitor /> : theme === 'light' ? <IconSun /> : <IconMoon />}
    </button>
  )
}

const ic = { width: 16, height: 16, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 1.8, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const }
function IconSun() { return (<svg {...ic}><circle cx="12" cy="12" r="4" /><path d="M12 2v2M12 20v2M2 12h2M20 12h2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" /></svg>) }
function IconMoon() { return (<svg {...ic}><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" /></svg>) }
function IconMonitor() { return (<svg {...ic}><rect x="2" y="4" width="20" height="13" rx="2" /><path d="M8 21h8M12 17v4" /></svg>) }
