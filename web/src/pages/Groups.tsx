import { useState } from 'react'
import { Layout } from '../components/Layout'
import { Modal } from '../components/Modal'
import { EmptyState, ErrorNotice, LoadingTable, MetricStrip, PageHeader } from '../components/PageChrome'
import { Td, Th } from '../components/Table'
import { ApiError } from '../lib/api'
import { useI18n } from '../lib/i18n'
import {
  useCreateGroup,
  useDeleteGroup,
  useGroups,
  useUpdateGroup,
  type GroupInput,
  type ProviderGroup,
} from '../lib/groups'

type Editing = 'new' | ProviderGroup | null

export function GroupsPage() {
  const { t } = useI18n()
  const { data: groups, isLoading, error } = useGroups()
  const create = useCreateGroup()
  const update = useUpdateGroup()
  const del = useDeleteGroup()
  const [editing, setEditing] = useState<Editing>(null)
  const [formErr, setFormErr] = useState<string | null>(null)

  const count = groups?.length ?? 0
  const grouped = groups?.reduce((s, g) => s + g.account_count, 0) ?? 0

  function close() {
    setEditing(null)
    setFormErr(null)
  }

  function submit(input: GroupInput) {
    setFormErr(null)
    const onError = (e: unknown) =>
      setFormErr(e instanceof ApiError ? e.message : t('groups.saveError'))
    if (editing === 'new') {
      create.mutate(input, { onSuccess: close, onError })
    } else if (editing) {
      update.mutate({ id: editing.id, input }, { onSuccess: close, onError })
    }
  }

  function remove(g: ProviderGroup) {
    if (!confirm(t('groups.deleteConfirm', { name: g.name, n: g.account_count }))) return
    del.mutate(g.id)
  }

  return (
    <Layout>
      <main className="mx-auto w-full max-w-7xl px-6 py-8">
        <div className="flex items-start justify-between gap-4">
          <PageHeader
            eyebrow={t('groups.eyebrow')}
            context={t('groups.context')}
            title={t('groups.title')}
            description={t('groups.description')}
          />
          <button onClick={() => setEditing('new')} className="btn btn-primary h-10 shrink-0">
            {t('groups.newGroup')}
          </button>
        </div>

        {groups && groups.length > 0 && (
          <MetricStrip
            metrics={[
              { label: t('groups.metricGroups'), value: count },
              { label: t('groups.metricGroupedAccounts'), value: grouped },
            ]}
          />
        )}

        <div className="mt-6" />

        {isLoading && <LoadingTable columns={4} />}
        {error && <ErrorNotice>{error instanceof ApiError ? error.message : t('groups.loadError')}</ErrorNotice>}

        {groups && groups.length === 0 && (
          <EmptyState
            eyebrow={t('groups.emptyEyebrow')}
            title={t('groups.emptyTitle')}
            description={t('groups.emptyDescription')}
            action={
              <button onClick={() => setEditing('new')} className="btn btn-primary h-10">
                {t('groups.newGroup')}
              </button>
            }
          />
        )}

        {groups && groups.length > 0 && (
          <div className="overflow-x-auto rounded-xl border border-line bg-surface">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
                <tr>
                  <Th>{t('common.name')}</Th>
                  <Th className="text-right">{t('groups.colCost')}</Th>
                  <Th className="text-right">{t('groups.colAccounts')}</Th>
                  <Th>{t('groups.colDescription')}</Th>
                  <Th className="text-right">{t('common.actions')}</Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {groups.map((g) => (
                  <tr key={g.id} className="transition-colors hover:bg-surface-2">
                    <Td className="font-medium">{g.name}</Td>
                    <Td className="text-right tabular-nums">{g.cost_multiplier}</Td>
                    <Td className="text-right tabular-nums">{g.account_count}</Td>
                    <Td className="text-muted">{g.description || '—'}</Td>
                    <Td className="text-right">
                      <button
                        onClick={() => setEditing(g)}
                        className="mr-1 inline-flex min-h-10 items-center rounded-md px-2 text-muted underline-offset-4 hover:bg-surface-2 hover:text-ink hover:underline sm:mr-3 sm:px-1"
                      >
                        {t('common.edit')}
                      </button>
                      <button
                        onClick={() => remove(g)}
                        disabled={del.isPending}
                        className="inline-flex min-h-10 items-center rounded-md px-2 btn-danger hover:underline disabled:opacity-50"
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

        {del.error && (
          <div className="mt-3">
            <ErrorNotice>{del.error instanceof ApiError ? del.error.message : t('groups.deleteError')}</ErrorNotice>
          </div>
        )}

        {editing && (
          <Modal title={editing === 'new' ? t('groups.newGroup') : t('groups.editGroup', { name: editing.name })} onClose={close}>
            <GroupForm
              group={editing === 'new' ? undefined : editing}
              submitting={create.isPending || update.isPending}
              error={formErr}
              onCancel={close}
              onSubmit={submit}
            />
          </Modal>
        )}
      </main>
    </Layout>
  )
}

const FIELD = 'w-full rounded-lg border border-line bg-surface px-3 py-2 text-sm outline-none focus:border-ink'

function GroupForm({
  group,
  submitting,
  error,
  onCancel,
  onSubmit,
}: {
  group?: ProviderGroup
  submitting: boolean
  error: string | null
  onCancel: () => void
  onSubmit: (input: GroupInput) => void
}) {
  const { t } = useI18n()
  const [name, setName] = useState(group?.name ?? '')
  const [mult, setMult] = useState(String(group?.cost_multiplier ?? 1))
  const [description, setDescription] = useState(group?.description ?? '')
  const [localErr, setLocalErr] = useState<string | null>(null)

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setLocalErr(null)
    if (!name.trim()) {
      setLocalErr(t('groups.nameRequired'))
      return
    }
    const m = Number(mult)
    if (!Number.isFinite(m) || m < 0) {
      setLocalErr(t('groups.multiplierInvalid'))
      return
    }
    onSubmit({ name: name.trim(), cost_multiplier: m, description: description.trim() })
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <label className="block space-y-1">
        <span className="text-sm text-muted">{t('common.name')}</span>
        <input className={FIELD} value={name} onChange={(e) => setName(e.target.value)} autoFocus />
      </label>
      <label className="block space-y-1">
        <span className="text-sm text-muted">{t('groups.costMultiplier')}</span>
        <input
          className={FIELD}
          type="number"
          step="0.1"
          min="0"
          value={mult}
          onChange={(e) => setMult(e.target.value)}
        />
      </label>
      <label className="block space-y-1">
        <span className="text-sm text-muted">{t('groups.descriptionOptional')}</span>
        <input className={FIELD} value={description} onChange={(e) => setDescription(e.target.value)} />
      </label>

      {(localErr || error) && <p className="text-sm text-danger">{localErr ?? error}</p>}

      <div className="flex justify-end gap-2">
        <button type="button" onClick={onCancel} className="btn btn-secondary">
          {t('common.cancel')}
        </button>
        <button type="submit" disabled={submitting} className="btn btn-primary">
          {submitting ? t('common.saving') : group ? t('common.saveChanges') : t('groups.createGroup')}
        </button>
      </div>
    </form>
  )
}
