import { useState } from 'react'
import { toPerMillion, toPerToken, type ModelPrice, type PriceInput } from '../lib/prices'

// PriceForm drives create and edit. All money fields are entered as USD
// per MILLION tokens (what vendor pages show) and converted to per-token
// on submit. The model name is the immutable key, so it's read-only on
// edit. Saving always writes a 'manual' row server-side.

export interface PriceFormProps {
  price?: ModelPrice
  submitting: boolean
  error?: string | null
  onCancel: () => void
  onSubmit: (input: PriceInput) => void
}

const FIELD =
  'w-full rounded-md border border-zinc-300 bg-white px-3 py-1.5 text-sm dark:border-zinc-700 dark:bg-zinc-950'

export function PriceForm({ price, submitting, error, onCancel, onSubmit }: PriceFormProps) {
  const isEdit = price != null
  const [model, setModel] = useState(price?.model ?? '')
  const [input, setInput] = useState(mtokStr(price?.input_cost_per_token))
  const [output, setOutput] = useState(mtokStr(price?.output_cost_per_token))
  const [cacheWrite5m, setCacheWrite5m] = useState(mtokStr(price?.cache_creation_input_token_cost))
  const [cacheWrite1h, setCacheWrite1h] = useState(
    mtokStr(price?.cache_creation_input_token_cost_above_1hr),
  )
  const [cacheRead, setCacheRead] = useState(mtokStr(price?.cache_read_input_token_cost))
  const [localErr, setLocalErr] = useState<string | null>(null)

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setLocalErr(null)
    if (!isEdit && !model.trim()) {
      setLocalErr('Model name is required.')
      return
    }
    const fields = [input, output, cacheWrite5m, cacheWrite1h, cacheRead]
    if (fields.some((v) => Number(v) < 0)) {
      setLocalErr('Costs cannot be negative.')
      return
    }
    const inputBody: PriceInput = {
      input_cost_per_token: toPerToken(numOr0(input)),
      output_cost_per_token: toPerToken(numOr0(output)),
      cache_creation_input_token_cost: toPerToken(numOr0(cacheWrite5m)),
      cache_creation_input_token_cost_above_1hr: toPerToken(numOr0(cacheWrite1h)),
      cache_read_input_token_cost: toPerToken(numOr0(cacheRead)),
    }
    if (!isEdit) inputBody.model = model.trim()
    onSubmit(inputBody)
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <Field label="Model name">
        <input
          className={FIELD + (isEdit ? ' opacity-60' : '')}
          value={model}
          onChange={(e) => setModel(e.target.value)}
          readOnly={isEdit}
          autoFocus={!isEdit}
          placeholder="gpt-5.2 (prefix-matched: gpt-5.2 also prices gpt-5.2-2025-12-11)"
        />
      </Field>

      <p className="text-xs text-zinc-500">All costs are USD per 1,000,000 tokens.</p>

      <div className="grid grid-cols-2 gap-4">
        <Field label="Input">
          <Money value={input} onChange={setInput} />
        </Field>
        <Field label="Output">
          <Money value={output} onChange={setOutput} />
        </Field>
      </div>

      <details className="rounded-md border border-zinc-200 p-3 dark:border-zinc-800">
        <summary className="cursor-pointer text-sm text-zinc-500">Cache rates (optional)</summary>
        <div className="mt-3 grid grid-cols-3 gap-4">
          <Field label="Cache write 5m">
            <Money value={cacheWrite5m} onChange={setCacheWrite5m} />
          </Field>
          <Field label="Cache write 1h">
            <Money value={cacheWrite1h} onChange={setCacheWrite1h} />
          </Field>
          <Field label="Cache read">
            <Money value={cacheRead} onChange={setCacheRead} />
          </Field>
        </div>
        <p className="mt-2 text-xs text-zinc-500">
          Leave a cache rate blank/0 and the engine falls back to Anthropic ratios off the input
          price (5m 1.25×, 1h 2×, read 0.10×).
        </p>
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
          {submitting ? 'Saving…' : isEdit ? 'Save changes' : 'Add price'}
        </button>
      </div>
    </form>
  )
}

function Money({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <div className="relative">
      <span className="pointer-events-none absolute left-2 top-1.5 text-sm text-zinc-400">$</span>
      <input
        className={FIELD + ' pl-5'}
        type="number"
        step="0.01"
        min="0"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder="0.00"
      />
    </div>
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

// mtokStr renders a per-token rate as a per-million string, dropping a
// trailing 0 so an unset rate shows blank.
function mtokStr(perToken: number | undefined): string {
  if (!perToken) return ''
  return String(toPerMillion(perToken))
}
function numOr0(s: string): number {
  const n = Number(s)
  return Number.isFinite(n) ? n : 0
}
