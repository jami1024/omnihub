import { FormEvent, useState } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'
import { ApiError } from '../lib/api'
import { useAuth } from '../lib/auth'
import { useI18n } from '../lib/i18n'

export function LoginPage() {
  const { t } = useI18n()
  const { me, login } = useAuth()
  const navigate = useNavigate()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  if (me) {
    return <Navigate to="/admin" replace />
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setBusy(true)
    try {
      await login(username, password)
      navigate('/admin', { replace: true })
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message)
      } else {
        setError(t('login.unexpectedError'))
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="relative min-h-screen overflow-hidden bg-bg text-ink">
      <div
        className="pointer-events-none absolute inset-0"
        style={{
          background:
            'radial-gradient(circle at 18% 20%, color-mix(in oklch, var(--brand-subtle) 72%, transparent), transparent 34%), linear-gradient(135deg, var(--surface) 0%, var(--bg) 52%, var(--surface-2) 100%)',
        }}
      />
      <div
        className="pointer-events-none absolute left-1/2 top-0 hidden h-full w-px bg-line lg:block"
        aria-hidden
      />

      <main className="relative mx-auto grid min-h-screen w-full max-w-6xl items-center gap-10 px-6 py-10 lg:grid-cols-[1.05fr_0.95fr] lg:px-8">
        <section className="hidden max-w-xl lg:block" aria-labelledby="login-intro-title">
          <div className="mb-12 flex items-center gap-3">
            <BrandMark />
            <div>
              <p className="text-sm font-semibold tracking-tight">OmniHub</p>
              <p className="text-xs text-muted">{t('login.gatewayControlPlane')}</p>
            </div>
          </div>

          <p className="mb-4 text-xs font-medium uppercase tracking-[0.18em] text-muted">
            {t('login.operatorAccess')}
          </p>
          <h1
            id="login-intro-title"
            className="max-w-[12ch] text-5xl font-semibold leading-[0.98] tracking-[-0.045em]"
          >
            {t('login.heroTitle')}
          </h1>
          <p className="mt-6 max-w-md text-base leading-7 text-muted">
            {t('login.heroSubtitle')}
          </p>

          <GatewayPath />
        </section>

        <section className="mx-auto w-full max-w-md lg:mx-0 lg:justify-self-end">
          <div className="mb-8 flex items-center justify-center gap-3 lg:hidden">
            <BrandMark />
            <div>
              <p className="text-sm font-semibold tracking-tight">OmniHub</p>
              <p className="text-xs text-muted">{t('login.adminConsole')}</p>
            </div>
          </div>

          <form
            onSubmit={onSubmit}
            aria-busy={busy}
            className="card relative overflow-hidden p-6 shadow-panel sm:p-7"
          >
            <header className="mb-6">
              <p className="mb-2 text-xs font-medium uppercase tracking-[0.16em] text-muted">
                {t('login.secureSignIn')}
              </p>
              <h2 className="text-2xl font-semibold tracking-tight">{t('login.enterControlPlane')}</h2>
              <p className="mt-2 text-sm leading-6 text-muted">
                {t('login.formSubtitle')}
              </p>
            </header>

            <div className="space-y-4">
              <label className="block">
                <span className="text-sm font-medium">{t('login.username')}</span>
                <input
                  type="text"
                  autoFocus
                  autoComplete="username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  className="field mt-1.5 h-10"
                  placeholder={t('login.usernamePlaceholder')}
                  required
                />
              </label>

              <label className="block">
                <span className="text-sm font-medium">{t('login.password')}</span>
                <input
                  type="password"
                  autoComplete="current-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  className="field mt-1.5 h-10"
                  placeholder="••••••••"
                  required
                />
              </label>
            </div>

            {error && (
              <p
                className="mt-4 rounded-lg bg-danger-bg px-3 py-2 text-sm text-danger"
                role="alert"
                aria-live="polite"
              >
                {error}
              </p>
            )}

            <button
              type="submit"
              disabled={busy}
              className="btn btn-primary mt-6 h-10 w-full"
            >
              {busy ? t('login.checkingCredentials') : t('login.signIn')}
            </button>

            <div className="mt-5 flex items-center justify-between border-t border-line pt-4 text-xs text-muted">
              <span>{t('login.protectedSession')}</span>
              <span className="inline-flex items-center gap-1.5">
                <span className="h-1.5 w-1.5 rounded-full bg-success" />
                {t('login.ready')}
              </span>
            </div>
          </form>
        </section>
      </main>
    </div>
  )
}

function BrandMark() {
  return (
    <span
      className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg"
      style={{
        background: 'linear-gradient(135deg, var(--brand), var(--brand-2))',
        boxShadow: '0 4px 12px -4px var(--glow)',
      }}
    >
      <svg width="19" height="19" viewBox="0 0 24 24" fill="none" aria-hidden>
        <circle cx="12" cy="12" r="2.6" fill="white" />
        <circle cx="5" cy="6" r="1.8" fill="white" opacity="0.7" />
        <circle cx="19" cy="6" r="1.8" fill="white" opacity="0.7" />
        <circle cx="5" cy="18" r="1.8" fill="white" opacity="0.7" />
        <circle cx="19" cy="18" r="1.8" fill="white" opacity="0.7" />
        <path d="M12 12 5 6M12 12l7-6M12 12l-7 6M12 12l7 6" stroke="white" strokeWidth="1.2" opacity="0.55" />
      </svg>
    </span>
  )
}

function GatewayPath() {
  const { t } = useI18n()
  const steps = [
    ['AUTH', 'login.stepAuth'],
    ['POLICY', 'login.stepPolicy'],
    ['ROUTE', 'login.stepRoute'],
    ['OBSERVE', 'login.stepObserve'],
  ] as const

  return (
    <div className="mt-10 max-w-lg border-y border-line py-2">
      {steps.map(([label, valueKey]) => (
        <div key={label} className="grid grid-cols-[5rem_1fr] items-center gap-4 py-3">
          <span className="font-mono text-[11px] font-medium tracking-[0.16em] text-muted">
            {label}
          </span>
          <span className="text-sm text-ink">{t(valueKey)}</span>
        </div>
      ))}
    </div>
  )
}
