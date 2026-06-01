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
