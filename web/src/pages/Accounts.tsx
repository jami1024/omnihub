import { useRef, useState } from 'react'
import { Layout } from '../components/Layout'
import { Modal } from '../components/Modal'
import { EmptyState, ErrorNotice, LoadingTable, MetricStrip, PageHeader } from '../components/PageChrome'
import { StatusBadge, Td, Th } from '../components/Table'
import { AccountForm, type ImportRequest } from '../components/AccountForm'
import { ApiError } from '../lib/api'
import { useI18n } from '../lib/i18n'
import {
  useAccountQuota,
  useAccounts,
  useCreateAccount,
  useDeleteAccount,
  useExportAccounts,
  useImportAccounts,
  useImportCredentials,
  useTestAccountById,
  useUpdateAccount,
  type Account,
  type AccountInput,
  type ImportResult,
} from '../lib/accounts'

// editing tracks which dialog (if any) is open: 'new' for the create
// form, an Account for an edit, or null for the table view.
type Editing = 'new' | Account | null

export function AccountsPage() {
  const { t } = useI18n()
  const { data: accounts, isLoading, error } = useAccounts()
  const create = useCreateAccount()
  const update = useUpdateAccount()
  const del = useDeleteAccount()
  const importCreds = useImportCredentials()
  const exportAccounts = useExportAccounts()
  const importAccounts = useImportAccounts()
  const fileInput = useRef<HTMLInputElement>(null)
  const [editing, setEditing] = useState<Editing>(null)
  const [formErr, setFormErr] = useState<string | null>(null)
  const [importMsg, setImportMsg] = useState<string | null>(null)
  const accountCount = accounts?.length ?? 0
  const enabledCount = accounts?.filter((a) => a.enabled).length ?? 0
  const providerCount = accounts ? new Set(accounts.map((a) => a.provider)).size : 0
  const credentialCount = accounts?.reduce((sum, a) => sum + a.credential_keys.length, 0) ?? 0

  function openNew() {
    setFormErr(null)
    setEditing('new')
  }
  function openEdit(a: Account) {
    setFormErr(null)
    setEditing(a)
  }
  function close() {
    setEditing(null)
    setFormErr(null)
  }

  function handleSubmit(input: AccountInput, importReq?: ImportRequest) {
    setFormErr(null)
    const onError = (err: unknown) =>
      setFormErr(err instanceof ApiError ? err.message : t('accounts.requestFailed'))
    // OAuth accounts save in two steps: persist the row, then run the
    // pasted credential file through the auth plugin. A failed import
    // keeps the dialog open — the row exists but cannot route until
    // credentials are imported (re-submit imports again, it won't
    // duplicate the account on edit).
    const runImport = (id: number) => {
      if (!importReq) {
        close()
        return
      }
      importCreds.mutate(
        { id, ...importReq },
        {
          onSuccess: close,
          onError: (err: unknown) =>
            setFormErr(
              t('accounts.importFailed', {
                msg: err instanceof ApiError ? err.message : t('accounts.requestFailed'),
              }),
            ),
        },
      )
    }
    if (editing === 'new') {
      create.mutate(input, {
        onSuccess: (a) => {
          // Switch the dialog to edit mode immediately: if the import
          // below fails, re-submitting must UPDATE the just-created row
          // instead of trying to create it again (409 name_taken).
          if (importReq) setEditing(a)
          runImport(a.id)
        },
        onError,
      })
    } else if (editing) {
      const id = editing.id
      update.mutate({ id, input }, { onSuccess: () => runImport(id), onError })
    }
  }

  function handleDelete(a: Account) {
    if (!confirm(t('accounts.deleteConfirm', { name: a.name }))) return
    del.mutate(a.id)
  }

  function handleExport() {
    setImportMsg(null)
    if (!confirm(t('accounts.exportConfirm'))) return
    exportAccounts.mutate()
  }

  function handleImportFile(e: React.ChangeEvent<HTMLInputElement>) {
    setImportMsg(null)
    const file = e.target.files?.[0]
    e.target.value = '' // allow re-selecting the same file later
    if (!file) return
    file
      .text()
      .then((text) => {
        const bundle = JSON.parse(text) as { accounts?: unknown[] }
        if (!Array.isArray(bundle.accounts)) {
          throw new Error(t('accounts.importBadFile'))
        }
        importAccounts.mutate(
          { accounts: bundle.accounts },
          {
            onSuccess: (res: ImportResult) =>
              setImportMsg(
                t('accounts.importDone', {
                  created: res.created,
                  skipped: res.skipped,
                  failed: res.failed,
                }),
              ),
            onError: (err: unknown) =>
              setImportMsg(err instanceof ApiError ? err.message : t('accounts.requestFailed')),
          },
        )
      })
      .catch((err: unknown) =>
        setImportMsg(err instanceof Error ? err.message : t('accounts.importBadFile')),
      )
  }

  return (
    <Layout>
      <main className="mx-auto w-full max-w-7xl px-6 py-8">
        <PageHeader
          eyebrow={t('accounts.eyebrow')}
          context={t('accounts.context')}
          title={t('accounts.title')}
          description={t('accounts.description')}
          action={
            <div className="flex items-center gap-2">
              <input
                ref={fileInput}
                type="file"
                accept="application/json,.json"
                className="hidden"
                onChange={handleImportFile}
              />
              <button
                onClick={() => fileInput.current?.click()}
                disabled={importAccounts.isPending}
                className="btn btn-secondary h-10 disabled:opacity-50"
              >
                {importAccounts.isPending ? t('accounts.importing') : t('accounts.import')}
              </button>
              <button
                onClick={handleExport}
                disabled={exportAccounts.isPending || accountCount === 0}
                className="btn btn-secondary h-10 disabled:opacity-50"
              >
                {t('accounts.export')}
              </button>
              <button onClick={openNew} className="btn btn-primary h-10">
                {t('accounts.newAccount')}
              </button>
            </div>
          }
        />

        {importMsg && (
          <div className="mt-4">
            <ErrorNotice>{importMsg}</ErrorNotice>
          </div>
        )}

        <MetricStrip
          metrics={[
            { label: t('accounts.total'), value: accountCount },
            { label: t('common.enabled'), value: enabledCount },
            { label: t('accounts.providers'), value: providerCount },
            { label: t('accounts.secrets'), value: credentialCount },
          ]}
        />

        <div className="mt-6" />

        {isLoading && <LoadingTable />}
        {error && (
          <ErrorNotice>
            {error instanceof ApiError ? error.message : t('accounts.loadError')}
          </ErrorNotice>
        )}

        {accounts && accounts.length === 0 && <EmptyAccounts onCreate={openNew} />}

        {accounts && accounts.length > 0 && (
          <div className="overflow-x-auto rounded-xl border border-line bg-surface">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
                <tr>
                  <Th>{t('common.name')}</Th>
                  <Th>{t('accounts.provider')}</Th>
                  <Th>{t('accounts.auth')}</Th>
                  <Th>{t('common.status')}</Th>
                  <Th className="text-right">{t('accounts.weight')}</Th>
                  <Th className="text-right">{t('accounts.priority')}</Th>
                  <Th className="text-right">{t('accounts.costMultiplierShort')}</Th>
                  <Th>{t('accounts.credentials')}</Th>
                  <Th className="text-right">{t('common.actions')}</Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {accounts.map((a) => (
                  <tr key={a.id} className="transition-colors hover:bg-surface-2">
                    <Td className="font-medium">{a.name}</Td>
                    <Td className="text-muted">{a.provider}</Td>
                    <Td>
                      <AuthCell account={a} />
                    </Td>
                    <Td>
                      <StatusBadge enabled={a.enabled} />
                    </Td>
                    <Td className="text-right tabular-nums">{a.weight}</Td>
                    <Td className="text-right tabular-nums">{a.priority}</Td>
                    <Td className="text-right tabular-nums">{a.cost_multiplier}</Td>
                    <Td className="text-muted">
                      {a.credential_keys.length > 0 ? a.credential_keys.join(', ') : '—'}
                    </Td>
                    <Td className="text-right">
                      <RowTest id={a.id} />
                      {a.auth_type !== 'api_key' && <RowQuota id={a.id} />}
                      <button
                        onClick={() => openEdit(a)}
                        className="mr-1 inline-flex min-h-10 items-center rounded-md px-2 text-muted underline-offset-4 hover:bg-surface-2 hover:text-ink hover:underline sm:mr-3 sm:px-1"
                      >
                        {t('common.edit')}
                      </button>
                      <button
                        onClick={() => handleDelete(a)}
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
            <ErrorNotice>
            {del.error instanceof ApiError ? del.error.message : t('accounts.deleteFailed')}
            </ErrorNotice>
          </div>
        )}
      </main>

      {editing && (
        <Modal title={editing === 'new' ? t('accounts.newAccount') : t('accounts.editTitle', { name: editing.name })} onClose={close}>
          <AccountForm
            account={editing === 'new' ? undefined : editing}
            submitting={create.isPending || update.isPending || importCreds.isPending}
            error={formErr}
            onCancel={close}
            onSubmit={handleSubmit}
          />
        </Modal>
      )}
    </Layout>
  )
}

function AccountsGlyph() {
  const { t } = useI18n()
  return (
    <div className="relative overflow-hidden rounded-lg bg-surface-2 p-5" aria-hidden>
      <svg viewBox="0 0 300 128" className="h-32 w-full text-ink">
        <path
          d="M44 64h58M102 64c22 0 22-34 44-34h24M102 64c22 0 22 34 44 34h24M204 30h54M204 98h54M170 30h34M170 98h34"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.4"
          strokeLinecap="round"
          opacity="0.42"
        />
        <circle cx="44" cy="64" r="13" fill="var(--surface)" stroke="currentColor" strokeWidth="1.4" />
        <circle cx="170" cy="30" r="13" fill="var(--surface)" stroke="currentColor" strokeWidth="1.4" />
        <circle cx="170" cy="98" r="13" fill="var(--surface)" stroke="currentColor" strokeWidth="1.4" />
        <rect x="216" y="17" width="42" height="26" rx="8" fill="var(--surface)" stroke="currentColor" strokeWidth="1.4" />
        <rect x="216" y="85" width="42" height="26" rx="8" fill="var(--surface)" stroke="currentColor" strokeWidth="1.4" />
        <path d="M39 64h10M165 30h10M165 98h10" stroke="var(--brand)" strokeWidth="2" strokeLinecap="round" />
        <circle cx="237" cy="30" r="3" fill="var(--success)" />
        <circle cx="237" cy="98" r="3" fill="var(--brand)" />
      </svg>
      <div className="absolute left-4 top-4 font-mono text-[10px] uppercase tracking-[0.16em] text-muted">
        {t('accounts.routeMap')}
      </div>
    </div>
  )
}

function EmptyAccounts({ onCreate }: { onCreate: () => void }) {
  const { t } = useI18n()
  return (
    <EmptyState
      eyebrow={t('accounts.emptyEyebrow')}
      title={t('accounts.emptyTitle')}
      description={t('accounts.emptyDescription')}
      action={
        <button onClick={onCreate} className="btn btn-primary h-10">
          {t('accounts.newAccount')}
        </button>
      }
      visual={<AccountsGlyph />}
    />
  )
}

// ROUTABLE_AUTH_STATUSES are the auth_status values the resolver still
// routes; anything else means the account is (or will soon be) skipped,
// so the cell flags it. Mirrors the server's status model.
const ROUTABLE_AUTH_STATUSES = new Set(['ok', 'expiring', 'refreshing'])

// AuthCell shows how the account authenticates upstream (auth_type) plus
// a status dot when the runtime auth state is degraded. Identity details
// (email / plan / refresh error) surface in the hover title.
function AuthCell({ account: a }: { account: Account }) {
  const degraded = a.auth_status !== 'ok'
  const tone = !degraded
    ? 'bg-emerald-500'
    : ROUTABLE_AUTH_STATUSES.has(a.auth_status)
      ? 'bg-amber-500'
      : 'bg-danger'
  const title = [a.auth_email, a.auth_plan, a.refresh_error].filter(Boolean).join(' · ')
  return (
    <span className="inline-flex items-center gap-1.5" title={title || undefined}>
      <span className={`inline-block h-2 w-2 rounded-full ${tone}`} />
      <span className="font-mono text-xs text-muted">{a.auth_type}</span>
      {degraded && <span className="font-mono text-xs text-danger">{a.auth_status}</span>}
    </span>
  )
}

// RowQuota queries a subscription account's remaining quota windows
// on demand and shows them inline ("5h 32% · 7d 61%"); the raw upstream
// payload lands in the hover title for shapes the server couldn't
// normalise.
function RowQuota({ id }: { id: number }) {
  const { t } = useI18n()
  const quota = useAccountQuota()
  const windows = quota.data?.windows ?? []
  let label = t('accounts.quota')
  if (quota.isPending) label = t('accounts.quotaLoading')
  else if (windows.length > 0) {
    label = windows.map((w) => `${w.label} ${Math.round(w.used_percent)}%`).join(' · ')
  } else if (quota.data) {
    label = t('accounts.quotaRawOnly')
  }
  const title = quota.error
    ? quota.error instanceof ApiError
      ? quota.error.message
      : t('accounts.quotaFailed')
    : quota.data?.raw
      ? JSON.stringify(quota.data.raw).slice(0, 500)
      : t('accounts.quotaHint')
  return (
    <button
      onClick={() => quota.mutate(id)}
      disabled={quota.isPending}
      title={title}
      className={`mr-3 inline-flex items-center underline-offset-4 hover:underline disabled:opacity-50 ${quota.error ? 'text-danger' : 'text-muted hover:text-ink'}`}
    >
      {label}
    </button>
  )
}

// RowTest is an inline per-row connectivity probe. It tests the account's
// stored credentials and shows a traffic-light dot with the latency /
// message on hover.
function RowTest({ id }: { id: number }) {
  const { t } = useI18n()
  const test = useTestAccountById()
  const r = test.data
  const tone = !r
    ? 'bg-line'
    : r.status === 'green'
      ? 'bg-emerald-500'
      : r.status === 'yellow'
        ? 'bg-amber-500'
        : 'bg-danger'
  const title = test.error
    ? test.error instanceof ApiError
      ? test.error.message
      : t('accounts.testFailed')
    : r
      ? `${r.message}${r.http_status ? ` · HTTP ${r.http_status}` : ''} · ${r.latency_ms}ms`
      : t('accounts.testConnectivity')
  return (
    <button
      onClick={() => test.mutate(id)}
      disabled={test.isPending}
      title={title}
      className="mr-3 inline-flex items-center gap-1.5 text-muted underline-offset-4 hover:text-ink hover:underline disabled:opacity-50"
    >
      <span className={`inline-block h-2 w-2 rounded-full ${test.error ? 'bg-danger' : tone}`} />
      {test.isPending ? t('accounts.testing') : t('accounts.test')}
    </button>
  )
}
