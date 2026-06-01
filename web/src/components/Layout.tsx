import { useState } from 'react'
import { NavLink } from 'react-router-dom'
import { useAuth } from '../lib/auth'
import { getTheme, nextTheme, setTheme, type Theme } from '../lib/theme'

// Layout is the shared chrome for every authenticated page: a sticky top
// bar with the brand mark, section nav, theme toggle, and the signed-in
// identity. Pages render their own <main> inside {children}.
export function Layout({ children }: { children: React.ReactNode }) {
  const { me, logout } = useAuth()
  return (
    <div className="min-h-screen bg-bg text-ink">
      <header className="sticky top-0 z-20 border-b border-line bg-surface-2/80 backdrop-blur supports-[backdrop-filter]:bg-surface-2/70">
        <div className="mx-auto flex max-w-6xl items-center justify-between gap-4 px-6 py-2.5">
          <div className="flex items-center gap-6">
            <Brand />
            <nav className="hidden items-center gap-0.5 text-sm md:flex">
              <NavItem to="/">Dashboard</NavItem>
              <NavItem to="/accounts">Accounts</NavItem>
              <NavItem to="/keys">Keys</NavItem>
              <NavItem to="/blocked-ips">Blocked IPs</NavItem>
              <NavItem to="/health">Health</NavItem>
              <NavItem to="/prices">Prices</NavItem>
            </nav>
          </div>
          <div className="flex items-center gap-2 text-sm">
            <ThemeToggle />
            <span className="hidden text-muted sm:inline">
              {me?.username}
            </span>
            <button onClick={logout} className="btn btn-ghost">
              Sign out
            </button>
          </div>
        </div>
        {/* Nav collapses to a scrollable row on narrow screens. */}
        <nav className="flex items-center gap-0.5 overflow-x-auto border-t border-line px-4 py-1.5 text-sm md:hidden">
          <NavItem to="/">Dashboard</NavItem>
          <NavItem to="/accounts">Accounts</NavItem>
          <NavItem to="/keys">Keys</NavItem>
          <NavItem to="/blocked-ips">Blocked IPs</NavItem>
          <NavItem to="/health">Health</NavItem>
          <NavItem to="/prices">Prices</NavItem>
        </nav>
      </header>
      {children}
    </div>
  )
}

function Brand() {
  return (
    <div className="flex items-center gap-2">
      <svg width="22" height="22" viewBox="0 0 24 24" fill="none" aria-hidden className="text-brand">
        <circle cx="12" cy="12" r="3" fill="currentColor" />
        <circle cx="4" cy="5" r="2" fill="currentColor" opacity="0.55" />
        <circle cx="20" cy="5" r="2" fill="currentColor" opacity="0.55" />
        <circle cx="4" cy="19" r="2" fill="currentColor" opacity="0.55" />
        <circle cx="20" cy="19" r="2" fill="currentColor" opacity="0.55" />
        <path
          d="M12 12 4 5M12 12l8-7M12 12l-8 7M12 12l8 7"
          stroke="currentColor"
          strokeWidth="1.25"
          opacity="0.4"
        />
      </svg>
      <span className="text-[15px] font-semibold tracking-tight">OmniHub</span>
    </div>
  )
}

function NavItem({ to, children }: { to: string; children: React.ReactNode }) {
  return (
    <NavLink
      to={to}
      end={to === '/'}
      className={({ isActive }) =>
        `whitespace-nowrap rounded-lg px-3 py-1.5 transition-colors ${
          isActive
            ? 'bg-brand-subtle font-medium text-brand'
            : 'text-muted hover:bg-surface hover:text-ink'
        }`
      }
    >
      {children}
    </NavLink>
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
      className="btn btn-ghost px-2"
      title={`${label} (click to change)`}
      aria-label={label}
    >
      {theme === 'system' ? <IconMonitor /> : theme === 'light' ? <IconSun /> : <IconMoon />}
    </button>
  )
}

function IconSun() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M2 12h2M20 12h2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
    </svg>
  )
}
function IconMoon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" />
    </svg>
  )
}
function IconMonitor() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
      <rect x="2" y="4" width="20" height="13" rx="2" />
      <path d="M8 21h8M12 17v4" />
    </svg>
  )
}
