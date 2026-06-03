import { useEffect, useState } from 'react'
import { Layout } from '../components/Layout'
import { ErrorNotice, PageHeader } from '../components/PageChrome'
import { ApiError } from '../lib/api'
import { useI18n } from '../lib/i18n'
import { useSettings, useUpdateSettings, type PortalSettings } from '../lib/settings'

// Settings is the admin control for the end-user portal policy: whether
// open registration is allowed, and the default / ceiling limits clamped
// onto keys that portal users create for themselves.
export function SettingsPage() {
  const { t } = useI18n()
  const { data, isLoading, error } = useSettings()
  const update = useUpdateSettings()
  const [form, setForm] = useState<PortalSettings | null>(null)
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    if (data && !form) setForm(data)
  }, [data, form])

  function num(v: string): number | null {
    const t = v.trim()
    if (t === '') return null
    const n = Number(t)
    return Number.isFinite(n) ? n : null
  }

  function save(e: React.FormEvent) {
    e.preventDefault()
    if (!form) return
    setSaved(false)
    update.mutate(form, { onSuccess: () => setSaved(true) })
  }

  return (
    <Layout>
      <main className="mx-auto w-full max-w-3xl px-6 py-8">
        <PageHeader
          eyebrow={t('settings.eyebrow')}
          context={t('settings.context')}
          title={t('settings.title')}
          description={t('settings.description')}
        />

        {isLoading && <p className="text-sm text-muted">{t('common.loading')}</p>}
        {error && <ErrorNotice>{error instanceof ApiError ? error.message : t('settings.loadError')}</ErrorNotice>}

        {form && (
          <form onSubmit={save} className="space-y-6">
            <section className="card p-5">
              <label className="flex items-start gap-3">
                <input
                  type="checkbox"
                  className="mt-1"
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
              <div className="grid grid-cols-3 gap-4">
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
      </main>
    </Layout>
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
