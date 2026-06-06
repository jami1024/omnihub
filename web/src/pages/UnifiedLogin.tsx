import { FormEvent, useMemo, useState } from 'react'
import { Link, Navigate, useNavigate, useSearchParams } from 'react-router-dom'
import { setToken as setAdminToken } from '../lib/api'
import { setToken as setPortalToken } from '../lib/portalApi'
import { useI18n } from '../lib/i18n'
import { unifiedLogin } from '../lib/unifiedAuth'
import { usePortalAuth } from '../lib/portalAuth'

export function UnifiedLoginPage() {
  const { t } = useI18n()
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const initialMode = params.get('mode') === 'signup' ? 'signup' : 'login'
  const [mode, setMode] = useState<'login' | 'signup'>(initialMode)
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const { me, signup } = usePortalAuth()
  const isSignup = mode === 'signup'

  const next = useMemo(() => {
    const raw = params.get('next')
    return raw === '/admin' || raw === '/portal' ? raw : null
  }, [params])

  if (me && next !== '/admin') return <Navigate to="/portal" replace />

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setBusy(true)
    try {
      if (isSignup) {
        await signup(email, password)
        navigate('/portal', { replace: true })
        return
      }
      const res = await unifiedLogin(email, password)
      if (res.role === 'admin') {
        setPortalToken(null)
        setAdminToken(res.token)
      } else {
        setAdminToken(null)
        setPortalToken(res.token)
      }
      navigate(res.redirect_to, { replace: true })
    } catch (err) {
      const code = err && typeof err === 'object' && 'code' in err ? String((err as { code?: string }).code) : ''
      if (code === 'admin_email_reserved' || code === 'email_taken') {
        setError(t('login.adminEmailReserved'))
      } else {
        setError(err instanceof Error ? err.message : t('login.unexpectedError'))
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="relative min-h-screen overflow-hidden bg-bg text-ink">
      <div
        className="login-ambient pointer-events-none absolute inset-0"
        style={{
          background:
            'radial-gradient(circle at 18% 20%, color-mix(in oklch, var(--brand-subtle) 72%, transparent), transparent 34%), linear-gradient(135deg, var(--surface) 0%, var(--bg) 52%, var(--surface-2) 100%)',
        }}
      />
      <div className="login-right-surface pointer-events-none absolute right-0 top-0 hidden h-full w-[43%] border-l border-line bg-surface/62 lg:block" aria-hidden />

      <main className="relative mx-auto grid min-h-screen w-full max-w-6xl items-center gap-12 px-6 py-10 lg:grid-cols-[57%_43%] lg:gap-0 lg:px-8">
        <section className="login-left-panel hidden max-w-2xl pl-4 lg:block xl:pl-8" aria-labelledby="unified-login-title">
          <div className="mb-14 flex items-center gap-3">
            <BrandMark />
            <div>
              <p className="text-sm font-semibold tracking-tight">OmniHub</p>
              <p className="text-xs text-muted">{t('login.gatewayControlPlane')}</p>
            </div>
          </div>

          <p className="mb-4 text-xs font-medium uppercase tracking-[0.18em] text-muted">
            {t('login.secureSignIn')}
          </p>
          <h1 id="unified-login-title" className="login-hero-title max-w-[10ch] text-6xl font-semibold leading-[0.96] tracking-[-0.055em]">
            {t('login.heroTitle')}
          </h1>
          <p className="mt-6 max-w-md text-base leading-7 text-muted">
            {t('login.heroSubtitle')}
          </p>

          <GatewaySignalCard />
        </section>

        <section className="login-form-panel mx-auto w-full max-w-md lg:mx-0 lg:justify-self-end lg:pl-14 lg:pt-24 xl:pl-16">
          <div className="mb-8 flex items-center justify-center gap-3 lg:hidden">
            <BrandMark />
            <div>
              <p className="text-sm font-semibold tracking-tight">OmniHub</p>
              <p className="text-xs text-muted">{t('login.gatewayControlPlane')}</p>
            </div>
          </div>

          <form onSubmit={onSubmit} aria-busy={busy} className="login-form-card relative rounded-3xl p-1 sm:p-2 lg:p-4">
            <header className="mb-6">
              <p className="mb-2 text-xs font-medium uppercase tracking-[0.16em] text-muted">
                {isSignup ? t('portalLogin.userPortal') : t('login.secureSignIn')}
              </p>
              <h2 className="text-2xl font-semibold tracking-tight">
                {isSignup ? t('portalLogin.signupTitle') : t('login.enterControlPlane')}
              </h2>
              <p className="mt-2 text-sm leading-6 text-muted">
                {isSignup ? t('portalLogin.signupSubtitle') : t('login.formSubtitle')}
              </p>
            </header>

            <div className="space-y-4">
              <label className="block">
                <span className="text-sm font-medium">{t('login.email')}</span>
                <input
                  type="email"
                  autoFocus
                  autoComplete="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  className="field login-field mt-1.5 h-10"
                  placeholder={t('login.emailPlaceholder')}
                  required
                />
              </label>

              <label className="block">
                <span className="text-sm font-medium">{t('login.password')}</span>
                <input
                  type="password"
                  autoComplete={isSignup ? 'new-password' : 'current-password'}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  className="field login-field mt-1.5 h-10"
                  placeholder="••••••••"
                  required
                />
                {isSignup && <span className="mt-1.5 block text-xs text-muted">{t('portalLogin.passwordHint')}</span>}
              </label>
            </div>

            {error && (
              <p className="mt-4 rounded-lg bg-danger-bg px-3 py-2 text-sm text-danger" role="alert" aria-live="polite">
                {error}
              </p>
            )}

            <button type="submit" disabled={busy} className="btn btn-primary login-submit mt-6 h-10 w-full">
              {busy ? t('login.checkingCredentials') : isSignup ? t('portalLogin.createAccount') : t('login.signIn')}
            </button>

            <div className="mt-5 border-t border-line pt-4 text-center text-sm text-muted">
              {isSignup ? t('portalLogin.alreadyHaveAccount') : t('portalLogin.newHere')}{' '}
              <button
                type="button"
                onClick={() => {
                  setMode(isSignup ? 'login' : 'signup')
                  setError(null)
                }}
                className="inline-flex min-h-10 items-center rounded-md px-2 font-medium text-brand hover:bg-surface-2 hover:underline"
              >
                {isSignup ? t('login.signIn') : t('portalLogin.createOne')}
              </button>
            </div>
          </form>

          <Link to="/" className="login-back-home group mx-auto mt-6 flex min-h-10 w-fit items-center gap-2 rounded-full px-3 text-sm text-muted">
            <span className="login-back-home-orbit" aria-hidden>
              <svg width="15" height="15" viewBox="0 0 16 16" fill="none">
                <path d="M9.5 4.5 6 8l3.5 3.5" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
                <path d="M6.4 8h6.1" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
                <circle cx="3" cy="8" r="1.15" fill="currentColor" />
              </svg>
            </span>
            <span>{t('portalLogin.backHome')}</span>
          </Link>
        </section>
      </main>
    </div>
  )
}

function GatewaySignalCard() {
  const { t } = useI18n()
  return (
    <div className="login-signal-card mt-12 max-w-lg overflow-hidden rounded-2xl border border-line bg-surface/72 p-5 shadow-sm">
      <div className="flex items-center justify-between gap-4">
        <div>
          <p className="font-mono text-[11px] uppercase tracking-[0.18em] text-muted">{t('login.signalEyebrow')}</p>
          <p className="mt-2 text-lg font-semibold tracking-tight">{t('login.signalTitle')}</p>
        </div>
        <span className="login-status-pill rounded-full border border-line bg-surface-2 px-3 py-1 font-mono text-[11px] text-muted">
          {t('login.signalStatus')}
        </span>
      </div>

      <div className="mt-5 grid gap-3">
        <SignalRow label={t('login.signalRoute')} value={t('login.signalRouteValue')} tone="brand" />
        <SignalRow label={t('login.signalCost')} value={t('login.signalCostValue')} tone="success" />
      </div>
    </div>
  )
}

function SignalRow({ label, value, tone }: { label: string; value: string; tone: 'brand' | 'success' }) {
  const dotClass = tone === 'brand' ? 'bg-brand' : 'bg-success'
  return (
    <div className="login-signal-row flex items-center justify-between gap-4 rounded-xl border border-line bg-surface-2 px-3 py-2.5">
      <div className="flex items-center gap-2">
        <span className={`login-signal-dot h-2 w-2 rounded-full ${dotClass}`} aria-hidden />
        <span className="text-sm text-muted">{label}</span>
      </div>
      <span className="font-mono text-xs text-ink">
        {value}
      </span>
    </div>
  )
}

function BrandMark() {
  return (
    <span
      className="login-brand-mark flex h-9 w-9 shrink-0 items-center justify-center rounded-lg"
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
