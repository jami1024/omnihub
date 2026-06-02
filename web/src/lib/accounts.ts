// Data layer for the /admin/api/accounts surface: the redacted DTO the
// server returns, the create/update request shape, and the TanStack
// Query hooks the Accounts page binds to.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'

// Account mirrors the server's accountDTO. Credentials are write-only:
// the server never returns secret VALUES, only the key names that are
// configured (credentialKeys), so the UI can show "api_key, aws_region"
// without ever holding the secrets.
// ModelRedirectMatch mirrors the server's match types. Rules rewrite a
// requested model name to a different upstream model before dispatch;
// first match wins.
export type ModelRedirectMatch = 'exact' | 'prefix' | 'suffix' | 'contains' | 'regex'

export interface ModelRedirect {
  match_type: ModelRedirectMatch
  source: string
  target: string
}

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
  model_redirects: ModelRedirect[]
  daily_usd_limit: number | null
  total_usd_limit: number | null
  group_id: number | null
  group_name: string
  custom_headers: Record<string, string>
  endpoints: string[]
  health_probe_enabled: boolean | null
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
  model_redirects: ModelRedirect[]
  daily_usd_limit: number | null
  total_usd_limit: number | null
  group_id: number | null
  custom_headers: Record<string, string>
  endpoints: string[]
  health_probe_enabled: boolean | null
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

// TestResult is the connectivity-probe verdict (green/yellow/red).
export interface TestResult {
  status: 'green' | 'yellow' | 'red' | string
  http_status?: number
  latency_ms: number
  message: string
}

// useTestAccount probes the form's provider/base_url/credentials before
// saving (the credentials must be present in the body).
export function useTestAccount() {
  return useMutation({
    mutationFn: (input: { provider: string; base_url: string; credentials: Record<string, string> }) =>
      api<TestResult>('/accounts/test', { method: 'POST', body: JSON.stringify(input) }),
  })
}

// useTestAccountById probes an existing account using its stored
// (write-only) credentials — used when editing without re-entering them.
export function useTestAccountById() {
  return useMutation({
    mutationFn: (id: number) => api<TestResult>(`/accounts/${id}/test`, { method: 'POST' }),
  })
}

export function useDeleteAccount() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api<void>(`/accounts/${id}`, { method: 'DELETE' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ACCOUNTS_KEY }),
  })
}
