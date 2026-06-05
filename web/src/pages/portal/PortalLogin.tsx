import { FormEvent, useState } from 'react'
import { Link, Navigate, useNavigate } from 'react-router-dom'
import { ApiError } from '../../lib/portalApi'
import { usePortalAuth } from '../../lib/portalAuth'
import { useI18n } from '../../lib/i18n'

// PortalLogin handles both sign in and open registration, toggled in
// place. A graphite "control plane" brand panel sits beside the form on
// large screens and collapses to a compact header on mobile.
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
    <div className="grid min-h-screen bg-bg text-ink lg:grid-cols-[1.1fr_1fr]">
      <BrandPanel t={t} />

      <main className="flex items-center justify-center px-6 py-12">
        <form onSubmit={onSubmit} className="reveal w-full max-w-sm space-y-5">
          {/* Compact brand header for mobile (panel is hidden < lg). */}
          <div className="flex items-center gap-2 lg:hidden">
            <Logo />
            <span className="text-lg font-semibold tracking-tight">OmniHub</span>
          </div>

          <div className="space-y-1">
            <h1 className="text-xl font-semibold tracking-tight">
              {isSignup ? t('portalLogin.signupTitle') : t('portalLogin.loginTitle')}
            </h1>
            <p className="text-sm text-muted">
              {isSignup ? t('portalLogin.signupSubtitle') : t('portalLogin.loginSubtitle')}
            </p>
          </div>

          <label className="block space-y-1.5">
            <span className="text-sm font-medium">{t('portalLogin.username')}</span>
            <input className="field py-2" autoFocus autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} required />
          </label>
          <label className="block space-y-1.5">
            <span className="text-sm font-medium">{t('portalLogin.password')}</span>
            <input
              className="field py-2"
              type="password"
              autoComplete={isSignup ? 'new-password' : 'current-password'}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
            {isSignup && <span className="text-xs text-muted">{t('portalLogin.passwordHint')}</span>}
          </label>

          {error && <p className="rounded-lg bg-danger-bg px-3 py-2 text-sm text-danger">{error}</p>}

          <button type="submit" disabled={busy} className="btn btn-primary h-11 w-full">
            {busy ? t('portalLogin.pleaseWait') : isSignup ? t('portalLogin.createAccount') : t('portalLogin.signIn')}
          </button>

          <p className="text-center text-sm text-muted">
            {isSignup ? t('portalLogin.alreadyHaveAccount') : t('portalLogin.newHere')}{' '}
            <button
              type="button"
              onClick={() => {
                setMode(isSignup ? 'login' : 'signup')
                setError(null)
              }}
              className="font-medium text-brand hover:underline"
            >
              {isSignup ? t('portalLogin.signIn') : t('portalLogin.createOne')}
            </button>
          </p>
        </form>
      </main>
    </div>
  )
}

/* The graphite brand panel: a deliberate dark "control plane" moment that
   stays dark in both themes (art direction), with the real portal motifs
   a user is signing in to manage. */
function BrandPanel({ t }: { t: (k: string) => string }) {
  return (
    <aside
      className="relative hidden flex-col justify-between overflow-hidden p-10 lg:flex"
      style={{ background: 'linear-gradient(155deg, oklch(0.2 0.008 286), oklch(0.141 0.005 285.8))', color: 'oklch(0.985 0 0)' }}
    >
      <div aria-hidden className="pointer-events-none absolute -right-24 -top-24 h-96 w-96 rounded-full" style={{ background: 'radial-gradient(closest-side, oklch(0.55 0.06 255 / 0.35), transparent)' }} />

      <div className="relative flex items-center justify-between">
        <Link to="/" className="flex items-center gap-2">
          <Logo />
          <span className="text-lg font-semibold tracking-tight">OmniHub</span>
        </Link>
        <Link to="/" className="font-mono text-xs text-white/55 transition-colors hover:text-white/90">{t('portalLogin.backHome')}</Link>
      </div>

      <div className="relative">
        <h2 className="max-w-md text-balance text-3xl font-semibold tracking-tight" style={{ letterSpacing: '-0.02em' }}>
          {t('portalLogin.brandTitle')}
        </h2>
        <p className="mt-3 max-w-sm text-pretty text-sm leading-relaxed text-white/65">{t('portalLogin.brandSub')}</p>

        <div className="mt-8 max-w-sm space-y-px overflow-hidden rounded-xl border border-white/10 font-mono text-[13px]">
          <PanelRow label="key" value="omni-7f3c…a91" />
          <PanelRow label="balance" value="$9.9959" tinted />
          <PanelRow label="requests · 24h" value="1,284" />
          <PanelRow label="ttfb · p95" value="510ms" tinted />
        </div>
      </div>

      <p className="relative font-mono text-xs text-white/40">{t('portalLogin.brandFootnote')}</p>
    </aside>
  )
}

function PanelRow({ label, value, tinted }: { label: string; value: string; tinted?: boolean }) {
  return (
    <div className={`flex items-center justify-between px-4 py-2.5 ${tinted ? 'bg-white/[0.03]' : ''}`}>
      <span className="text-white/45">{label}</span>
      <span className="text-white/90">{value}</span>
    </div>
  )
}

function Logo() {
  return (
    <span className="flex h-8 w-8 items-center justify-center rounded-lg" style={{ background: 'linear-gradient(135deg, var(--brand), var(--brand-2))' }}>
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden>
        <circle cx="12" cy="12" r="2.6" fill="white" />
        <circle cx="5" cy="6" r="1.8" fill="white" opacity="0.7" />
        <circle cx="19" cy="6" r="1.8" fill="white" opacity="0.7" />
        <circle cx="5" cy="18" r="1.8" fill="white" opacity="0.7" />
        <circle cx="19" cy="18" r="1.8" fill="white" opacity="0.7" />
      </svg>
    </span>
  )
}
