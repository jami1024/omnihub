import { Layout } from '../components/Layout'
import { EmptyState, ErrorNotice, LoadingTable, MetricStrip, PageHeader } from '../components/PageChrome'
import { StatusBadge, Td, Th } from '../components/Table'
import { ApiError } from '../lib/api'
import { useDeleteUser, useSetUserEnabled, useUsers, type AdminUser } from '../lib/users'

export function UsersPage() {
  const { data: users, isLoading, error } = useUsers()
  const setEnabled = useSetUserEnabled()
  const del = useDeleteUser()

  const total = users?.length ?? 0
  const enabled = users?.filter((u) => u.enabled).length ?? 0
  const keyCount = users?.reduce((s, u) => s + u.key_count, 0) ?? 0
  const spend = users?.reduce((s, u) => s + u.spend_30d, 0) ?? 0

  function toggle(u: AdminUser) {
    setEnabled.mutate({ id: u.id, enabled: !u.enabled })
  }
  function remove(u: AdminUser) {
    if (!confirm(`Delete user "${u.username}"? Their keys stay but become unowned.`)) return
    del.mutate(u.id)
  }

  return (
    <Layout>
      <main className="mx-auto w-full max-w-7xl px-6 py-8">
        <PageHeader
          eyebrow="PORTAL"
          context="Self-service accounts"
          title="Users"
          description="People who registered on the end-user portal, with their key count and 30-day spend. Disable or remove an account here."
        />

        {users && users.length > 0 && (
          <MetricStrip
            metrics={[
              { label: 'Users', value: total },
              { label: 'Enabled', value: enabled },
              { label: 'Keys', value: keyCount },
              { label: 'Spend 30d', value: `$${spend.toFixed(2)}` },
            ]}
          />
        )}

        <div className="mt-6" />

        {isLoading && <LoadingTable columns={6} />}
        {error && <ErrorNotice>{error instanceof ApiError ? error.message : 'Could not load users.'}</ErrorNotice>}

        {users && users.length === 0 && (
          <EmptyState
            eyebrow="NO USERS YET"
            title="No one has signed up"
            description="End users register at /portal. Once they do, their accounts, keys, and spend show up here for you to manage."
          />
        )}

        {users && users.length > 0 && (
          <div className="overflow-x-auto rounded-xl border border-line bg-surface">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
                <tr>
                  <Th>Username</Th>
                  <Th>Email</Th>
                  <Th className="text-right">Keys</Th>
                  <Th className="text-right">Spent (30d)</Th>
                  <Th>Status</Th>
                  <Th>Joined</Th>
                  <Th className="text-right">Actions</Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {users.map((u) => (
                  <tr key={u.id} className="transition-colors hover:bg-surface-2">
                    <Td className="font-medium">{u.username}</Td>
                    <Td className="text-muted">{u.email || '—'}</Td>
                    <Td className="text-right tabular-nums">{u.key_count}</Td>
                    <Td className="text-right tabular-nums">${u.spend_30d.toFixed(4)}</Td>
                    <Td>
                      <StatusBadge enabled={u.enabled} />
                    </Td>
                    <Td className="text-muted">{new Date(u.created_at).toLocaleDateString()}</Td>
                    <Td className="text-right">
                      <button
                        onClick={() => toggle(u)}
                        disabled={setEnabled.isPending}
                        className="mr-3 text-muted underline-offset-4 hover:text-ink hover:underline disabled:opacity-50"
                      >
                        {u.enabled ? 'Disable' : 'Enable'}
                      </button>
                      <button
                        onClick={() => remove(u)}
                        disabled={del.isPending}
                        className="btn-danger hover:underline disabled:opacity-50"
                      >
                        Delete
                      </button>
                    </Td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {(setEnabled.error || del.error) && (
          <div className="mt-3">
            <ErrorNotice>
              {(setEnabled.error || del.error) instanceof ApiError
                ? ((setEnabled.error || del.error) as ApiError).message
                : 'Action failed.'}
            </ErrorNotice>
          </div>
        )}
      </main>
    </Layout>
  )
}
