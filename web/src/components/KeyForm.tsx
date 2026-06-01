import { useState } from 'react'
import type { Key, KeyInput } from '../lib/keys'

// KeyForm drives both create and edit. There is no secret-value field:
// the server always generates the key and returns the cleartext once on
// create. Editing changes metadata only; the value is immutable.

export interface KeyFormProps {
  apiKey?: Key
  submitting: boolean
  error?: string | null
  onCancel: () => void
  onSubmit: (input: KeyInput) => void
}

const FIELD =
  'field'

export function KeyForm({ apiKey, submitting, error, onCancel, onSubmit }: KeyFormProps) {
  const isEdit = apiKey != null
  const [name, setName] = useState(apiKey?.name ?? '')
  const [label, setLabel] = useState(apiKey?.label ?? '')
  const [enabled, setEnabled] = useState(apiKey?.enabled ?? true)
  const [dailyUSD, setDailyUSD] = useState(numToStr(apiKey?.daily_usd_limit))
  const [rpm, setRpm] = useState(numToStr(apiKey?.rpm_limit))
  const [models, setModels] = useState((apiKey?.allowed_models ?? []).join(', '))
  const [localErr, setLocalErr] = useState<string | null>(null)

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setLocalErr(null)

    if (!name.trim()) {
      setLocalErr('Name is required.')
      return
    }
    const rpmVal = strToNum(rpm)
    if (rpmVal != null && rpmVal <= 0) {
      setLocalErr('RPM limit must be greater than 0 (leave blank for no limit).')
      return
    }
    const dailyVal = strToNum(dailyUSD)
    if (dailyVal != null && dailyVal < 0) {
      setLocalErr('Daily USD limit cannot be negative (leave blank for no limit).')
      return
    }

    const input: KeyInput = {
      name: name.trim(),
      label: label.trim(),
      enabled,
      daily_usd_limit: dailyVal,
      rpm_limit: rpmVal,
      allowed_models: models
        .split(',')
        .map((m) => m.trim())
        .filter(Boolean),
    }
    onSubmit(input)
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <div className="grid grid-cols-2 gap-4">
        <Field label="Name">
          <input className={FIELD} value={name} onChange={(e) => setName(e.target.value)} autoFocus />
        </Field>
        <Field label="Label (shown in logs)">
          <input
            className={FIELD}
            value={label}
            onChange={(e) => setLabel(e.target.value)}
            placeholder="defaults to the name"
          />
        </Field>
      </div>

      <div className="grid grid-cols-2 gap-4">
        <Field label="Daily USD limit">
          <input
            className={FIELD}
            type="number"
            step="0.01"
            value={dailyUSD}
            onChange={(e) => setDailyUSD(e.target.value)}
            placeholder="no limit"
          />
        </Field>
        <Field label="RPM limit">
          <input
            className={FIELD}
            type="number"
            value={rpm}
            onChange={(e) => setRpm(e.target.value)}
            placeholder="no limit"
          />
        </Field>
      </div>

      <Field label="Allowed models (comma-separated)">
        <input
          className={FIELD}
          value={models}
          onChange={(e) => setModels(e.target.value)}
          placeholder="blank = all models"
        />
      </Field>

      <label className="flex items-center gap-2 text-sm">
        <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
        Enabled
      </label>

      {!isEdit && (
        <p className="rounded-md bg-warning-bg px-3 py-2 text-xs text-warning">
          The key is generated on the server and shown to you once after creation.
          Store it somewhere safe — it cannot be retrieved again.
        </p>
      )}

      {(localErr || error) && (
        <p className="text-sm text-danger">{localErr ?? error}</p>
      )}

      <div className="flex justify-end gap-2">
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
          {submitting ? 'Saving…' : isEdit ? 'Save changes' : 'Create key'}
        </button>
      </div>
    </form>
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
