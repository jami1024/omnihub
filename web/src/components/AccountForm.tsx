import { useState } from 'react'
import { ApiError } from '../lib/api'
import { useGroups } from '../lib/groups'
import {
  useTestAccount,
  useTestAccountById,
  type Account,
  type AccountInput,
  type ModelRedirect,
  type ModelRedirectMatch,
  type TestResult,
} from '../lib/accounts'

// AccountForm drives both create and edit. When `account` is provided it
// pre-fills the metadata for an edit (credentials always start blank —
// the server never returns secret values, so the operator only types
// them to CHANGE them). On create, at least one credential row is
// required; on edit, leaving them all blank keeps the stored secret.

interface CredRow {
  key: string
  value: string
}

export interface AccountFormProps {
  account?: Account
  submitting: boolean
  error?: string | null
  onCancel: () => void
  onSubmit: (input: AccountInput) => void
}

const FIELD =
  'field'

export function AccountForm({
  account,
  submitting,
  error,
  onCancel,
  onSubmit,
}: AccountFormProps) {
  const isEdit = account != null
  const [name, setName] = useState(account?.name ?? '')
  const [provider, setProvider] = useState(account?.provider ?? '')
  const [enabled, setEnabled] = useState(account?.enabled ?? true)
  const [weight, setWeight] = useState(String(account?.weight ?? 100))
  const [priority, setPriority] = useState(String(account?.priority ?? 0))
  const [costMultiplier, setCostMultiplier] = useState(
    String(account?.cost_multiplier ?? 1),
  )
  const [baseURL, setBaseURL] = useState(account?.base_url ?? '')
  const [creds, setCreds] = useState<CredRow[]>([{ key: 'api_key', value: '' }])
  const [failureThreshold, setFailureThreshold] = useState(
    numToStr(account?.circuit_failure_threshold),
  )
  const [openDurationMs, setOpenDurationMs] = useState(
    numToStr(account?.circuit_open_duration_ms),
  )
  const [halfOpenSuccess, setHalfOpenSuccess] = useState(
    numToStr(account?.circuit_half_open_success),
  )
  const { data: groups } = useGroups()
  const [groupID, setGroupID] = useState(account?.group_id != null ? String(account.group_id) : '')
  const [redirects, setRedirects] = useState<ModelRedirect[]>(account?.model_redirects ?? [])
  const [headers, setHeaders] = useState<CredRow[]>(
    account?.custom_headers
      ? Object.entries(account.custom_headers).map(([key, value]) => ({ key, value }))
      : [],
  )
  const [endpoints, setEndpoints] = useState<string[]>(account?.endpoints ?? [])
  const [proxyURL, setProxyURL] = useState(account?.proxy_url ?? '')
  const [healthProbe, setHealthProbe] = useState(
    account?.health_probe_enabled == null ? '' : account.health_probe_enabled ? 'true' : 'false',
  )
  const [dailyLimit, setDailyLimit] = useState(numToStr(account?.daily_usd_limit))
  const [totalLimit, setTotalLimit] = useState(numToStr(account?.total_usd_limit))
  const [localErr, setLocalErr] = useState<string | null>(null)

  function updateRedirect(i: number, patch: Partial<ModelRedirect>) {
    setRedirects((rows) => rows.map((r, j) => (j === i ? { ...r, ...patch } : r)))
  }
  function addRedirect() {
    setRedirects((rows) => [...rows, { match_type: 'exact', source: '', target: '' }])
  }
  function removeRedirect(i: number) {
    setRedirects((rows) => rows.filter((_, j) => j !== i))
  }

  function updateHeader(i: number, patch: Partial<CredRow>) {
    setHeaders((rows) => rows.map((r, j) => (j === i ? { ...r, ...patch } : r)))
  }
  function addHeader() {
    setHeaders((rows) => [...rows, { key: '', value: '' }])
  }
  function removeHeader(i: number) {
    setHeaders((rows) => rows.filter((_, j) => j !== i))
  }

  function updateEndpoint(i: number, value: string) {
    setEndpoints((rows) => rows.map((r, j) => (j === i ? value : r)))
  }
  function addEndpoint() {
    setEndpoints((rows) => [...rows, ''])
  }
  function removeEndpoint(i: number) {
    setEndpoints((rows) => rows.filter((_, j) => j !== i))
  }

  const test = useTestAccount()
  const testById = useTestAccountById()
  const testing = test.isPending || testById.isPending
  const testResult: TestResult | undefined = test.data ?? testById.data
  const testErr = test.error ?? testById.error

  // handleTest probes connectivity without saving. If the form carries
  // credentials we test those exact values; otherwise (editing without
  // re-entering the secret) we test the stored account by id.
  function handleTest() {
    setLocalErr(null)
    test.reset()
    testById.reset()
    const credentials: Record<string, string> = {}
    for (const row of creds) {
      const k = row.key.trim()
      if (k && row.value) credentials[k] = row.value
    }
    if (!provider.trim()) {
      setLocalErr('Choose a provider before testing.')
      return
    }
    if (Object.keys(credentials).length > 0) {
      test.mutate({ provider: provider.trim(), base_url: baseURL.trim(), credentials })
    } else if (isEdit && account) {
      testById.mutate(account.id)
    } else {
      setLocalErr('Enter the API key to test the connection.')
    }
  }

  function updateCred(i: number, patch: Partial<CredRow>) {
    setCreds((rows) => rows.map((r, j) => (j === i ? { ...r, ...patch } : r)))
  }
  function addCred() {
    setCreds((rows) => [...rows, { key: '', value: '' }])
  }
  function removeCred(i: number) {
    setCreds((rows) => rows.filter((_, j) => j !== i))
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setLocalErr(null)

    if (!name.trim() || !provider.trim()) {
      setLocalErr('Name and provider are required.')
      return
    }

    // Collapse the credential rows into a map, dropping blank keys. A
    // row with a key but no value is a mistake worth flagging.
    const credentials: Record<string, string> = {}
    for (const row of creds) {
      const k = row.key.trim()
      if (!k) continue
      if (!row.value) {
        setLocalErr(`Credential "${k}" has no value.`)
        return
      }
      credentials[k] = row.value
    }
    const hasCreds = Object.keys(credentials).length > 0

    if (!isEdit && !hasCreds) {
      setLocalErr('At least one credential (e.g. api_key) is required.')
      return
    }

    // Trim redirect rows and drop blank ones; flag a half-filled row.
    const cleanRedirects: ModelRedirect[] = []
    for (const r of redirects) {
      const source = r.source.trim()
      const target = r.target.trim()
      if (!source && !target) continue
      if (!source || !target) {
        setLocalErr('A model redirect needs both a source and a target.')
        return
      }
      cleanRedirects.push({ match_type: r.match_type, source, target })
    }

    // Collapse header rows into a map, dropping rows with a blank name.
    const cleanHeaders: Record<string, string> = {}
    for (const row of headers) {
      const k = row.key.trim()
      if (!k) continue
      cleanHeaders[k] = row.value
    }

    const input: AccountInput = {
      name: name.trim(),
      provider: provider.trim(),
      enabled,
      weight: parseIntOr(weight, 100),
      priority: parseIntOr(priority, 0),
      cost_multiplier: parseFloatOr(costMultiplier, 1),
      base_url: baseURL.trim(),
      // Omit credentials on edit when untouched so the server keeps the
      // stored secret; always send them on create.
      credentials: hasCreds ? credentials : undefined,
      circuit_failure_threshold: strToNum(failureThreshold),
      circuit_open_duration_ms: strToNum(openDurationMs),
      circuit_half_open_success: strToNum(halfOpenSuccess),
      model_redirects: cleanRedirects,
      daily_usd_limit: strToNum(dailyLimit),
      total_usd_limit: strToNum(totalLimit),
      group_id: groupID === '' ? null : Number(groupID),
      custom_headers: cleanHeaders,
      endpoints: endpoints.map((e) => e.trim()).filter((e) => e !== ''),
      health_probe_enabled: healthProbe === '' ? null : healthProbe === 'true',
      proxy_url: proxyURL.trim(),
    }
    onSubmit(input)
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <div className="grid grid-cols-2 gap-4">
        <Field label="Name">
          <input className={FIELD} value={name} onChange={(e) => setName(e.target.value)} autoFocus />
        </Field>
        <Field label="Provider">
          <input
            className={FIELD}
            value={provider}
            onChange={(e) => setProvider(e.target.value)}
            placeholder="anthropic, openai, …"
          />
        </Field>
      </div>

      <Field label="Base URL (optional)">
        <input
          className={FIELD}
          value={baseURL}
          onChange={(e) => setBaseURL(e.target.value)}
          placeholder="leave blank for the provider default"
        />
      </Field>

      <details className="rounded-lg border border-line p-3" open={endpoints.length > 0 || proxyURL !== ''}>
        <summary className="cursor-pointer text-sm text-muted">Network: failover & proxy (optional)</summary>
        <div className="mt-3">
          <Field label="Outbound proxy URL">
            <input
              className={FIELD}
              value={proxyURL}
              onChange={(e) => setProxyURL(e.target.value)}
              placeholder="http://, https://, socks5:// — blank for direct"
            />
          </Field>
        </div>
        <p className="mt-3 text-xs text-muted">
          Failover endpoints: additional base URLs (same credentials) tried in order after the Base
          URL when a request fails with a transport error or a retriable status (5xx / 429), before
          failing over to another account.
        </p>
        <div className="mt-3 space-y-2">
          {endpoints.map((url, i) => (
            <div key={i} className="flex gap-2">
              <input
                className={FIELD + ' flex-1'}
                value={url}
                onChange={(e) => updateEndpoint(i, e.target.value)}
                placeholder="https://backup.example.com"
              />
              <button type="button" onClick={() => removeEndpoint(i)} className="btn btn-secondary px-2">
                ✕
              </button>
            </div>
          ))}
          <button type="button" onClick={addEndpoint} className="text-sm text-muted hover:text-ink">
            + add endpoint
          </button>
        </div>
      </details>

      <div className="grid grid-cols-3 gap-4">
        <Field label="Weight">
          <input className={FIELD} type="number" value={weight} onChange={(e) => setWeight(e.target.value)} />
        </Field>
        <Field label="Priority">
          <input className={FIELD} type="number" value={priority} onChange={(e) => setPriority(e.target.value)} />
        </Field>
        <Field label="Cost multiplier">
          <input
            className={FIELD}
            type="number"
            step="0.1"
            value={costMultiplier}
            onChange={(e) => setCostMultiplier(e.target.value)}
          />
        </Field>
      </div>

      <div className="grid grid-cols-2 gap-4">
        <Field label="Group (optional)">
          <select className={FIELD} value={groupID} onChange={(e) => setGroupID(e.target.value)}>
            <option value="">Ungrouped</option>
            {(groups ?? []).map((g) => (
              <option key={g.id} value={g.id}>
                {g.name} (×{g.cost_multiplier})
              </option>
            ))}
          </select>
        </Field>
        <Field label="Active health probe">
          <select className={FIELD} value={healthProbe} onChange={(e) => setHealthProbe(e.target.value)}>
            <option value="">Inherit default</option>
            <option value="true">Enabled</option>
            <option value="false">Disabled</option>
          </select>
        </Field>
      </div>

      <label className="flex items-center gap-2 text-sm">
        <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
        Enabled (routable)
      </label>

      <fieldset className="space-y-2">
        <legend className="text-sm font-medium">Credentials</legend>
        {isEdit && account!.credential_keys.length > 0 && (
          <p className="text-xs text-muted">
            Currently set: {account!.credential_keys.join(', ')}. Re-enter only to change them;
            leave blank to keep.
          </p>
        )}
        {creds.map((row, i) => (
          <div key={i} className="flex gap-2">
            <input
              className={FIELD + ' flex-1'}
              value={row.key}
              onChange={(e) => updateCred(i, { key: e.target.value })}
              placeholder="key (e.g. api_key)"
            />
            <input
              className={FIELD + ' flex-1'}
              type="password"
              value={row.value}
              onChange={(e) => updateCred(i, { value: e.target.value })}
              placeholder="value"
            />
            <button
              type="button"
              onClick={() => removeCred(i)}
              className="btn btn-secondary px-2"
            >
              ✕
            </button>
          </div>
        ))}
        <button
          type="button"
          onClick={addCred}
          className="text-sm text-muted hover:text-ink"
        >
          + add credential
        </button>
      </fieldset>

      <details className="rounded-lg border border-line p-3" open={redirects.length > 0}>
        <summary className="cursor-pointer text-sm text-muted">
          Model redirects (optional)
        </summary>
        <p className="mt-2 text-xs text-muted">
          Rewrite the requested model to a different upstream model before sending. Rules run
          top-to-bottom; the first match wins.
        </p>
        <div className="mt-3 space-y-2">
          {redirects.map((row, i) => (
            <div key={i} className="flex gap-2">
              <select
                className={FIELD + ' w-28'}
                value={row.match_type}
                onChange={(e) => updateRedirect(i, { match_type: e.target.value as ModelRedirectMatch })}
              >
                <option value="exact">exact</option>
                <option value="prefix">prefix</option>
                <option value="suffix">suffix</option>
                <option value="contains">contains</option>
                <option value="regex">regex</option>
              </select>
              <input
                className={FIELD + ' flex-1'}
                value={row.source}
                onChange={(e) => updateRedirect(i, { source: e.target.value })}
                placeholder="source (requested)"
              />
              <span className="self-center text-muted">→</span>
              <input
                className={FIELD + ' flex-1'}
                value={row.target}
                onChange={(e) => updateRedirect(i, { target: e.target.value })}
                placeholder="target (upstream)"
              />
              <button type="button" onClick={() => removeRedirect(i)} className="btn btn-secondary px-2">
                ✕
              </button>
            </div>
          ))}
          <button type="button" onClick={addRedirect} className="text-sm text-muted hover:text-ink">
            + add redirect
          </button>
        </div>
      </details>

      <details className="rounded-lg border border-line p-3" open={headers.length > 0}>
        <summary className="cursor-pointer text-sm text-muted">
          Custom headers (optional)
        </summary>
        <p className="mt-2 text-xs text-muted">
          Extra HTTP headers sent on every upstream request for this account (org id, beta flags,
          routing hints). Forwarded-for and encoding headers are always enforced by the gateway.
        </p>
        <div className="mt-3 space-y-2">
          {headers.map((row, i) => (
            <div key={i} className="flex gap-2">
              <input
                className={FIELD + ' flex-1'}
                value={row.key}
                onChange={(e) => updateHeader(i, { key: e.target.value })}
                placeholder="header name (e.g. X-Org-Id)"
              />
              <input
                className={FIELD + ' flex-1'}
                value={row.value}
                onChange={(e) => updateHeader(i, { value: e.target.value })}
                placeholder="value"
              />
              <button type="button" onClick={() => removeHeader(i)} className="btn btn-secondary px-2">
                ✕
              </button>
            </div>
          ))}
          <button type="button" onClick={addHeader} className="text-sm text-muted hover:text-ink">
            + add header
          </button>
        </div>
      </details>

      <details className="rounded-lg border border-line p-3" open={dailyLimit !== '' || totalLimit !== ''}>
        <summary className="cursor-pointer text-sm text-muted">
          Spend caps (optional)
        </summary>
        <p className="mt-2 text-xs text-muted">
          Stop routing to this account once its spend reaches a cap. Daily is a rolling 24-hour
          window; total is lifetime. Leave blank for no cap.
        </p>
        <div className="mt-3 grid grid-cols-2 gap-4">
          <Field label="Daily USD limit">
            <input
              className={FIELD}
              type="number"
              step="0.01"
              min="0"
              value={dailyLimit}
              onChange={(e) => setDailyLimit(e.target.value)}
              placeholder="no cap"
            />
          </Field>
          <Field label="Total USD limit">
            <input
              className={FIELD}
              type="number"
              step="0.01"
              min="0"
              value={totalLimit}
              onChange={(e) => setTotalLimit(e.target.value)}
              placeholder="no cap"
            />
          </Field>
        </div>
      </details>

      <details className="rounded-lg border border-line p-3">
        <summary className="cursor-pointer text-sm text-muted">
          Circuit-breaker overrides (optional)
        </summary>
        <div className="mt-3 grid grid-cols-3 gap-4">
          <Field label="Failure threshold">
            <input
              className={FIELD}
              type="number"
              value={failureThreshold}
              onChange={(e) => setFailureThreshold(e.target.value)}
              placeholder="default"
            />
          </Field>
          <Field label="Open duration (ms)">
            <input
              className={FIELD}
              type="number"
              value={openDurationMs}
              onChange={(e) => setOpenDurationMs(e.target.value)}
              placeholder="default"
            />
          </Field>
          <Field label="Half-open success">
            <input
              className={FIELD}
              type="number"
              value={halfOpenSuccess}
              onChange={(e) => setHalfOpenSuccess(e.target.value)}
              placeholder="default"
            />
          </Field>
        </div>
      </details>

      {(localErr || error) && (
        <p className="text-sm text-danger">{localErr ?? error}</p>
      )}

      {(testResult || testErr) && (
        <TestVerdict
          result={testResult}
          error={testErr ? (testErr instanceof ApiError ? testErr.message : 'Test failed.') : null}
        />
      )}

      <div className="flex items-center justify-between gap-2">
        <button
          type="button"
          onClick={handleTest}
          disabled={testing}
          className="btn btn-secondary"
        >
          {testing ? 'Testing…' : 'Test connection'}
        </button>
        <div className="flex gap-2">
        <button
          type="button"
          onClick={onCancel}
          className="btn btn-secondary"
        >
          Cancel
        </button>
        <button
          type="submit"
          disabled={submitting}
          className="btn btn-primary"
        >
          {submitting ? 'Saving…' : isEdit ? 'Save changes' : 'Create account'}
        </button>
        </div>
      </div>
    </form>
  )
}

