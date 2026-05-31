// Data layer for the /admin/api/accounts surface: the redacted DTO the
// server returns, the create/update request shape, and the TanStack
// Query hooks the Accounts page binds to.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'

// Account mirrors the server's accountDTO. Credentials are write-only:
// the server never returns secret VALUES, only the key names that are
// configured (credentialKeys), so the UI can show "api_key, aws_region"
// without ever holding the secrets.
export interface Account {
  id: number
  name: string
  provider: string
  enabled: boolean
  weight: number
  priority: number
  cost_multiplier: number
  base_url: string
  credential_keys: string[]
  circuit_failure_threshold: number | null
  circuit_open_duration_ms: number | null
  circuit_half_open_success: number | null
}

// AccountInput is the create/update body. On create, credentials is
// required. On update, omit credentials (undefined) to keep the stored
// secret untouched — never send an empty object, the server rejects it.
export interface AccountInput {
  name: string
  provider: string
  enabled: boolean
  weight: number
  priority: number
  cost_multiplier: number
  base_url: string
  credentials?: Record<string, string>
  circuit_failure_threshold: number | null
  circuit_open_duration_ms: number | null
  circuit_half_open_success: number | null
}

const ACCOUNTS_KEY = ['accounts'] as const

export function useAccounts() {
  return useQuery({
    queryKey: ACCOUNTS_KEY,
    queryFn: () => api<{ accounts: Account[] }>('/accounts').then((r) => r.accounts),
  })
}

export function useCreateAccount() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: AccountInput) =>
      api<Account>('/accounts', { method: 'POST', body: JSON.stringify(input) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ACCOUNTS_KEY }),
  })
}

export function useUpdateAccount() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: AccountInput }) =>
      api<Account>(`/accounts/${id}`, { method: 'PATCH', body: JSON.stringify(input) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ACCOUNTS_KEY }),
  })
}

export function useDeleteAccount() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api<void>(`/accounts/${id}`, { method: 'DELETE' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ACCOUNTS_KEY }),
  })
}
