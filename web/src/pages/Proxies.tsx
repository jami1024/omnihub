import { useState } from 'react'
import { Layout } from '../components/Layout'
import { Modal } from '../components/Modal'
import { EmptyState, ErrorNotice, LoadingTable, MetricStrip, PageHeader } from '../components/PageChrome'
import { Td, Th } from '../components/Table'
import { ApiError } from '../lib/api'
import { useI18n } from '../lib/i18n'
import {
  useCreateProxy,
  useDeleteProxy,
  useImportProxies,
  useProxies,
  useTestProxy,
  useUpdateProxy,
  type Proxy,
  type ProxyImportResult,
  type ProxyInput,
} from '../lib/proxies'

type Editing = 'new' | 'import' | Proxy | null

const FIELD = 'w-full rounded-lg border border-line bg-surface px-3 py-2 text-sm outline-none focus:border-ink'

export function ProxiesPage() {
  const { t } = useI18n()
  const { data: proxies, isLoading, error } = useProxies()
  const create = useCreateProxy()
  const update = useUpdateProxy()
  const del = useDeleteProxy()
  const [editing, setEditing] = useState<Editing>(null)
  const [formErr, setFormErr] = useState<string | null>(null)

  const count = proxies?.length ?? 0
  const active = proxies?.filter((p) => p.status === 'active').length ?? 0

  function close() {
    setEditing(null)
    setFormErr(null)
  }

  function submit(input: ProxyInput) {
    setFormErr(null)
    const onError = (e: unknown) =>
      setFormErr(e instanceof ApiError ? e.message : t('proxies.saveError'))
    if (editing === 'new') {
      create.mutate(input, { onSuccess: close, onError })
    } else if (editing && editing !== 'import') {
      update.mutate({ id: editing.id, input }, { onSuccess: close, onError })
    }
  }

  function remove(p: Proxy) {
    if (!confirm(t('proxies.deleteConfirm', { name: p.name }))) return
    del.mutate(p.id)
  }

  return (
    <Layout>
      <main className="mx-auto w-full max-w-7xl px-6 py-8">
        <PageHeader
          eyebrow={t('proxies.eyebrow')}
          context={t('proxies.context')}
          title={t('proxies.title')}
          description={t('proxies.description')}
          action={
            <div className="flex items-center gap-2">
              <button onClick={() => { setFormErr(null); setEditing('import') }} className="btn btn-secondary h-10">
                {t('proxies.import')}
              </button>
              <button onClick={() => { setFormErr(null); setEditing('new') }} className="btn btn-primary h-10">
                {t('proxies.newProxy')}
              </button>
            </div>
          }
        />

        <MetricStrip
          metrics={[
            { label: t('proxies.total'), value: count },
            { label: t('common.enabled'), value: active },
          ]}
        />

        <div className="mt-6" />

        {isLoading && <LoadingTable />}
        {error && (
          <ErrorNotice>{error instanceof ApiError ? error.message : t('proxies.loadError')}</ErrorNotice>
        )}

        {proxies && proxies.length === 0 && (
          <EmptyState
            eyebrow={t('proxies.emptyEyebrow')}
            title={t('proxies.emptyTitle')}
            description={t('proxies.emptyDescription')}
            action={
              <button onClick={() => setEditing('new')} className="btn btn-primary h-10">
                {t('proxies.newProxy')}
              </button>
            }
          />
        )}

        {proxies && proxies.length > 0 && (
          <div className="overflow-x-auto rounded-xl border border-line bg-surface">
            <table className="w-full text-left text-sm">
              <thead className="border-b border-line bg-surface-2 text-xs uppercase tracking-wide text-muted">
                <tr>
                  <Th>{t('common.name')}</Th>
                  <Th>{t('proxies.endpoint')}</Th>
                  <Th>{t('common.status')}</Th>
                  <Th>{t('proxies.health')}</Th>
                  <Th>{t('proxies.fallback')}</Th>
                  <Th className="text-right">{t('common.actions')}</Th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line">
                {proxies.map((p) => (
                  <tr key={p.id} className="transition-colors hover:bg-surface-2">
                    <Td className="font-medium">{p.name}</Td>
                    <Td className="font-mono text-xs text-muted">
                      {p.protocol}://{p.username ? `${p.username}@` : ''}{p.host}:{p.port}
                    </Td>
                    <Td className={p.status === 'active' ? '' : 'text-muted'}>{p.status}</Td>
                    <Td>
                      <HealthCell proxy={p} />
                    </Td>
                    <Td className="text-muted">{p.fallback_mode}</Td>
                    <Td className="text-right">
                      <RowTest id={p.id} />
                      <button
                        onClick={() => { setFormErr(null); setEditing(p) }}
                        className="mr-1 inline-flex min-h-10 items-center rounded-md px-2 text-muted underline-offset-4 hover:bg-surface-2 hover:text-ink hover:underline sm:mr-3 sm:px-1"
                      >
                        {t('common.edit')}
                      </button>
                      <button
                        onClick={() => remove(p)}
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
            <ErrorNotice>{del.error instanceof ApiError ? del.error.message : t('proxies.deleteError')}</ErrorNotice>
          </div>
        )}

        {editing === 'import' && (
          <Modal title={t('proxies.importTitle')} onClose={close}>
            <ImportForm onClose={close} />
          </Modal>
        )}

        {editing && editing !== 'import' && (
          <Modal title={editing === 'new' ? t('proxies.newProxy') : t('proxies.editTitle', { name: editing.name })} onClose={close}>
            <ProxyForm
              proxy={editing === 'new' ? undefined : editing}
              others={(proxies ?? []).filter((p) => typeof editing === 'string' || p.id !== editing.id)}
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

// ImportForm bulk-creates proxies from pasted lines (one per line).
function ImportForm({ onClose }: { onClose: () => void }) {
  const { t } = useI18n()
  const importProxies = useImportProxies()
  const [text, setText] = useState('')
  const [proto, setProto] = useState('http')
  const [result, setResult] = useState<ProxyImportResult | null>(null)
  const [err, setErr] = useState<string | null>(null)

  function handleImport() {
    setErr(null)
    setResult(null)
    const lines = text.split('\n').map((l) => l.trim()).filter((l) => l !== '')
    if (lines.length === 0) {
      setErr(t('proxies.importEmpty'))
      return
    }
    importProxies.mutate(
      { proxies: lines, default_protocol: proto },
      {
        onSuccess: (r) => setResult(r),
        onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : t('proxies.saveError')),
      },
    )
  }

  return (
    <div className="space-y-4">
      <p className="text-xs text-muted">{t('proxies.importHelp')}</p>
      <textarea
        className={FIELD + ' h-40 font-mono text-xs'}
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder={'socks5://user:pass@1.2.3.4:1080\n5.6.7.8:8080:user:pass\n9.9.9.9:3128'}
        spellCheck={false}
      />
      <label className="flex items-center gap-2 text-sm">
        <span className="text-muted">{t('proxies.importDefaultProtocol')}</span>
        <select className={FIELD + ' w-40'} value={proto} onChange={(e) => setProto(e.target.value)}>
          <option value="http">http</option>
          <option value="https">https</option>
          <option value="socks5">socks5</option>
          <option value="socks5h">socks5h</option>
        </select>
      </label>

      {err && <ErrorNotice>{err}</ErrorNotice>}
      {result && (
        <ErrorNotice>
          {t('proxies.importDone', {
            created: result.created,
            skipped: result.skipped,
            failed: result.failed,
          })}
        </ErrorNotice>
      )}

      <div className="flex justify-end gap-2">
        <button type="button" onClick={onClose} className="btn btn-secondary h-10">
          {result ? t('common.done') : t('common.cancel')}
        </button>
        <button
          type="button"
          onClick={handleImport}
          disabled={importProxies.isPending}
          className="btn btn-primary h-10 disabled:opacity-50"
        >
          {importProxies.isPending ? t('common.saving') : t('proxies.import')}
        </button>
      </div>
    </div>
  )
}

// HealthCell shows the background prober's verdict: a green/red dot with
// latency, or "—" when the proxy hasn't been probed yet.
function HealthCell({ proxy }: { proxy: Proxy }) {
  const { t } = useI18n()
  if (proxy.healthy == null) {
    return <span className="text-muted">—</span>
  }
  const tone = proxy.healthy ? 'bg-emerald-500' : 'bg-danger'
  return (
    <span className="inline-flex items-center gap-1.5" title={t('proxies.healthHint')}>
      <span className={`inline-block h-2 w-2 rounded-full ${tone}`} />
      <span className="font-mono text-xs text-muted">
        {proxy.healthy ? t('proxies.healthy') : t('proxies.unhealthy')}
        {proxy.latency_ms != null ? ` · ${proxy.latency_ms}ms` : ''}
      </span>
    </span>
  )
}

// RowTest probes a proxy's connectivity through the gateway.
function RowTest({ id }: { id: number }) {
  const { t } = useI18n()
  const test = useTestProxy()
  const r = test.data
  const tone = !r ? 'bg-line' : r.status === 'green' ? 'bg-emerald-500' : 'bg-danger'
  const title = test.error
    ? test.error instanceof ApiError ? test.error.message : t('proxies.testFailed')
    : r ? `${r.message}${r.latency_ms != null ? ` · ${r.latency_ms}ms` : ''}` : t('proxies.testHint')
  return (
    <button
      onClick={() => test.mutate(id)}
      disabled={test.isPending}
      title={title}
      className="mr-3 inline-flex items-center gap-1.5 text-muted underline-offset-4 hover:text-ink hover:underline disabled:opacity-50"
    >
      <span className={`inline-block h-2 w-2 rounded-full ${test.error ? 'bg-danger' : tone}`} />
      {test.isPending ? t('proxies.testing') : t('proxies.test')}
    </button>
  )
}

function ProxyForm({
  proxy,
  others,
  submitting,
  error,
  onCancel,
  onSubmit,
}: {
  proxy?: Proxy
  others: Proxy[]
  submitting: boolean
  error: string | null
  onCancel: () => void
  onSubmit: (input: ProxyInput) => void
}) {
  const { t } = useI18n()
  const isEdit = proxy != null
  const [name, setName] = useState(proxy?.name ?? '')
  const [protocol, setProtocol] = useState(proxy?.protocol ?? 'http')
  const [host, setHost] = useState(proxy?.host ?? '')
  const [port, setPort] = useState(String(proxy?.port ?? 1080))
  const [username, setUsername] = useState(proxy?.username ?? '')
  const [password, setPassword] = useState('')
  const [status, setStatus] = useState(proxy?.status ?? 'active')
  const [expiresAt, setExpiresAt] = useState(
    proxy?.expires_at ? new Date(proxy.expires_at * 1000).toISOString().slice(0, 10) : '',
  )
  const [fallbackMode, setFallbackMode] = useState(proxy?.fallback_mode ?? 'none')
  const [backupProxyID, setBackupProxyID] = useState(
    proxy?.backup_proxy_id != null ? String(proxy.backup_proxy_id) : '',
  )
  const [localErr, setLocalErr] = useState<string | null>(null)

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setLocalErr(null)
    const p = Number(port)
    if (!name.trim() || !host.trim()) {
      setLocalErr(t('proxies.nameHostRequired'))
      return
    }
    if (!Number.isInteger(p) || p < 1 || p > 65535) {
      setLocalErr(t('proxies.portInvalid'))
      return
    }
    if (fallbackMode === 'proxy' && backupProxyID === '') {
      setLocalErr(t('proxies.backupRequired'))
      return
    }
    const input: ProxyInput = {
      name: name.trim(),
      protocol,
      host: host.trim(),
      port: p,
      username: username.trim(),
      // Omit password on edit when left blank (keeps stored secret);
      // always send on create.
      password: password !== '' ? password : isEdit ? undefined : '',
      status,
      expires_at: expiresAt ? Math.floor(new Date(expiresAt + 'T00:00:00Z').getTime() / 1000) : null,
      fallback_mode: fallbackMode,
      backup_proxy_id: fallbackMode === 'proxy' && backupProxyID !== '' ? Number(backupProxyID) : null,
    }
    onSubmit(input)
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <div className="grid grid-cols-2 gap-4">
        <label className="block space-y-1">
          <span className="text-sm text-muted">{t('common.name')}</span>
          <input className={FIELD} value={name} onChange={(e) => setName(e.target.value)} autoFocus />
        </label>
        <label className="block space-y-1">
          <span className="text-sm text-muted">{t('proxies.protocol')}</span>
          <select className={FIELD} value={protocol} onChange={(e) => setProtocol(e.target.value)}>
            <option value="http">http</option>
            <option value="https">https</option>
            <option value="socks5">socks5</option>
            <option value="socks5h">socks5h</option>
          </select>
        </label>
      </div>

      <div className="grid grid-cols-3 gap-4">
        <label className="col-span-2 block space-y-1">
          <span className="text-sm text-muted">{t('proxies.host')}</span>
          <input className={FIELD} value={host} onChange={(e) => setHost(e.target.value)} placeholder="proxy.example.com" />
        </label>
        <label className="block space-y-1">
          <span className="text-sm text-muted">{t('proxies.port')}</span>
          <input className={FIELD} type="number" value={port} onChange={(e) => setPort(e.target.value)} />
        </label>
      </div>

      <div className="grid grid-cols-2 gap-4">
        <label className="block space-y-1">
          <span className="text-sm text-muted">{t('proxies.username')}</span>
          <input className={FIELD} value={username} onChange={(e) => setUsername(e.target.value)} />
        </label>
        <label className="block space-y-1">
          <span className="text-sm text-muted">{t('proxies.password')}</span>
          <input
            className={FIELD}
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder={isEdit && proxy?.has_password ? t('proxies.passwordKeep') : ''}
          />
        </label>
      </div>

      <div className="grid grid-cols-2 gap-4">
        <label className="block space-y-1">
          <span className="text-sm text-muted">{t('common.status')}</span>
          <select className={FIELD} value={status} onChange={(e) => setStatus(e.target.value)}>
            <option value="active">{t('proxies.statusActive')}</option>
            <option value="disabled">{t('proxies.statusDisabled')}</option>
          </select>
        </label>
        <label className="block space-y-1">
          <span className="text-sm text-muted">{t('proxies.expiresAt')}</span>
          <input className={FIELD} type="date" value={expiresAt} onChange={(e) => setExpiresAt(e.target.value)} />
        </label>
      </div>

      <div className="grid grid-cols-2 gap-4">
        <label className="block space-y-1">
          <span className="text-sm text-muted">{t('proxies.fallbackMode')}</span>
          <select className={FIELD} value={fallbackMode} onChange={(e) => setFallbackMode(e.target.value)}>
            <option value="none">{t('proxies.fallbackNone')}</option>
            <option value="direct">{t('proxies.fallbackDirect')}</option>
            <option value="proxy">{t('proxies.fallbackProxy')}</option>
          </select>
        </label>
        {fallbackMode === 'proxy' && (
          <label className="block space-y-1">
            <span className="text-sm text-muted">{t('proxies.backupProxy')}</span>
            <select className={FIELD} value={backupProxyID} onChange={(e) => setBackupProxyID(e.target.value)}>
              <option value="">—</option>
              {others.map((p) => (
                <option key={p.id} value={p.id}>{p.name}</option>
              ))}
            </select>
          </label>
        )}
      </div>

      <p className="text-xs text-muted">{t('proxies.fallbackHelp')}</p>

      {(localErr || error) && <ErrorNotice>{localErr || error}</ErrorNotice>}

      <div className="flex justify-end gap-2">
        <button type="button" onClick={onCancel} className="btn btn-secondary h-10">
          {t('common.cancel')}
        </button>
        <button type="submit" disabled={submitting} className="btn btn-primary h-10 disabled:opacity-50">
          {submitting ? t('common.saving') : t('common.save')}
        </button>
      </div>
    </form>
  )
}
