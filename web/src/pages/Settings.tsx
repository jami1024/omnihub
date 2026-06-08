import { useEffect, useState } from 'react'
import { SettingsLayout } from '../components/SettingsLayout'
import { ErrorNotice, PageHeader } from '../components/PageChrome'
import { ApiError } from '../lib/api'
import { useI18n } from '../lib/i18n'
import {
  useGatewaySettings,
  useSettings,
  useUpdateGatewaySettings,
  useUpdateSettings,
  type GatewaySettings,
  type PortalSettings,
} from '../lib/settings'

// Settings is the admin control for the end-user portal policy: whether
// open registration is allowed, and the default / ceiling limits clamped
// onto keys that portal users create for themselves.
export function SettingsPage() {
  const { t } = useI18n()
  const { data, isLoading, error } = useSettings()
  const { data: gatewayData, isLoading: gatewayLoading, error: gatewayError } = useGatewaySettings()
  const update = useUpdateSettings()
  const updateGateway = useUpdateGatewaySettings()
  const [form, setForm] = useState<PortalSettings | null>(null)
  const [gatewayForm, setGatewayForm] = useState<GatewaySettings | null>(null)
  const [saved, setSaved] = useState(false)
  const [gatewaySaved, setGatewaySaved] = useState(false)

  useEffect(() => {
    if (data && !form) setForm(data)
  }, [data, form])

  useEffect(() => {
    if (gatewayData && !gatewayForm) setGatewayForm(gatewayData)
  }, [gatewayData, gatewayForm])

  function num(v: string): number | null {
    const t = v.trim()
    if (t === '') return null
    const n = Number(t)
    return Number.isFinite(n) ? n : null
  }

  function int(v: string, fallback = 0): number {
    const n = Number(v)
    return Number.isFinite(n) ? Math.trunc(n) : fallback
  }

  function seconds(ms: number): number {
    return ms / 1000
  }

  function secondsToMs(v: string, fallbackMs: number): number {
    const n = Number(v)
    return Number.isFinite(n) ? Math.round(n * 1000) : fallbackMs
  }

  function save(e: React.FormEvent) {
    e.preventDefault()
    if (!form) return
    setSaved(false)
    update.mutate(form, { onSuccess: () => setSaved(true) })
  }

  function saveGateway(e: React.FormEvent) {
    e.preventDefault()
    if (!gatewayForm) return
    setGatewaySaved(false)
    updateGateway.mutate(gatewayForm, { onSuccess: () => setGatewaySaved(true) })
  }

  return (
    <SettingsLayout>
      <div className="max-w-3xl">
        <PageHeader
          eyebrow={t('settings.eyebrow')}
          context={t('settings.context')}
          title={t('settings.title')}
          description={t('settings.description')}
        />

        {(isLoading || gatewayLoading) && <p className="text-sm text-muted">{t('common.loading')}</p>}
        {error && <ErrorNotice>{error instanceof ApiError ? error.message : t('settings.loadError')}</ErrorNotice>}
        {gatewayError && (
          <ErrorNotice>
            {gatewayError instanceof ApiError ? gatewayError.message : t('settings.gatewayLoadError')}
          </ErrorNotice>
        )}

        {form && (
          <form onSubmit={save} className="space-y-6">
            <section className="card p-5">
              <label className="flex items-start gap-3">
                <input
                  type="checkbox"
                  className="mt-1 h-5 w-5 shrink-0"
                  checked={form.signup_enabled}
                  onChange={(e) => setForm({ ...form, signup_enabled: e.target.checked })}
                />
                <span>
                  <span className="text-sm font-medium">{t('settings.allowOpenRegistration')}</span>
                  <span className="block text-xs text-muted">
                    {t('settings.allowOpenRegistrationHint')}
                  </span>
                </span>
              </label>
            </section>

            <section className="card space-y-4 p-5">
              <div>
                <h3 className="text-sm font-medium">{t('settings.userKeyLimits')}</h3>
                <p className="text-xs text-muted">
                  {t('settings.userKeyLimitsHint')}
                </p>
              </div>
              <div className="grid gap-4 sm:grid-cols-3">
                <Field label={t('settings.defaultDailyUsd')}>
                  <input
                    className="field"
                    type="number"
                    step="0.01"
                    value={form.key_daily_usd_default ?? ''}
                    onChange={(e) => setForm({ ...form, key_daily_usd_default: num(e.target.value) })}
                    placeholder={t('common.none')}
                  />
                </Field>
                <Field label={t('settings.maxDailyUsd')}>
                  <input
                    className="field"
                    type="number"
                    step="0.01"
                    value={form.key_daily_usd_max ?? ''}
                    onChange={(e) => setForm({ ...form, key_daily_usd_max: num(e.target.value) })}
                    placeholder={t('common.noCap')}
                  />
                </Field>
                <Field label={t('settings.maxRpm')}>
                  <input
                    className="field"
                    type="number"
                    value={form.key_rpm_max ?? ''}
                    onChange={(e) => setForm({ ...form, key_rpm_max: num(e.target.value) })}
                    placeholder={t('common.noCap')}
                  />
                </Field>
              </div>
            </section>

            <section className="card space-y-4 p-5">
              <div>
                <h3 className="text-sm font-medium">{t('settings.billing')}</h3>
                <p className="text-xs text-muted">{t('settings.signupBonusHint')}</p>
              </div>
              <Field label={t('settings.signupBonus')}>
                <input
                  className="field max-w-xs"
                  type="number"
                  step="0.01"
                  min="0"
                  value={form.signup_bonus_usd}
                  onChange={(e) => setForm({ ...form, signup_bonus_usd: Number(e.target.value) || 0 })}
                  placeholder="0"
                />
              </Field>
            </section>

            {update.error && (
              <p className="text-sm text-danger">
                {update.error instanceof ApiError ? update.error.message : t('settings.saveError')}
              </p>
            )}
            <div className="flex items-center gap-3">
              <button type="submit" disabled={update.isPending} className="btn btn-primary">
                {update.isPending ? t('common.saving') : t('settings.saveSettings')}
              </button>
              {saved && !update.isPending && <span className="text-sm text-muted">{t('common.saved')}</span>}
            </div>
          </form>
        )}

        {gatewayForm && (
          <form onSubmit={saveGateway} className="mt-8 space-y-6 border-t border-line pt-8">
            <section className="card space-y-4 p-5">
              <div>
                <h3 className="text-sm font-medium">{t('settings.gatewayHealth')}</h3>
                <p className="text-xs text-muted">{t('settings.gatewayHealthHint')}</p>
              </div>

              <label className="flex items-start gap-3">
                <input
                  type="checkbox"
                  className="mt-1 h-5 w-5 shrink-0"
                  checked={gatewayForm.health_probe_enabled}
                  onChange={(e) =>
                    setGatewayForm({ ...gatewayForm, health_probe_enabled: e.target.checked })
                  }
                />
                <span>
                  <span className="text-sm font-medium">{t('settings.healthProbeEnabled')}</span>
                  <span className="block text-xs text-muted">{t('settings.healthProbeEnabledHint')}</span>
                </span>
              </label>

              <div className="grid gap-4 sm:grid-cols-2">
                <Field label={t('settings.healthProbeInterval')} help={t('settings.healthProbeIntervalHint')}>
                  <input
                    className="field"
                    type="number"
                    min="10"
                    step="1"
                    value={seconds(gatewayForm.health_probe_interval_ms)}
                    onChange={(e) =>
                      setGatewayForm({
                        ...gatewayForm,
                        health_probe_interval_ms: secondsToMs(e.target.value, gatewayForm.health_probe_interval_ms),
                      })
                    }
                  />
                </Field>
                <Field label={t('settings.healthProbeConcurrency')} help={t('settings.healthProbeConcurrencyHint')}>
                  <input
                    className="field"
                    type="number"
                    min="1"
                    max="16"
                    value={gatewayForm.health_probe_concurrency}
                    onChange={(e) =>
                      setGatewayForm({ ...gatewayForm, health_probe_concurrency: int(e.target.value, 1) })
                    }
                  />
                </Field>
                <Field label={t('settings.healthProbeRedThreshold')} help={t('settings.healthProbeRedThresholdHint')}>
                  <input
                    className="field"
                    type="number"
                    min="1"
                    value={gatewayForm.health_probe_red_threshold}
                    onChange={(e) =>
                      setGatewayForm({ ...gatewayForm, health_probe_red_threshold: int(e.target.value, 1) })
                    }
                  />
                </Field>
                <Field label={t('settings.healthProbeGreenThreshold')} help={t('settings.healthProbeGreenThresholdHint')}>
                  <input
                    className="field"
                    type="number"
                    min="1"
                    value={gatewayForm.health_probe_green_threshold}
                    onChange={(e) =>
                      setGatewayForm({ ...gatewayForm, health_probe_green_threshold: int(e.target.value, 1) })
                    }
                  />
                </Field>
                <Field label={t('settings.healthProbeTimeout')} help={t('settings.healthProbeTimeoutHint')}>
                  <input
                    className="field"
                    type="number"
                    min="1"
                    step="1"
                    value={seconds(gatewayForm.health_probe_timeout_ms)}
                    onChange={(e) =>
                      setGatewayForm({
                        ...gatewayForm,
                        health_probe_timeout_ms: secondsToMs(e.target.value, gatewayForm.health_probe_timeout_ms),
                      })
                    }
                  />
                </Field>
                <Field label={t('settings.healthProbeSlowThreshold')} help={t('settings.healthProbeSlowThresholdHint')}>
                  <input
                    className="field"
                    type="number"
                    min="1"
                    value={gatewayForm.health_probe_slow_threshold_ms}
                    onChange={(e) =>
                      setGatewayForm({
                        ...gatewayForm,
                        health_probe_slow_threshold_ms: int(e.target.value, 1),
                      })
                    }
                  />
                </Field>
              </div>
            </section>

            <section className="card space-y-4 p-5">
              <div>
                <h3 className="text-sm font-medium">{t('settings.circuitFailover')}</h3>
                <p className="text-xs text-muted">{t('settings.circuitFailoverHint')}</p>
              </div>
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label={t('settings.circuitFailureThreshold')} help={t('settings.circuitFailureThresholdHint')}>
                  <input
                    className="field"
                    type="number"
                    min="0"
                    value={gatewayForm.circuit_failure_threshold}
                    onChange={(e) =>
                      setGatewayForm({ ...gatewayForm, circuit_failure_threshold: int(e.target.value, 0) })
                    }
                  />
                </Field>
                <Field label={t('settings.circuitOpenDuration')} help={t('settings.circuitOpenDurationHint')}>
                  <input
                    className="field"
                    type="number"
                    min="1"
                    step="1"
                    value={seconds(gatewayForm.circuit_open_duration_ms)}
                    onChange={(e) =>
                      setGatewayForm({
                        ...gatewayForm,
                        circuit_open_duration_ms: secondsToMs(e.target.value, gatewayForm.circuit_open_duration_ms),
                      })
                    }
                  />
                </Field>
                <Field label={t('settings.circuitHalfOpenSuccess')} help={t('settings.circuitHalfOpenSuccessHint')}>
                  <input
                    className="field"
                    type="number"
                    min="1"
                    value={gatewayForm.circuit_half_open_success}
                    onChange={(e) =>
                      setGatewayForm({ ...gatewayForm, circuit_half_open_success: int(e.target.value, 1) })
                    }
                  />
                </Field>
                <Field label={t('settings.failoverMaxAttempts')} help={t('settings.failoverMaxAttemptsHint')}>
                  <input
                    className="field"
                    type="number"
                    min="1"
                    max="10"
                    value={gatewayForm.failover_max_attempts}
                    onChange={(e) =>
                      setGatewayForm({ ...gatewayForm, failover_max_attempts: int(e.target.value, 1) })
                    }
                  />
                </Field>
              </div>
            </section>

            {updateGateway.error && (
              <p className="text-sm text-danger">
                {updateGateway.error instanceof ApiError ? updateGateway.error.message : t('settings.saveError')}
              </p>
            )}
            <div className="flex items-center gap-3">
              <button type="submit" disabled={updateGateway.isPending} className="btn btn-primary">
                {updateGateway.isPending ? t('common.saving') : t('settings.saveGatewaySettings')}
              </button>
              {gatewaySaved && !updateGateway.isPending && (
                <span className="text-sm text-muted">{t('settings.gatewaySaved')}</span>
              )}
            </div>
          </form>
        )}
      </div>
    </SettingsLayout>
  )
}

function Field({ label, help, children }: { label: string; help?: string; children: React.ReactNode }) {
  return (
    <label className="block space-y-1">
      <span className="text-sm text-muted">{label}</span>
      {children}
      {help && <span className="block text-xs text-muted">{help}</span>}
    </label>
  )
}
