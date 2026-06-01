// Data layer for /admin/api/prices. The API speaks USD-per-token (the
// canonical LiteLLM shape); this layer exposes per-MILLION-token helpers
// because that's what operators read on vendor pricing pages.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'

export interface ModelPrice {
  id: number
  model: string
  input_cost_per_token: number
  output_cost_per_token: number
  cache_creation_input_token_cost: number
  cache_creation_input_token_cost_above_1hr: number
  cache_read_input_token_cost: number
  source: 'litellm' | 'manual' | string
  updated_at: string
}

// PriceInput mirrors the API body (per-token). `model` only matters on
// create.
export interface PriceInput {
  model?: string
  input_cost_per_token: number
  output_cost_per_token: number
  cache_creation_input_token_cost: number
  cache_creation_input_token_cost_above_1hr: number
  cache_read_input_token_cost: number
}

export interface SyncResult {
  added: number
  updated: number
  skipped: number
}

// Per-token ↔ per-million-token. 1 token rate × 1e6 = per-MTok rate.
export const PER_MILLION = 1_000_000
export const toPerMillion = (perToken: number) => perToken * PER_MILLION
export const toPerToken = (perMillion: number) => perMillion / PER_MILLION

const PRICES_KEY = ['prices'] as const

export function usePrices() {
  return useQuery({
    queryKey: PRICES_KEY,
    queryFn: () => api<{ prices: ModelPrice[] }>('/prices').then((r) => r.prices),
  })
}

export function useCreatePrice() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: PriceInput) =>
      api<ModelPrice>('/prices', { method: 'POST', body: JSON.stringify(input) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: PRICES_KEY }),
  })
}

export function useUpdatePrice() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: PriceInput }) =>
      api<ModelPrice>(`/prices/${id}`, { method: 'PATCH', body: JSON.stringify(input) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: PRICES_KEY }),
  })
}

export function useDeletePrice() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api<void>(`/prices/${id}`, { method: 'DELETE' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: PRICES_KEY }),
  })
}

export function useSyncPrices() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api<SyncResult>('/prices/sync', { method: 'POST' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: PRICES_KEY }),
  })
}