// TestVerdict renders the traffic-light connectivity result inline.
function TestVerdict({ result, error }: { result?: TestResult; error: string | null }) {
  if (error) {
    return (
      <div className="flex items-center gap-2 rounded-lg border border-line bg-surface-2 px-3 py-2 text-sm">
        <span className="inline-block h-2.5 w-2.5 rounded-full bg-danger" />
        <span className="text-danger">{error}</span>
      </div>
    )
  }
  if (!result) return null
  const tone =
    result.status === 'green'
      ? 'bg-emerald-500'
      : result.status === 'yellow'
        ? 'bg-amber-500'
        : 'bg-danger'
  return (
    <div className="flex items-center gap-2 rounded-lg border border-line bg-surface-2 px-3 py-2 text-sm">
      <span className={`inline-block h-2.5 w-2.5 rounded-full ${tone}`} />
      <span className="text-ink">{result.message}</span>
      <span className="text-muted">
        {result.http_status ? `HTTP ${result.http_status} · ` : ''}
        {result.latency_ms}ms
      </span>
    </div>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block space-y-1">
      <span className="text-sm text-muted">{label}</span>
      {children}
    </label>
  )
}

function numToStr(n: number | null | undefined): string {
  return n == null ? '' : String(n)
}
function strToNum(s: string): number | null {
  const t = s.trim()
  if (t === '') return null
  const n = Number(t)
  return Number.isFinite(n) ? n : null
}
function parseIntOr(s: string, def: number): number {
  const n = parseInt(s, 10)
  return Number.isFinite(n) ? n : def
}
function parseFloatOr(s: string, def: number): number {
  const n = parseFloat(s)
  return Number.isFinite(n) ? n : def
}
