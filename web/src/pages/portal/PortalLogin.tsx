import { FormEvent, useState } from 'react'
import { Link, Navigate, useNavigate } from 'react-router-dom'
import { ApiError } from '../../lib/portalApi'
import { usePortalAuth } from '../../lib/portalAuth'
import { useI18n } from '../../lib/i18n'

// PortalLogin mirrors the admin login composition: a calm product-intro
// column beside a standard card form. It keeps portal registration in the
// same place without changing the auth flow.
export function PortalLoginPage({ initialMode = 'login' }: { initialMode?: 'login' | 'signup' }) {
  const { t } = useI18n()
  const { me, login, signup } = usePortalAuth()
  const navigate = useNavigate()
  const [mode, setMode] = useState<'login' | 'signup'>(initialMode)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  if (me) return <Navigate to="/portal" replace />

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setBusy(true)
    try {
      if (mode === 'signup') await signup(username, password)
      else await login(username, password)
      navigate('/portal', { replace: true })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t('portalLogin.genericError'))
    } finally {
      setBusy(false)
    }
  }

  const isSignup = mode === 'signup'

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
        <section className="hidden max-w-xl lg:block" aria-labelledby="portal-login-intro-title">
          <div className="mb-12 flex items-center gap-3">
            <Logo />
            <div>
              <p className="text-sm font-semibold tracking-tight">OmniHub</p>
              <p className="text-xs text-muted">{t('portalLogin.brandFootnote')}</p>
            </div>
          </div>

          <p className="mb-4 text-xs font-medium uppercase tracking-[0.18em] text-muted">
            {t('portalLogin.userPortal')}
          </p>
          <h1
            id="portal-login-intro-title"
            className="max-w-[13ch] text-5xl font-semibold leading-[0.98] tracking-[-0.045em]"
          >
            {t('portalLogin.brandTitle')}
          </h1>
          <p className="mt-6 max-w-md text-base leading-7 text-muted">
            {t('portalLogin.brandSub')}
          </p>

          <PortalPath />
        </section>

        <section className="mx-auto w-full max-w-md lg:mx-0 lg:justify-self-end">
          <div className="mb-8 flex items-center justify-center gap-3 lg:hidden">
            <Logo />
            <div>
              <p className="text-sm font-semibold tracking-tight">OmniHub</p>
              <p className="text-xs text-muted">{t('portalLogin.brandFootnote')}</p>
            </div>
          </div>

          <form
            onSubmit={onSubmit}
            aria-busy={busy}
            className="card relative overflow-hidden p-6 shadow-panel sm:p-7"
          >
            <header className="mb-6">
              <p className="mb-2 text-xs font-medium uppercase tracking-[0.16em] text-muted">
                {t('portalLogin.portalAccess')}
              </p>
              <h2 className="text-2xl font-semibold tracking-tight">
                {isSignup ? t('portalLogin.signupTitle') : t('portalLogin.loginTitle')}
              </h2>
              <p className="mt-2 text-sm leading-6 text-muted">
                {isSignup ? t('portalLogin.signupSubtitle') : t('portalLogin.loginSubtitle')}
              </p>
            </header>

            <div className="space-y-4">
              <label className="block">
                <span className="text-sm font-medium">{t('portalLogin.username')}</span>
                <input
                  className="field mt-1.5 h-10"
                  autoFocus
                  autoComplete="username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  placeholder={t('portalLogin.username')}
                  required
                />
              </label>
              <label className="block">
                <span className="text-sm font-medium">{t('portalLogin.password')}</span>
                <input
                  className="field mt-1.5 h-10"
                  type="password"
                  autoComplete={isSignup ? 'new-password' : 'current-password'}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="••••••••"
                  required
                />
                {isSignup && <span className="mt-1.5 block text-xs text-muted">{t('portalLogin.passwordHint')}</span>}
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

            <button type="submit" disabled={busy} className="btn btn-primary mt-6 h-10 w-full">
              {busy ? t('portalLogin.pleaseWait') : isSignup ? t('portalLogin.createAccount') : t('portalLogin.signIn')}
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
                {isSignup ? t('portalLogin.signIn') : t('portalLogin.createOne')}
              </button>
            </div>
          </form>

          <Link to="/" className="mx-auto mt-5 flex min-h-10 w-fit items-center rounded-md px-2 text-sm text-muted transition-colors hover:bg-surface-2 hover:text-ink">
            {t('portalLogin.backHome')}
          </Link>
        </section>
      </main>
    </div>
  )
}

function PortalPath() {
  const rows = [
    ['KEY', 'omni-7f3c…a91'],
    ['BALANCE', '$9.9959'],
    ['REQUESTS', '1,284 · 24h'],
    ['TTFB', '510ms · p95'],
  ] as const

  return (
    <div className="mt-10 max-w-lg border-y border-line py-2">
      {rows.map(([label, value]) => (
        <div key={label} className="grid grid-cols-[5rem_1fr] items-center gap-4 py-3">
          <span className="font-mono text-[11px] font-medium tracking-[0.16em] text-muted">
            {label}
          </span>
          <span className="font-mono text-sm text-ink">{value}</span>
        </div>
      ))}
    </div>
  )
}

function Logo() {
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
