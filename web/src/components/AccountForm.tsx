import { useState } from 'react'
import { ApiError } from '../lib/api'
import { useI18n } from '../lib/i18n'
import { useGroups } from '../lib/groups'
import { useProxies } from '../lib/proxies'
import {
  useAuthPlugins,
  useTestAccount,
  useTestAccountById,
  type Account,
  type AccountInput,
  type ActiveWindow,
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

// ImportRequest carries the pasted CLI credential file that should be
// run through the auth plugin right after the account row is saved.
export interface ImportRequest {
  plugin: string
  payload: string
}

export interface AccountFormProps {
  account?: Account
  submitting: boolean
  error?: string | null
  onCancel: () => void
  onSubmit: (input: AccountInput, importReq?: ImportRequest) => void
}

const FIELD =
  'field'

// DAY_KEYS index 0..6 = Sunday..Saturday, matching the server's
// ActiveWindow.Days encoding. Resolved to labels via t() at render time.
const DAY_KEYS = [
  'accountForm.daySun',
  'accountForm.dayMon',
  'accountForm.dayTue',
  'accountForm.dayWed',
  'accountForm.dayThu',
  'accountForm.dayFri',
  'accountForm.daySat',
]

export function AccountForm({
  account,
  submitting,
  error,
  onCancel,
  onSubmit,
}: AccountFormProps) {
  const { t } = useI18n()
  const isEdit = account != null
  const [name, setName] = useState(account?.name ?? '')
  const [provider, setProvider] = useState(account?.provider ?? '')
  const [enabled, setEnabled] = useState(account?.enabled ?? true)
  const [weight, setWeight] = useState(String(account?.weight ?? 100))
  const [maxConcurrency, setMaxConcurrency] = useState(String(account?.max_concurrency ?? 0))
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
  // Per-account model allow-list, edited as a comma/newline-separated list.
  const [allowedModels, setAllowedModels] = useState((account?.allowed_models ?? []).join(', '))
  const [proxyURL, setProxyURL] = useState(account?.proxy_url ?? '')
  const { data: proxies } = useProxies()
  const [proxyID, setProxyID] = useState(account?.proxy_id != null ? String(account.proxy_id) : '')
  const [forwardClientIP, setForwardClientIP] = useState(account?.forward_client_ip ?? false)
  const po = account?.param_overrides
  const [ovMaxTokens, setOvMaxTokens] = useState(numToStr(po?.max_tokens))
  const [ovTemperature, setOvTemperature] = useState(numToStr(po?.temperature))
  const [ovTopP, setOvTopP] = useState(numToStr(po?.top_p))
  const [ovThinking, setOvThinking] = useState(numToStr(po?.thinking_budget_tokens))
  const [windows, setWindows] = useState<ActiveWindow[]>(account?.active_windows ?? [])
  const [timezone, setTimezone] = useState(account?.active_timezone ?? '')
  // Upstream auth method. Only api_key and imported_oauth are offered
  // here; browser OAuth / service accounts arrive with later plugins.
  const [authType, setAuthType] = useState(account?.auth_type ?? 'api_key')
  const { data: authPlugins } = useAuthPlugins()
  const [authPlugin, setAuthPlugin] = useState(account?.auth_plugin ?? '')
  const [authJSON, setAuthJSON] = useState('')

  function updateWindow(i: number, patch: Partial<ActiveWindow>) {
    setWindows((rows) => rows.map((r, j) => (j === i ? { ...r, ...patch } : r)))
  }
  function toggleDay(i: number, day: number) {
    setWindows((rows) =>
      rows.map((r, j) => {
        if (j !== i) return r
        const days = r.days ?? []
        return { ...r, days: days.includes(day) ? days.filter((d) => d !== day) : [...days, day].sort() }
      }),
    )
  }
  function addWindow() {
    setWindows((rows) => [...rows, { days: [], start: '09:00', end: '18:00' }])
  }
  function removeWindow(i: number) {
    setWindows((rows) => rows.filter((_, j) => j !== i))
  }
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
      setLocalErr(t('accountForm.errChooseProvider'))
      return
    }
    if (Object.keys(credentials).length > 0) {
      test.mutate({ provider: provider.trim(), base_url: baseURL.trim(), credentials })
    } else if (isEdit && account) {
      testById.mutate(account.id)
    } else {
      setLocalErr(t('accountForm.errEnterApiKey'))
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
      setLocalErr(t('accountForm.errNameProviderRequired'))
      return
    }

    const isImportedOAuth = authType === 'imported_oauth'

    // Collapse the credential rows into a map, dropping blank keys. A
    // row with a key but no value is a mistake worth flagging. OAuth
    // accounts don't use the api_key rows (that fieldset is hidden), so
    // skip them entirely — the default empty api_key row must not block
    // an OAuth submit.
    const credentials: Record<string, string> = {}
    if (!isImportedOAuth) {
      for (const row of creds) {
        const k = row.key.trim()
        if (!k) continue
        if (!row.value) {
          setLocalErr(t('accountForm.errCredentialNoValue', { key: k }))
          return
        }
        credentials[k] = row.value
      }
    }
    const hasCreds = Object.keys(credentials).length > 0

    if (!isEdit && !hasCreds && !isImportedOAuth) {
      setLocalErr(t('accountForm.errCredentialRequired'))
      return
    }
    // OAuth accounts get their tokens either from a pasted credential
    // file (import) OR from a later browser "Re-login". An empty auth.json
    // on create is fine — the account is created credential-less and the
    // operator finishes it with Re-login; until then it just won't route.
    if (isImportedOAuth && authPlugin.trim() === '') {
      setLocalErr(t('accountForm.errAuthPluginRequired'))
      return
    }

    // Trim redirect rows and drop blank ones; flag a half-filled row.
    const cleanRedirects: ModelRedirect[] = []
    for (const r of redirects) {
      const source = r.source.trim()
      const target = r.target.trim()
      if (!source && !target) continue
      if (!source || !target) {
        setLocalErr(t('accountForm.errRedirectSourceTarget'))
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
      allowed_models: allowedModels
        .split(/[\n,]/)
        .map((m) => m.trim())
        .filter((m) => m !== ''),
      health_probe_enabled: healthProbe === '' ? null : healthProbe === 'true',
      proxy_url: proxyURL.trim(),
      proxy_id: proxyID === '' ? null : Number(proxyID),
      forward_client_ip: forwardClientIP,
      param_overrides: {
        ...(strToNum(ovMaxTokens) != null ? { max_tokens: strToNum(ovMaxTokens)! } : {}),
        ...(strToNum(ovTemperature) != null ? { temperature: strToNum(ovTemperature)! } : {}),
        ...(strToNum(ovTopP) != null ? { top_p: strToNum(ovTopP)! } : {}),
        ...(strToNum(ovThinking) != null ? { thinking_budget_tokens: strToNum(ovThinking)! } : {}),
      },
      active_windows: windows.map((w) => ({
        days: w.days ?? [],
        start: w.start.trim(),
        end: w.end.trim(),
      })),
      active_timezone: timezone.trim(),
      auth_type: authType,
      auth_plugin: isImportedOAuth ? authPlugin.trim() : '',
      max_concurrency: Math.max(0, parseIntOr(maxConcurrency, 0)),
      // client_profile has no form controls yet: echo the stored values
      // so the PUT-style update keeps them.
      client_profile: account?.client_profile ?? '',
      client_profile_config: account?.client_profile_config ?? {},
    }
    const importReq =
      isImportedOAuth && authJSON.trim() !== ''
        ? { plugin: authPlugin.trim(), payload: authJSON.trim() }
        : undefined
    onSubmit(input, importReq)
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      {!isEdit && (
        <div className="rounded-lg border border-line bg-surface-2 px-3 py-3 text-sm text-muted">
          <p className="font-medium text-ink">{t('accountForm.quickStartTitle')}</p>
          <p className="mt-1">{t('accountForm.quickStartBody')}</p>
        </div>
      )}

      <div className="grid grid-cols-2 gap-4">
        <Field label={t('common.name')} help={t('accountForm.nameHelp')}>
          <input
            className={FIELD}
            value={name}
            onChange={(e) => setName(e.target.value)}
            autoFocus
            placeholder={t('accountForm.namePlaceholder')}
          />
        </Field>
        <Field label={t('accountForm.provider')} help={t('accountForm.providerHelp')}>
          <input
            className={FIELD}
            value={provider}
            onChange={(e) => setProvider(e.target.value)}
            placeholder="anthropic, openai, claude-platform, openai-codex, claude-subscription"
          />
        </Field>
      </div>

      <Field label={t('accountForm.authMethod')} help={t('accountForm.authMethodHelp')}>
        <select
          className={FIELD}
          value={authType}
          onChange={(e) => {
            const next = e.target.value
            setAuthType(next)
            if (next === 'imported_oauth' && authPlugin === '') {
              setAuthPlugin(authPlugins?.[0]?.name ?? 'codex-oauth')
            }
          }}
        >
          <option value="api_key">{t('accountForm.authApiKey')}</option>
          <option value="imported_oauth">{t('accountForm.authImportedOAuth')}</option>
        </select>
      </Field>

      {authType === 'imported_oauth' && (
        <fieldset className="space-y-2 rounded-lg border border-line p-3">
          <legend className="text-sm font-medium">{t('accountForm.authImportLegend')}</legend>
          <Field label={t('accountForm.authPlugin')}>
            <select className={FIELD} value={authPlugin} onChange={(e) => setAuthPlugin(e.target.value)}>
              {(authPlugins ?? []).map((p) => (
                <option key={p.name} value={p.name}>
                  {p.display_name || p.name}
                  {p.experimental ? ` (${t('accountForm.experimental')})` : ''}
                </option>
              ))}
              {authPlugins == null && <option value="codex-oauth">OpenAI Codex OAuth</option>}
            </select>
          </Field>
          <Field label={t('accountForm.authJsonLabel')} help={t('accountForm.authJsonHelp')}>
            <textarea
              className={FIELD + ' h-28 font-mono text-xs'}
              value={authJSON}
              onChange={(e) => setAuthJSON(e.target.value)}
              placeholder='{"tokens":{"id_token":"...","access_token":"...","refresh_token":"..."},...}'
              spellCheck={false}
            />
          </Field>
          {isEdit && (
            <p className="text-xs text-muted">{t('accountForm.authJsonKeepHint')}</p>
          )}
        </fieldset>
      )}

      <Field label={t('accountForm.baseUrlOptional')} help={t('accountForm.baseUrlHelp')}>
        <input
          className={FIELD}
          value={baseURL}
          onChange={(e) => setBaseURL(e.target.value)}
          placeholder={t('accountForm.baseUrlPlaceholder')}
        />
      </Field>

      <details className="rounded-lg border border-line p-3" open={endpoints.length > 0 || proxyURL !== '' || proxyID !== ''}>
        <summary className="cursor-pointer text-sm text-muted">{t('accountForm.networkSummary')}</summary>
        <div className="mt-3">
          <Field label={t('accountForm.proxy')} help={t('accountForm.proxyHelp')}>
            <select className={FIELD} value={proxyID} onChange={(e) => setProxyID(e.target.value)}>
              <option value="">{t('accountForm.proxyNone')}</option>
              {(proxies ?? []).map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} ({p.protocol}://{p.host}:{p.port})
                </option>
              ))}
            </select>
          </Field>
          <Field label={t('accountForm.proxyUrl')}>
            <input
              className={FIELD}
              value={proxyURL}
              onChange={(e) => setProxyURL(e.target.value)}
              placeholder={t('accountForm.proxyUrlPlaceholder')}
              disabled={proxyID !== ''}
            />
          </Field>
          <label className="mt-3 flex items-start gap-2 text-sm">
            <input
              type="checkbox"
              className="mt-0.5"
              checked={forwardClientIP}
              onChange={(e) => setForwardClientIP(e.target.checked)}
            />
            <span>
              {t('accountForm.forwardClientIp')}
              <span className="block text-xs text-muted">{t('accountForm.forwardClientIpHelp')}</span>
            </span>
          </label>
        </div>
        <p className="mt-3 text-xs text-muted">
          {t('accountForm.failoverHelp')}
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
            {t('accountForm.addEndpoint')}
          </button>
        </div>
      </details>

      <div className="grid grid-cols-3 gap-4">
        <Field label={t('accountForm.weight')} help={t('accountForm.weightHelp')}>
          <input className={FIELD} type="number" value={weight} onChange={(e) => setWeight(e.target.value)} />
        </Field>
        <Field label={t('accountForm.priority')} help={t('accountForm.priorityHelp')}>
          <input className={FIELD} type="number" value={priority} onChange={(e) => setPriority(e.target.value)} />
        </Field>
        <Field label={t('accountForm.costMultiplier')} help={t('accountForm.costMultiplierHelp')}>
          <input
            className={FIELD}
            type="number"
            step="0.1"
            value={costMultiplier}
            onChange={(e) => setCostMultiplier(e.target.value)}
          />
        </Field>
      </div>

      <Field label={t('accountForm.maxConcurrency')} help={t('accountForm.maxConcurrencyHelp')}>
        <input
          className={FIELD}
          type="number"
          min="0"
          value={maxConcurrency}
          onChange={(e) => setMaxConcurrency(e.target.value)}
        />
      </Field>

      <div className="grid grid-cols-2 gap-4">
        <Field label={t('accountForm.groupOptional')} help={t('accountForm.groupHelp')}>
          <select className={FIELD} value={groupID} onChange={(e) => setGroupID(e.target.value)}>
            <option value="">{t('accountForm.ungrouped')}</option>
            {(groups ?? []).map((g) => (
              <option key={g.id} value={g.id}>
                {g.name} (×{g.cost_multiplier})
              </option>
            ))}
          </select>
        </Field>
        <Field label={t('accountForm.activeHealthProbe')} help={t('accountForm.activeHealthProbeHelp')}>
          <select className={FIELD} value={healthProbe} onChange={(e) => setHealthProbe(e.target.value)}>
            <option value="">{t('accountForm.inheritDefault')}</option>
            <option value="true">{t('common.enabled')}</option>
            <option value="false">{t('common.disabled')}</option>
          </select>
        </Field>
      </div>

      <label className="flex items-center gap-2 text-sm">
        <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
        {t('accountForm.enabledRoutable')}
      </label>

      {authType === 'api_key' && (
      <fieldset className="space-y-2">
        <legend className="text-sm font-medium">{t('accountForm.credentials')}</legend>
        <p className="text-xs text-muted">{t('accountForm.credentialsHelp')}</p>
        {isEdit && account!.credential_keys.length > 0 && (
          <p className="text-xs text-muted">
            {t('accountForm.credentialsCurrentlySet', { keys: account!.credential_keys.join(', ') })}
          </p>
        )}
        {creds.map((row, i) => (
          <div key={i} className="flex gap-2">
            <input
              className={FIELD + ' flex-1'}
              value={row.key}
              onChange={(e) => updateCred(i, { key: e.target.value })}
              placeholder={t('accountForm.credKeyPlaceholder')}
            />
            <input
              className={FIELD + ' flex-1'}
              type="password"
              value={row.value}
              onChange={(e) => updateCred(i, { value: e.target.value })}
              placeholder={t('accountForm.valuePlaceholder')}
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
          {t('accountForm.addCredential')}
        </button>
      </fieldset>
      )}

      <details className="rounded-lg border border-line p-3" open={redirects.length > 0}>
        <summary className="cursor-pointer text-sm text-muted">
          {t('accountForm.modelRedirectsSummary')}
        </summary>
        <p className="mt-2 text-xs text-muted">
          {t('accountForm.modelRedirectsHelp')}
        </p>
        <div className="mt-3 space-y-2">
          {redirects.map((row, i) => (
            <div key={i} className="flex gap-2">
              <select
                className={FIELD + ' w-28'}
                value={row.match_type}
                onChange={(e) => updateRedirect(i, { match_type: e.target.value as ModelRedirectMatch })}
              >
                <option value="exact">{t('accountForm.matchExact')}</option>
                <option value="prefix">{t('accountForm.matchPrefix')}</option>
                <option value="suffix">{t('accountForm.matchSuffix')}</option>
                <option value="contains">{t('accountForm.matchContains')}</option>
                <option value="regex">{t('accountForm.matchRegex')}</option>
              </select>
              <input
                className={FIELD + ' flex-1'}
                value={row.source}
                onChange={(e) => updateRedirect(i, { source: e.target.value })}
                placeholder={t('accountForm.redirectSourcePlaceholder')}
              />
              <span className="self-center text-muted">→</span>
              <input
                className={FIELD + ' flex-1'}
                value={row.target}
                onChange={(e) => updateRedirect(i, { target: e.target.value })}
                placeholder={t('accountForm.redirectTargetPlaceholder')}
              />
              <button type="button" onClick={() => removeRedirect(i)} className="btn btn-secondary px-2">
                ✕
              </button>
            </div>
          ))}
          <button type="button" onClick={addRedirect} className="text-sm text-muted hover:text-ink">
            {t('accountForm.addRedirect')}
          </button>
        </div>
        <div className="mt-4 border-t border-line pt-3">
          <Field label={t('accountForm.allowedModelsLabel')} help={t('accountForm.allowedModelsHelp')}>
            <input
              className={FIELD}
              value={allowedModels}
              onChange={(e) => setAllowedModels(e.target.value)}
              placeholder={t('accountForm.allowedModelsPlaceholder')}
            />
          </Field>
        </div>
      </details>

      <details className="rounded-lg border border-line p-3" open={headers.length > 0}>
        <summary className="cursor-pointer text-sm text-muted">
          {t('accountForm.customHeadersSummary')}
        </summary>
        <p className="mt-2 text-xs text-muted">
          {t('accountForm.customHeadersHelp')}
        </p>
        <div className="mt-3 space-y-2">
          {headers.map((row, i) => (
            <div key={i} className="flex gap-2">
              <input
                className={FIELD + ' flex-1'}
                value={row.key}
                onChange={(e) => updateHeader(i, { key: e.target.value })}
                placeholder={t('accountForm.headerNamePlaceholder')}
              />
              <input
                className={FIELD + ' flex-1'}
                value={row.value}
                onChange={(e) => updateHeader(i, { value: e.target.value })}
                placeholder={t('accountForm.valuePlaceholder')}
              />
              <button type="button" onClick={() => removeHeader(i)} className="btn btn-secondary px-2">
                ✕
              </button>
            </div>
          ))}
          <button type="button" onClick={addHeader} className="text-sm text-muted hover:text-ink">
            {t('accountForm.addHeader')}
          </button>
        </div>
      </details>

      <details className="rounded-lg border border-line p-3" open={dailyLimit !== '' || totalLimit !== ''}>
        <summary className="cursor-pointer text-sm text-muted">
          {t('accountForm.spendCapsSummary')}
        </summary>
        <p className="mt-2 text-xs text-muted">
          {t('accountForm.spendCapsHelp')}
        </p>
        <div className="mt-3 grid grid-cols-2 gap-4">
          <Field label={t('accountForm.dailyUsdLimit')}>
            <input
              className={FIELD}
              type="number"
              step="0.01"
              min="0"
              value={dailyLimit}
              onChange={(e) => setDailyLimit(e.target.value)}
              placeholder={t('common.noCap')}
            />
          </Field>
          <Field label={t('accountForm.totalUsdLimit')}>
            <input
              className={FIELD}
              type="number"
              step="0.01"
              min="0"
              value={totalLimit}
              onChange={(e) => setTotalLimit(e.target.value)}
              placeholder={t('common.noCap')}
            />
          </Field>
        </div>
      </details>

      <details
        className="rounded-lg border border-line p-3"
        open={ovMaxTokens !== '' || ovTemperature !== '' || ovTopP !== '' || ovThinking !== ''}
      >
        <summary className="cursor-pointer text-sm text-muted">{t('accountForm.paramOverridesSummary')}</summary>
        <p className="mt-2 text-xs text-muted">
          {t('accountForm.paramOverridesHelp')}
        </p>
        <div className="mt-3 grid grid-cols-2 gap-4">
          <Field label={t('accountForm.maxTokens')}>
            <input className={FIELD} type="number" min="1" value={ovMaxTokens}
              onChange={(e) => setOvMaxTokens(e.target.value)} placeholder={t('accountForm.passthrough')} />
          </Field>
          <Field label={t('accountForm.temperature')}>
            <input className={FIELD} type="number" step="0.1" min="0" max="2" value={ovTemperature}
              onChange={(e) => setOvTemperature(e.target.value)} placeholder={t('accountForm.passthrough')} />
          </Field>
          <Field label={t('accountForm.topP')}>
            <input className={FIELD} type="number" step="0.05" min="0" max="1" value={ovTopP}
              onChange={(e) => setOvTopP(e.target.value)} placeholder={t('accountForm.passthrough')} />
          </Field>
          <Field label={t('accountForm.thinkingBudget')}>
            <input className={FIELD} type="number" min="1" value={ovThinking}
              onChange={(e) => setOvThinking(e.target.value)} placeholder={t('accountForm.off')} />
          </Field>
        </div>
      </details>

      <details className="rounded-lg border border-line p-3" open={windows.length > 0}>
        <summary className="cursor-pointer text-sm text-muted">{t('accountForm.activeWindowsSummary')}</summary>
        <p className="mt-2 text-xs text-muted">
          {t('accountForm.activeWindowsHelp')}
        </p>
        <div className="mt-3">
          <Field label={t('accountForm.timezone')}>
            <input
              className={FIELD}
              value={timezone}
              onChange={(e) => setTimezone(e.target.value)}
              placeholder={t('accountForm.timezonePlaceholder')}
            />
          </Field>
        </div>
        <div className="mt-3 space-y-3">
          {windows.map((w, i) => (
            <div key={i} className="rounded-lg border border-line p-3">
              <div className="flex items-center gap-2">
                <input
                  className={FIELD + ' w-28'}
                  type="time"
                  value={w.start}
                  onChange={(e) => updateWindow(i, { start: e.target.value })}
                />
                <span className="text-muted">→</span>
                <input
                  className={FIELD + ' w-28'}
                  type="time"
                  value={w.end}
                  onChange={(e) => updateWindow(i, { end: e.target.value })}
                />
                <button
                  type="button"
                  onClick={() => removeWindow(i)}
                  className="btn btn-secondary px-2 ml-auto"
                >
                  ✕
                </button>
              </div>
              <div className="mt-2 flex flex-wrap gap-1">
                {DAY_KEYS.map((dayKey, day) => {
                  const on = (w.days ?? []).includes(day)
                  return (
                    <button
                      key={day}
                      type="button"
                      onClick={() => toggleDay(i, day)}
                      className={
                        'rounded px-2 py-0.5 text-xs ' +
                        (on ? 'bg-ink text-bg' : 'border border-line text-muted hover:text-ink')
                      }
                    >
                      {t(dayKey)}
                    </button>
                  )
                })}
                <span className="ml-1 self-center text-xs text-muted">
                  {(w.days ?? []).length === 0 ? t('accountForm.everyDay') : ''}
                </span>
              </div>
            </div>
          ))}
          <button type="button" onClick={addWindow} className="text-sm text-muted hover:text-ink">
            {t('accountForm.addWindow')}
          </button>
        </div>
      </details>

      <details className="rounded-lg border border-line p-3">
        <summary className="cursor-pointer text-sm text-muted">
          {t('accountForm.circuitBreakerSummary')}
        </summary>
        <p className="mt-2 text-xs text-muted">
          {t('accountForm.circuitBreakerHelp')}
        </p>
        <div className="mt-3 grid grid-cols-3 gap-4">
          <Field label={t('accountForm.failureThreshold')}>
            <input
              className={FIELD}
              type="number"
              value={failureThreshold}
              onChange={(e) => setFailureThreshold(e.target.value)}
              placeholder={t('accountForm.defaultPlaceholder')}
            />
          </Field>
          <Field label={t('accountForm.openDuration')}>
            <input
              className={FIELD}
              type="number"
              value={openDurationMs}
              onChange={(e) => setOpenDurationMs(e.target.value)}
              placeholder={t('accountForm.defaultPlaceholder')}
            />
          </Field>
          <Field label={t('accountForm.halfOpenSuccess')}>
            <input
              className={FIELD}
              type="number"
              value={halfOpenSuccess}
              onChange={(e) => setHalfOpenSuccess(e.target.value)}
              placeholder={t('accountForm.defaultPlaceholder')}
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
          error={testErr ? (testErr instanceof ApiError ? testErr.message : t('accountForm.testFailed')) : null}
        />
      )}

      <div className="flex items-center justify-between gap-2">
        <button
          type="button"
          onClick={handleTest}
          disabled={testing}
          className="btn btn-secondary"
        >
          {testing ? t('accountForm.testing') : t('accountForm.testConnection')}
        </button>
        <div className="flex gap-2">
        <button
          type="button"
          onClick={onCancel}
          className="btn btn-secondary"
        >
          {t('common.cancel')}
        </button>
        <button
          type="submit"
          disabled={submitting}
          className="btn btn-primary"
        >
          {submitting ? t('common.saving') : isEdit ? t('common.saveChanges') : t('accountForm.createAccount')}
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

function Field({
  label,
  help,
  children,
}: {
  label: string
  help?: string
  children: React.ReactNode
}) {
  return (
    <label className="block space-y-1">
      <span className="text-sm text-muted">{label}</span>
      {children}
      {help ? <span className="block text-xs leading-5 text-muted">{help}</span> : null}
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
