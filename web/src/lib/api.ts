// Tiny fetch wrapper for the /admin/api/* surface. Attaches the saved
// Bearer token, parses the canonical {error: {message, type, code}}
// envelope into a thrown Error, and surfaces 401 specially so the
// AuthProvider can clear the token and route to /login.

const TOKEN_STORAGE_KEY = 'omnihub.admin.token'

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
  if (token) {
    localStorage.setItem(TOKEN_STORAGE_KEY, token)
  } else {
    localStorage.removeItem(TOKEN_STORAGE_KEY)
  }
}

export async function api<T = unknown>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const headers = new Headers(init.headers)
  if (!headers.has('Content-Type') && init.body) {
    headers.set('Content-Type', 'application/json')
  }
  const token = getToken()
  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }

  const res = await fetch(`/admin/api${path}`, { ...init, headers })

  if (res.status === 204) {
    return undefined as T
  }

  let body: unknown = null
  const contentType = res.headers.get('Content-Type') ?? ''
  if (contentType.includes('application/json')) {
    body = await res.json().catch(() => null)
  } else {
    body = await res.text().catch(() => null)
  }

  if (!res.ok) {
    const envelope = (body ?? {}) as {
      error?: { message?: string; type?: string; code?: string }
    }
    const err = envelope.error ?? {}
    throw new ApiError(
      res.status,
      err.code ?? `http_${res.status}`,
      err.message ?? `request failed (${res.status})`,
    )
  }

  return body as T
}
