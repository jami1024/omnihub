import { Layout } from '../components/Layout'
import { EmptyState, ErrorNotice, LoadingTable, MetricStrip, PageHeader } from '../components/PageChrome'
import { StatusBadge, Td, Th } from '../components/Table'
import { ApiError } from '../lib/api'
import { useI18n } from '../lib/i18n'
import { useDeleteUser, useSetUserEnabled, useUsers, type AdminUser } from '../lib/users'

export function UsersPage() {
  const { t } = useI18n()
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
    if (!confirm(t('users.deleteConfirm', { name: u.username }))) return
    del.mutate(u.id)
  }

  return (
    <Layout>
      <main className="mx-auto w-full max-w-7xl px-6 py-8">
        <PageHeader
          eyebrow={t('users.eyebrow')}
          context={t('users.context')}
          title={t('users.title')}
          description={t('users.description')}
        />

        {users && users.length > 0 && (
          <MetricStrip
            metrics={[
              { label: t('users.metricUsers'), value: total },
              { label: t('common.enabled'), value: enabled },
              { label: t('users.metricKeys'), value: keyCount },
              { label: t('users.metricSpend30d'), value: `$${spend.toFixed(2)}` },
            ]}
          />
        )}

        <div className="mt-6" />

        {isLoading && <LoadingTable columns={6} />}
        {error && <ErrorNotice>{error instanceof ApiError ? error.message : t('users.loadError')}</ErrorNotice>}

        {users && users.length === 0 && (
          <EmptyState
            eyebrow={t('users.emptyEyebrow')}
            title={t('users.emptyTitle')}
            description={t('users.emptyDescription')}
          />
        )}

        {users && users.length > 0 && (
          <div className="overflow-x-auto rounded-xl border border-line bg-surface">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
                <tr>
                  <Th>{t('users.colUsername')}</Th>
                  <Th>{t('common.email')}</Th>
                  <Th className="text-right">{t('users.colKeys')}</Th>
                  <Th className="text-right">{t('users.colSpent30d')}</Th>
                  <Th>{t('common.status')}</Th>
                  <Th>{t('users.colJoined')}</Th>
                  <Th className="text-right">{t('common.actions')}</Th>
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
                        {u.enabled ? t('common.disable') : t('common.enable')}
                      </button>
                      <button
                        onClick={() => remove(u)}
                        disabled={del.isPending}
                        className="btn-danger hover:underline disabled:opacity-50"
                      >
                        {t('common.delete')}
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
                : t('common.actionFailed')}
            </ErrorNotice>
          </div>
        )}
      </main>
    </Layout>
  )
}
