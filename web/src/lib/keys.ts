// Data layer for the /admin/api/keys surface. A key's secret value is
// never returned by the API; it is surfaced exactly once in the create
// response (CreateKeyResult.key) and is unrecoverable thereafter.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'

// Key mirrors the server's keyDTO — metadata only, no hash, no value.
export interface Key {
  id: number
  name: string
  label: string
  enabled: boolean
  daily_usd_limit: number | null
  rpm_limit: number | null
  allowed_models: string[]
}

// CreateKeyResult is the 201 body: the new key's metadata plus the
// one-time cleartext.
export interface CreateKeyResult extends Key {
  key: string
}

// KeyInput is the create/update body. The key VALUE is never sent — the
// server always generates it. Update is a full-metadata replace, so a
// null limit clears it.
export interface KeyInput {
  name: string
  label: string
  enabled: boolean
  daily_usd_limit: number | null
  rpm_limit: number | null
  allowed_models: string[]
}

const KEYS_KEY = ['keys'] as const

export function useKeys() {
  return useQuery({
    queryKey: KEYS_KEY,
    queryFn: () => api<{ keys: Key[] }>('/keys').then((r) => r.keys),
  })
}

export function useCreateKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: KeyInput) =>
      api<CreateKeyResult>('/keys', { method: 'POST', body: JSON.stringify(input) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEYS_KEY }),
  })
}

export function useUpdateKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: KeyInput }) =>
      api<Key>(`/keys/${id}`, { method: 'PATCH', body: JSON.stringify(input) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEYS_KEY }),
  })
}

export function useDeleteKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api<void>(`/keys/${id}`, { method: 'DELETE' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEYS_KEY }),
  })
}
