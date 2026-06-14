// Data layer for the /admin/api/proxies surface: egress proxies as a
// first-class resource that accounts bind to by id (migration 0038).
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'

// Proxy mirrors the server's proxyDTO. The password is write-only: the
// server returns only has_password, never the secret.
export interface Proxy {
  id: number
  name: string
  protocol: string
  host: string
  port: number
  username: string
  has_password: boolean
  status: string
  expires_at: number | null
  fallback_mode: string
  backup_proxy_id: number | null
}

// ProxyInput is the create/update body. On update, omit password
// (undefined) to keep the stored secret; "" clears it. expires_at is
// unix seconds (null = never).
export interface ProxyInput {
  name: string
  protocol: string
  host: string
  port: number
  username: string
  password?: string
  status: string
  expires_at: number | null
  fallback_mode: string
  backup_proxy_id: number | null
}

const PROXIES_KEY = ['proxies'] as const

export function useProxies() {
  return useQuery({
    queryKey: PROXIES_KEY,
    queryFn: () => api<{ proxies: Proxy[] }>('/proxies').then((r) => r.proxies),
  })
}

export function useCreateProxy() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: ProxyInput) =>
      api<Proxy>('/proxies', { method: 'POST', body: JSON.stringify(input) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: PROXIES_KEY }),
  })
}

export function useUpdateProxy() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: ProxyInput }) =>
      api<Proxy>(`/proxies/${id}`, { method: 'PATCH', body: JSON.stringify(input) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: PROXIES_KEY }),
  })
}

export function useDeleteProxy() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api<void>(`/proxies/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: PROXIES_KEY })
      qc.invalidateQueries({ queryKey: ['accounts'] })
    },
  })
}

export interface ProxyTestResult {
  status: 'green' | 'red' | string
  http_status?: number
  latency_ms?: number
  message: string
}

export function useTestProxy() {
  return useMutation({
    mutationFn: (id: number) => api<ProxyTestResult>(`/proxies/${id}/test`, { method: 'POST' }),
  })
}
