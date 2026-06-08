import { useQuery } from '@tanstack/react-query'
import type { Plan } from './plans'

export interface PublicModelPrice {
  model: string
  input_cost_per_token: number
  output_cost_per_token: number
  cache_creation_input_token_cost: number
  cache_creation_input_token_cost_above_1hr: number
  cache_read_input_token_cost: number
  source: string
  updated_at: string
}

export interface PublicPricing {
  plans: Plan[]
  prices: PublicModelPrice[]
}

export const PER_MILLION_TOKENS = 1_000_000

export function usePublicPricing() {
  return useQuery({
    queryKey: ['public-pricing'],
    queryFn: async () => {
      const res = await fetch('/public/api/pricing')
      if (!res.ok) throw new Error(`pricing failed (${res.status})`)
      return res.json() as Promise<PublicPricing>
    },
  })
}
