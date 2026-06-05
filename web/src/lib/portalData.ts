import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { papi } from './portalApi'

export interface PortalKey {
  id: number
  name: string
  enabled: boolean
  daily_usd_limit: number | null
  rpm_limit: number | null
  allowed_models: string[]
  spend_24h: number
}
export interface CreatePortalKeyResult extends PortalKey {
  key: string
}
export interface PortalKeyInput {
  name: string
  daily_usd_limit: number | null
  rpm_limit: number | null
  allowed_models: string[]
}

export interface PortalUsage {
  window_days: number
  summary: {
    requests: number
    cost_usd: number
    input_tokens: number
    output_tokens: number
    errors: number
  }
  daily: { day: string; requests: number; cost_usd: number; input_tokens: number; output_tokens: number }[]
  by_model: { model: string; requests: number; cost_usd: number; input_tokens: number; output_tokens: number }[]
}

const KEYS = ['portal-keys'] as const

export function usePortalKeys() {
  return useQuery({ queryKey: KEYS, queryFn: () => papi<{ keys: PortalKey[] }>('/keys').then((r) => r.keys) })
}
export function useCreatePortalKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: PortalKeyInput) =>
      papi<CreatePortalKeyResult>('/keys', { method: 'POST', body: JSON.stringify(input) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEYS }),
  })
}
export function useDeletePortalKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => papi<void>(`/keys/${id}`, { method: 'DELETE' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEYS }),
  })
}
export function usePortalUsage(days: number) {
  return useQuery({ queryKey: ['portal-usage', days], queryFn: () => papi<PortalUsage>(`/usage?days=${days}`) })
}

export interface PortalRequestRow {
  created_at: string
  key_name: string
  model: string
  status_code: number | null
  input_tokens: number
  output_tokens: number
  cost_usd: number
  duration_ms: number | null
  error: string
}

export interface PortalRequests {
  window_days: number
  page: number
  page_size: number
  total: number
  requests: PortalRequestRow[]
}

export function usePortalRequests(days: number, page: number) {
  return useQuery({
    queryKey: ['portal-requests', days, page],
    queryFn: () => papi<PortalRequests>(`/requests?days=${days}&page=${page}`),
    placeholderData: (prev) => prev, // keep the table visible while paging
  })
}

export interface WalletEntry {
  kind: string
  amount_usd: number
  note: string
  created_at: string
}
export interface PortalWallet {
  balance: number
  credits: number
  spent: number
  entries: WalletEntry[]
}

export function usePortalWallet() {
  return useQuery({ queryKey: ['portal-wallet'], queryFn: () => papi<PortalWallet>('/wallet') })
}
