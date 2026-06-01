import { FormEvent, useState } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'
import { ApiError } from '../../lib/portalApi'
import { usePortalAuth } from '../../lib/portalAuth'

// PortalLogin handles both sign in and open registration, toggled in
// place. Registration succeeds straight into a session.
export function PortalLoginPage({ initialMode = 'login' }: { initialMode?: 'login' | 'signup' }) {
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
      setError(err instanceof ApiError ? err.message : 'Something went wrong.')
    } finally {
      setBusy(false)
    }
  }

  const isSignup = mode === 'signup'
  return (
    <div className="flex min-h-screen items-center justify-center bg-bg px-4 text-ink">
      <form onSubmit={onSubmit} className="card w-full max-w-sm space-y-4 p-6 shadow-panel">
        <header className="space-y-1">
          <div className="flex items-center gap-2">
            <span
              className="flex h-8 w-8 items-center justify-center rounded-lg"
              style={{ background: 'var(--brand)', color: 'var(--brand-ink)' }}
            >
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden>
                <circle cx="12" cy="12" r="2.6" fill="currentColor" />
                <circle cx="5" cy="6" r="1.8" fill="currentColor" opacity="0.7" />
                <circle cx="19" cy="6" r="1.8" fill="currentColor" opacity="0.7" />
                <circle cx="5" cy="18" r="1.8" fill="currentColor" opacity="0.7" />
                <circle cx="19" cy="18" r="1.8" fill="currentColor" opacity="0.7" />
              </svg>
            </span>
            <span className="text-lg font-semibold tracking-tight">OmniHub</span>
          </div>
          <p className="text-sm text-muted">
            {isSignup ? 'Create an account to get an API key.' : 'Sign in to your account.'}
          </p>
        </header>

        <label className="block space-y-1">
          <span className="text-sm font-medium">Username</span>
          <input className="field" autoFocus autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} required />
        </label>
        <label className="block space-y-1">
          <span className="text-sm font-medium">Password</span>
          <input
            className="field"
            type="password"
            autoComplete={isSignup ? 'new-password' : 'current-password'}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
          {isSignup && <span className="text-xs text-muted">At least 8 characters.</span>}
        </label>

        {error && <p className="rounded-lg bg-danger-bg px-3 py-2 text-sm text-danger">{error}</p>}

        <button type="submit" disabled={busy} className="btn btn-primary w-full py-2">
          {busy ? 'Please wait…' : isSignup ? 'Create account' : 'Sign in'}
        </button>

        <p className="text-center text-sm text-muted">
          {isSignup ? 'Already have an account?' : 'New here?'}{' '}
          <button
            type="button"
            onClick={() => {
              setMode(isSignup ? 'login' : 'signup')
              setError(null)
            }}
            className="font-medium text-brand hover:underline"
          >
            {isSignup ? 'Sign in' : 'Create one'}
          </button>
        </p>
      </form>
    </div>
  )
}
