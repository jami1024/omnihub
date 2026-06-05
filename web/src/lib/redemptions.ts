import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'

export interface RedemptionBatch {
  batch_id: string
  amount_usd: number
  total: number
  redeemed: number
  expires_at: string | null
  created_by: string
  created_at: string
}

export interface GenerateResult {
  batch_id: string
  amount_usd: number
  codes: string[]
}

export interface GenerateInput {
  count: number
  amount_usd: number
  expires_in_days?: number
}

const KEY = ['admin-redemptions'] as const

export function useRedemptions() {
  return useQuery({
    queryKey: KEY,
    queryFn: () => api<{ batches: RedemptionBatch[] }>('/redemptions').then((r) => r.batches),
  })
}

export function useGenerateRedemptions() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: GenerateInput) =>
      api<GenerateResult>('/redemptions', { method: 'POST', body: JSON.stringify(input) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
  })
}
