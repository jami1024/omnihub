import { Layout } from '../components/Layout'

export function DashboardPage() {
  return (
    <Layout>
      <main className="mx-auto max-w-4xl px-6 py-10">
        <section className="rounded-lg border border-dashed border-zinc-300 bg-white p-8 text-center dark:border-zinc-700 dark:bg-zinc-900">
          <p className="text-base font-medium">Dashboard</p>
          <p className="mt-2 text-sm text-zinc-500">
            Usage charts, blocked-IPs, and circuit-breaker events land in the
            next milestones. Manage upstream accounts under{' '}
            <span className="text-zinc-700 dark:text-zinc-200">Accounts</span>.
          </p>
        </section>
      </main>
    </Layout>
  )
}
