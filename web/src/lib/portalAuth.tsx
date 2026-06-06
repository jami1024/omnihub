import { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { ApiError, getToken, papi, setToken } from './portalApi'

export interface PortalMe {
  id: number
  username: string
  email: string
}

interface PortalAuthState {
  me: PortalMe | null
  loading: boolean
  login(email: string, password: string): Promise<void>
  signup(email: string, password: string): Promise<void>
  logout(): void
}

const Ctx = createContext<PortalAuthState | null>(null)

export function PortalAuthProvider({ children }: { children: React.ReactNode }) {
  const [me, setMe] = useState<PortalMe | null>(null)
  const [loading, setLoading] = useState(true)

  // On boot, validate a cached token by probing /me.
  useEffect(() => {
    let cancelled = false
    if (!getToken()) {
      setLoading(false)
      return
    }
    ;(async () => {
      try {
        const profile = await papi<PortalMe>('/me')
        if (!cancelled) setMe(profile)
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) setToken(null)
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  const authed = useCallback(async (path: string, email: string, password: string) => {
    const res = await papi<{ token: string; username: string; email: string }>(path, {
      method: 'POST',
      body: JSON.stringify({ email, password }),
    })
    setToken(res.token)
    const profile = await papi<PortalMe>('/me')
    setMe(profile)
  }, [])

  const login = useCallback((u: string, p: string) => authed('/login', u, p), [authed])
  const signup = useCallback((u: string, p: string) => authed('/signup', u, p), [authed])
  const logout = useCallback(() => {
    setToken(null)
    setMe(null)
  }, [])

  return <Ctx.Provider value={{ me, loading, login, signup, logout }}>{children}</Ctx.Provider>
}

export function usePortalAuth(): PortalAuthState {
  const ctx = useContext(Ctx)
  if (!ctx) throw new Error('usePortalAuth must be used inside <PortalAuthProvider>')
  return ctx
}
