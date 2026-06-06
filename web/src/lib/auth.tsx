import { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { ApiError, api, getToken, setToken } from './api'

export interface Me {
  id: number
  username: string
}

interface AuthState {
  me: Me | null
  loading: boolean
  login(email: string, password: string): Promise<void>
  logout(): void
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [me, setMe] = useState<Me | null>(null)
  const [loading, setLoading] = useState<boolean>(true)

  // On boot, if a token is cached, probe /me to validate it. The
  // result decides whether the SPA routes to /login or to the
  // protected layout.
  useEffect(() => {
    let cancelled = false
    if (!getToken()) {
      setLoading(false)
      return
    }
    ;(async () => {
      try {
        const profile = await api<Me>('/me')
        if (!cancelled) setMe(profile)
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) {
          setToken(null)
        }
        // Other errors (5xx, network) keep the token; the user can
        // retry from the login screen.
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  const login = useCallback(async (email: string, password: string) => {
    const res = await api<{ token: string; expires_at: number; username: string; email?: string }>(
      '/login',
      {
        method: 'POST',
        body: JSON.stringify({ email, password }),
      },
    )
    setToken(res.token)
    setMe({ id: 0, username: res.username }) // id unknown until /me; refetch:
    try {
      const profile = await api<Me>('/me')
      setMe(profile)
    } catch {
      // Keep the optimistic me from login; the protected layout will
      // probe again on next mount.
    }
  }, [])

  const logout = useCallback(() => {
    setToken(null)
    setMe(null)
  }, [])

  return (
    <AuthContext.Provider value={{ me, loading, login, logout }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used inside <AuthProvider>')
  return ctx
}
