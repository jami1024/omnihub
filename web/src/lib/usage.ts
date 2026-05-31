// Data layer for the /admin/api/usage dashboard endpoint — a single
// fetch returning headline totals, a gap-filled daily series, and a
// per-model breakdown for the trailing N days.
import { useQuery } from '@tanstack/react-query'
import { api } from './api'

export interface UsageTotals {
  requests: number
  cost_usd: number
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  errors: number
}

export interface DailyUsage {
  day: string // ISO timestamp at UTC midnight
  requests: number
  cost_usd: number
  input_tokens: number
  output_tokens: number
}

export interface ModelUsage {
  model: string
  requests: number
  cost_usd: number
  input_tokens: number
  output_tokens: number
}

export interface UsageReport {
  window_days: number
  since: string
  summary: UsageTotals
  daily: DailyUsage[]
  by_model: ModelUsage[]
}

export function useUsage(days: number) {
  return useQuery({
    queryKey: ['usage', days],
    queryFn: () => api<UsageReport>(`/usage?days=${days}`),
  })
}
