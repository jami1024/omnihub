import { useAuth } from '../lib/auth'

export function DashboardPage() {
  const { me, logout } = useAuth()
  return (
    <div className="min-h-screen">
      <header className="flex items-center justify-between border-b border-zinc-200 bg-white px-6 py-3 dark:border-zinc-800 dark:bg-zinc-900">
        <div>
          <h1 className="text-lg font-semibold">OmniHub admin</h1>
        </div>
        <div className="flex items-center gap-3 text-sm text-zinc-500">
          <span>
            Signed in as <span className="text-zinc-700 dark:text-zinc-200">{me?.username}</span>
          </span>
          <button
            onClick={logout}
            className="rounded-md border border-zinc-300 px-2 py-1 hover:bg-zinc-100 dark:border-zinc-700 dark:hover:bg-zinc-800"
          >
            Sign out
          </button>
        </div>
      </header>

      <main className="mx-auto max-w-4xl px-6 py-10">
        <section className="rounded-lg border border-dashed border-zinc-300 bg-white p-8 text-center dark:border-zinc-700 dark:bg-zinc-900">
          <p className="text-base font-medium">M1 skeleton</p>
          <p className="mt-2 text-sm text-zinc-500">
            Accounts, keys, dashboard, blocked-IPs, and circuit-breaker events
            land in the next milestones.
          </p>
        </section>
      </main>
    </div>
  )
}
