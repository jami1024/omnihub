import { NavLink } from 'react-router-dom'
import { useAuth } from '../lib/auth'

// Layout is the shared chrome for every authenticated page: the top bar
// with the brand, the section nav, and the signed-in identity + sign-out
// control. Pages render their own <main> inside {children}.
export function Layout({ children }: { children: React.ReactNode }) {
  const { me, logout } = useAuth()
  return (
    <div className="min-h-screen">
      <header className="flex items-center justify-between border-b border-zinc-200 bg-white px-6 py-3 dark:border-zinc-800 dark:bg-zinc-900">
        <div className="flex items-center gap-6">
          <h1 className="text-lg font-semibold">OmniHub admin</h1>
          <nav className="flex items-center gap-1 text-sm">
            <NavItem to="/">Dashboard</NavItem>
            <NavItem to="/accounts">Accounts</NavItem>
            <NavItem to="/keys">Keys</NavItem>
          </nav>
        </div>
        <div className="flex items-center gap-3 text-sm text-zinc-500">
          <span>
            Signed in as{' '}
            <span className="text-zinc-700 dark:text-zinc-200">{me?.username}</span>
          </span>
          <button
            onClick={logout}
            className="rounded-md border border-zinc-300 px-2 py-1 hover:bg-zinc-100 dark:border-zinc-700 dark:hover:bg-zinc-800"
          >
            Sign out
          </button>
        </div>
      </header>
      {children}
    </div>
  )
}

function NavItem({ to, children }: { to: string; children: React.ReactNode }) {
  return (
    <NavLink
      to={to}
      end={to === '/'}
      className={({ isActive }) =>
        `rounded-md px-3 py-1.5 ${
          isActive
            ? 'bg-zinc-100 font-medium text-zinc-900 dark:bg-zinc-800 dark:text-zinc-100'
            : 'text-zinc-500 hover:bg-zinc-100 dark:hover:bg-zinc-800'
        }`
      }
    >
      {children}
    </NavLink>
  )
}
