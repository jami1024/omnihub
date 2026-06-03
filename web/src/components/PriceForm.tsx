import { useState } from 'react'
import { toPerMillion, toPerToken, type ModelPrice, type PriceInput } from '../lib/prices'
import { useI18n } from '../lib/i18n'

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
  'field'

export function PriceForm({ price, submitting, error, onCancel, onSubmit }: PriceFormProps) {
  const { t } = useI18n()
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
      setLocalErr(t('priceForm.modelNameRequired'))
      return
    }
    const fields = [input, output, cacheWrite5m, cacheWrite1h, cacheRead]
    if (fields.some((v) => Number(v) < 0)) {
      setLocalErr(t('priceForm.costsNegative'))
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
      <Field label={t('priceForm.modelName')}>
        <input
          className={FIELD + (isEdit ? ' opacity-60' : '')}
          value={model}
          onChange={(e) => setModel(e.target.value)}
          readOnly={isEdit}
          autoFocus={!isEdit}
          placeholder={t('priceForm.modelNamePlaceholder')}
        />
      </Field>

      <p className="text-xs text-muted">{t('priceForm.costsUnit')}</p>

      <div className="grid grid-cols-2 gap-4">
        <Field label={t('priceForm.input')}>
          <Money value={input} onChange={setInput} />
        </Field>
        <Field label={t('priceForm.output')}>
          <Money value={output} onChange={setOutput} />
        </Field>
      </div>

      <details className="rounded-lg border border-line p-3">
        <summary className="cursor-pointer text-sm text-muted">{t('priceForm.cacheRates')}</summary>
        <div className="mt-3 grid grid-cols-3 gap-4">
          <Field label={t('priceForm.cacheWrite5m')}>
            <Money value={cacheWrite5m} onChange={setCacheWrite5m} />
          </Field>
          <Field label={t('priceForm.cacheWrite1h')}>
            <Money value={cacheWrite1h} onChange={setCacheWrite1h} />
          </Field>
          <Field label={t('priceForm.cacheRead')}>
            <Money value={cacheRead} onChange={setCacheRead} />
          </Field>
        </div>
        <p className="mt-2 text-xs text-muted">
          {t('priceForm.cacheFallback')}
        </p>
      </details>

      {(localErr || error) && (
        <p className="text-sm text-danger">{localErr ?? error}</p>
      )}

      <div className="flex justify-end gap-2">
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
          {submitting ? t('common.saving') : isEdit ? t('common.saveChanges') : t('priceForm.addPrice')}
        </button>
      </div>
    </form>
  )
}

function Money({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <div className="relative">
      <span className="pointer-events-none absolute left-2 top-1.5 text-sm text-muted">$</span>
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
      <span className="text-sm text-muted">{label}</span>
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
