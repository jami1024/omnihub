import { useState } from 'react'
import type { Account, AccountInput } from '../lib/accounts'

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
  'w-full rounded-md border border-zinc-300 bg-white px-3 py-1.5 text-sm dark:border-zinc-700 dark:bg-zinc-950'

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
  const [localErr, setLocalErr] = useState<string | null>(null)

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

      <label className="flex items-center gap-2 text-sm">
        <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
        Enabled (routable)
      </label>

      <fieldset className="space-y-2">
        <legend className="text-sm font-medium">Credentials</legend>
        {isEdit && account!.credential_keys.length > 0 && (
          <p className="text-xs text-zinc-500">
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
              className="rounded-md border border-zinc-300 px-2 text-sm text-zinc-500 hover:bg-zinc-100 dark:border-zinc-700 dark:hover:bg-zinc-800"
            >
              ✕
            </button>
          </div>
        ))}
        <button
          type="button"
          onClick={addCred}
          className="text-sm text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200"
        >
          + add credential
        </button>
      </fieldset>

      <details className="rounded-md border border-zinc-200 p-3 dark:border-zinc-800">
        <summary className="cursor-pointer text-sm text-zinc-500">
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
        <p className="text-sm text-red-600 dark:text-red-400">{localErr ?? error}</p>
      )}

      <div className="flex justify-end gap-2">
        <button
          type="button"
          onClick={onCancel}
          className="rounded-md border border-zinc-300 px-3 py-1.5 text-sm hover:bg-zinc-100 dark:border-zinc-700 dark:hover:bg-zinc-800"
        >
          Cancel
        </button>
        <button
          type="submit"
          disabled={submitting}
          className="rounded-md bg-zinc-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-zinc-700 disabled:opacity-50 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-300"
        >
          {submitting ? 'Saving…' : isEdit ? 'Save changes' : 'Create account'}
        </button>
      </div>
    </form>
  )
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block space-y-1">
      <span className="text-sm text-zinc-600 dark:text-zinc-400">{label}</span>
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
