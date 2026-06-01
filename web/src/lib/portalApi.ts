// Fetch wrapper for the /portal/api/* surface — the end-user portal.
// Mirrors lib/api.ts but with its own token (so an admin and a user
// session can coexist in one browser) and base path.
const TOKEN_STORAGE_KEY = 'omnihub.user.token'

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  constructor(status: number, code: string, message: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_STORAGE_KEY)
}
export function setToken(token: string | null) {
  if (token) localStorage.setItem(TOKEN_STORAGE_KEY, token)
  else localStorage.removeItem(TOKEN_STORAGE_KEY)
}

export async function papi<T = unknown>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  if (!headers.has('Content-Type') && init.body) headers.set('Content-Type', 'application/json')
  const token = getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)

  const res = await fetch(`/portal/api${path}`, { ...init, headers })
  if (res.status === 204) return undefined as T

  let body: unknown = null
  const ct = res.headers.get('Content-Type') ?? ''
  body = ct.includes('application/json') ? await res.json().catch(() => null) : await res.text().catch(() => null)

  if (!res.ok) {
    const env = (body ?? {}) as { error?: { message?: string; type?: string; code?: string } }
    const err = env.error ?? {}
    throw new ApiError(res.status, err.code ?? `http_${res.status}`, err.message ?? `request failed (${res.status})`)
  }
  return body as T
}
