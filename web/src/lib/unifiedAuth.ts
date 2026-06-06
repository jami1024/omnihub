import { ApiError } from './api'

export type LoginRole = 'admin' | 'user'

export interface LoginResponse {
  token: string
  expires_at: number
  username: string
  email: string
  role: LoginRole
  redirect_to: '/admin' | '/portal'
}

export async function unifiedLogin(email: string, password: string): Promise<LoginResponse> {
  const res = await fetch('/auth/api/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password }),
  })
  const body = await res.json().catch(() => null)
  if (!res.ok) {
    const err = (body ?? {}) as { error?: { message?: string; code?: string } }
    throw new ApiError(
      res.status,
      err.error?.code ?? `http_${res.status}`,
      err.error?.message ?? `request failed (${res.status})`,
    )
  }
  return body as LoginResponse
}
