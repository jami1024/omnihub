import { useState } from 'react'
import type { BlockedIP, BlockedIPInput } from '../lib/blockedIps'
import { useI18n } from '../lib/i18n'

// BlockedIPForm drives both create and edit. Leaving every limit blank
// makes the row a hard block (403); setting any limit turns it into a
// soft cap (429 when exceeded). The IP is immutable once created (it's
// the primary key), so it's read-only on edit.

export interface BlockedIPFormProps {
  entry?: BlockedIP
  submitting: boolean
  error?: string | null
  onCancel: () => void
  onSubmit: (input: BlockedIPInput) => void
}

const FIELD =
  'field'

export function BlockedIPForm({ entry, submitting, error, onCancel, onSubmit }: BlockedIPFormProps) {
  const { t } = useI18n()
  const isEdit = entry != null
  const [ip, setIp] = useState(entry?.ip ?? '')
  const [reason, setReason] = useState(entry?.reason ?? '')
  const [rpm, setRpm] = useState(numToStr(entry?.rpm_limit))
  const [tpm, setTpm] = useState(numToStr(entry?.tpm_limit))
  const [concurrent, setConcurrent] = useState(numToStr(entry?.concurrent_limit))
  const [localErr, setLocalErr] = useState<string | null>(null)

  const rpmVal = strToNum(rpm)
  const tpmVal = strToNum(tpm)
  const concurrentVal = strToNum(concurrent)
  const isHardBlock = rpmVal == null && tpmVal == null && concurrentVal == null

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setLocalErr(null)

    if (!isEdit && !ip.trim()) {
      setLocalErr(t('blockedIpForm.ipRequired'))
      return
    }
    for (const [label, v] of [
      ['RPM', rpmVal],
      ['TPM', tpmVal],
      ['Concurrent', concurrentVal],
    ] as const) {
      if (v != null && v <= 0) {
        setLocalErr(t('blockedIpForm.limitPositive', { label }))
        return
      }
    }

    const input: BlockedIPInput = {
      reason: reason.trim(),
      rpm_limit: rpmVal,
      tpm_limit: tpmVal,
      concurrent_limit: concurrentVal,
    }
    if (!isEdit) input.ip = ip.trim()
    onSubmit(input)
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <Field label={t('blockedIpForm.ipAddress')}>
        <input
          className={FIELD + (isEdit ? ' opacity-60' : '')}
          value={ip}
          onChange={(e) => setIp(e.target.value)}
          readOnly={isEdit}
          autoFocus={!isEdit}
          placeholder="203.0.113.7 or 2001:db8::1"
        />
      </Field>

      <Field label={t('blockedIpForm.reasonOptional')}>
        <input
          className={FIELD}
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          placeholder={t('blockedIpForm.reasonPlaceholder')}
        />
      </Field>

      <div className="grid grid-cols-3 gap-4">
        <Field label={t('blockedIpForm.rpmCap')}>
          <input className={FIELD} type="number" value={rpm} onChange={(e) => setRpm(e.target.value)} placeholder={t('common.none')} />
        </Field>
        <Field label={t('blockedIpForm.tpmCap')}>
          <input className={FIELD} type="number" value={tpm} onChange={(e) => setTpm(e.target.value)} placeholder={t('common.none')} />
        </Field>
        <Field label={t('blockedIpForm.concurrentCap')}>
          <input
            className={FIELD}
            type="number"
            value={concurrent}
            onChange={(e) => setConcurrent(e.target.value)}
            placeholder={t('common.none')}
          />
        </Field>
      </div>

      <p
        className={`rounded-md px-3 py-2 text-xs ${
          isHardBlock
            ? 'bg-danger-bg text-danger'
            : 'bg-warning-bg text-warning'
        }`}
      >
        {isHardBlock
          ? t('blockedIpForm.hardBlockHint')
          : t('blockedIpForm.softCapHint')}
      </p>

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
          {submitting ? t('common.saving') : isEdit ? t('common.saveChanges') : t('blockedIpForm.blockIp')}
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
