import { useState } from 'react'
import { Modal } from '../components/Modal'
import { EmptyState, ErrorNotice, LoadingTable, MetricStrip, PageHeader } from '../components/PageChrome'
import { SettingsLayout } from '../components/SettingsLayout'
import { StatusBadge, Td, Th } from '../components/Table'
import { ApiError } from '../lib/api'
import { useI18n } from '../lib/i18n'
import { useAnnouncements, useDeleteAnnouncement, useSaveAnnouncement, type Announcement, type AnnouncementInput } from '../lib/announcements'

type Editing = 'new' | Announcement | null

export function AnnouncementsPage() {
  const { t } = useI18n()
  const { data, isLoading, error } = useAnnouncements()
  const save = useSaveAnnouncement()
  const del = useDeleteAnnouncement()
  const [editing, setEditing] = useState<Editing>(null)
  const published = data?.filter((a) => a.status === 'published').length ?? 0

  function remove(a: Announcement) {
    if (!confirm(t('announcements.deleteConfirm', { title: a.title }))) return
    del.mutate(a.id)
  }

  return (
    <SettingsLayout>
      <PageHeader
        eyebrow={t('announcements.eyebrow')}
        context={t('announcements.context')}
        title={t('announcements.title')}
        description={t('announcements.description')}
        action={<button className="btn btn-primary h-10" onClick={() => setEditing('new')}>{t('announcements.add')}</button>}
      />
      <MetricStrip metrics={[{ label: t('announcements.metricTotal'), value: data?.length ?? 0 }, { label: t('announcements.metricPublished'), value: published }, { label: t('announcements.metricDraft'), value: (data?.length ?? 0) - published }, { label: t('announcements.metricPlacements'), value: 3 }]} />
      <div className="mt-6" />
      {isLoading && <LoadingTable columns={6} />}
      {error && <ErrorNotice>{error instanceof ApiError ? error.message : t('announcements.loadError')}</ErrorNotice>}
      {data && data.length === 0 && <EmptyState eyebrow={t('announcements.emptyEyebrow')} title={t('announcements.emptyTitle')} description={t('announcements.emptyDescription')} action={<button className="btn btn-primary" onClick={() => setEditing('new')}>{t('announcements.add')}</button>} />}
      {data && data.length > 0 && (
        <div className="overflow-x-auto rounded-xl border border-line bg-surface">
          <table className="w-full text-left text-sm">
            <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted"><tr><Th>{t('announcements.colTitle')}</Th><Th>{t('announcements.colKind')}</Th><Th>{t('announcements.colPlacement')}</Th><Th>{t('common.status')}</Th><Th className="text-right">{t('announcements.colPriority')}</Th><Th className="text-right">{t('common.actions')}</Th></tr></thead>
            <tbody className="divide-y divide-line">
              {data.map((a) => <tr key={a.id} className="transition-colors hover:bg-surface-2"><Td><div className="font-medium">{a.title}</div><div className="mt-1 max-w-xl truncate text-xs text-muted">{a.body}</div></Td><Td>{a.kind}</Td><Td>{a.placement}</Td><Td><StatusBadge enabled={a.status === 'published'} /></Td><Td className="text-right tabular-nums">{a.priority}</Td><Td className="text-right"><button className="mr-3 inline-flex min-h-10 items-center rounded-md px-2 text-muted hover:bg-surface-2 hover:text-ink" onClick={() => setEditing(a)}>{t('common.edit')}</button><button className="inline-flex min-h-10 items-center rounded-md px-2 btn-danger" onClick={() => remove(a)}>{t('common.delete')}</button></Td></tr>)}
            </tbody>
          </table>
        </div>
      )}
      {(save.error || del.error) && <div className="mt-3"><ErrorNotice>{((save.error || del.error) as ApiError)?.message ?? t('common.actionFailed')}</ErrorNotice></div>}
      {editing && <Modal title={editing === 'new' ? t('announcements.add') : t('announcements.edit')} onClose={() => setEditing(null)}><AnnouncementForm row={editing === 'new' ? undefined : editing} submitting={save.isPending} onCancel={() => setEditing(null)} onSubmit={(input) => save.mutate(input, { onSuccess: () => setEditing(null) })} /></Modal>}
    </SettingsLayout>
  )
}

function AnnouncementForm({ row, submitting, onCancel, onSubmit }: { row?: Announcement; submitting: boolean; onCancel: () => void; onSubmit: (input: AnnouncementInput) => void }) {
  const { t } = useI18n()
  const [title, setTitle] = useState(row?.title ?? '')
  const [body, setBody] = useState(row?.body ?? '')
  const [kind, setKind] = useState(row?.kind ?? 'info')
  const [status, setStatus] = useState(row?.status ?? 'draft')
  const [placement, setPlacement] = useState(row?.placement ?? 'portal_home')
  const [priority, setPriority] = useState(String(row?.priority ?? 0))
  const [dismissible, setDismissible] = useState(row?.dismissible ?? true)
  function submit(e: React.FormEvent) { e.preventDefault(); onSubmit({ id: row?.id, title: title.trim(), body: body.trim(), kind, status, placement, priority: Number(priority) || 0, starts_at: row?.starts_at ?? null, ends_at: row?.ends_at ?? null, dismissible }) }
  return <form className="space-y-4" onSubmit={submit}><Field label={t('announcements.fieldTitle')}><input className="field" value={title} onChange={(e) => setTitle(e.target.value)} autoFocus /></Field><Field label={t('announcements.fieldBody')}><textarea className="field min-h-28" value={body} onChange={(e) => setBody(e.target.value)} /></Field><div className="grid gap-3 sm:grid-cols-3"><Select label={t('announcements.fieldKind')} value={kind} onChange={setKind} options={['info', 'maintenance', 'pricing', 'model']} /><Select label={t('announcements.fieldStatus')} value={status} onChange={setStatus} options={['draft', 'published', 'archived']} /><Select label={t('announcements.fieldPlacement')} value={placement} onChange={setPlacement} options={['portal_home', 'login', 'banner']} /></div><div className="grid gap-3 sm:grid-cols-2"><Field label={t('announcements.fieldPriority')}><input className="field" type="number" value={priority} onChange={(e) => setPriority(e.target.value)} /></Field><label className="flex items-center gap-2 pt-7 text-sm text-muted"><input type="checkbox" checked={dismissible} onChange={(e) => setDismissible(e.target.checked)} />{t('announcements.fieldDismissible')}</label></div><div className="flex justify-end gap-2"><button type="button" className="btn btn-secondary" onClick={onCancel}>{t('common.cancel')}</button><button className="btn btn-primary" disabled={submitting}>{submitting ? t('common.saving') : t('common.save')}</button></div></form>
}
function Field({ label, children }: { label: string; children: React.ReactNode }) { return <label className="block space-y-1"><span className="text-sm text-muted">{label}</span>{children}</label> }
function Select({ label, value, onChange, options }: { label: string; value: string; onChange: (v: string) => void; options: string[] }) { return <Field label={label}><select className="field" value={value} onChange={(e) => onChange(e.target.value)}>{options.map((o) => <option key={o} value={o}>{o}</option>)}</select></Field> }
