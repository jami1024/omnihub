// Data layer for the circuit-breaker admin surface: live per-account
// state (/circuit), the transition feed (/circuit/events), and the reset
// action (/accounts/:id/reset-breaker).
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'

export interface CircuitStatus {
  account_id: number
  account_name: string
  enabled: boolean
  state: 'closed' | 'open' | 'half-open' | string
  failure_count: number
  last_failure: string | null
  open_until: string | null
  half_open_success: number
}

export interface CircuitReport {
  available: boolean
  accounts: CircuitStatus[]
}

export interface HealthEvent {
  created_at: string
  account_id: number
  account_name: string
  from_state: string
  to_state: string
  failure_count: number
  reason: string | null
}

const CIRCUIT_KEY = ['circuit'] as const

export function useCircuit() {
  return useQuery({
    queryKey: CIRCUIT_KEY,
    queryFn: () => api<CircuitReport>('/circuit'),
    // Breaker state changes on its own as the gateway runs; keep it fresh.
    refetchInterval: 10_000,
  })
}

export function useCircuitEvents(limit = 50) {
  return useQuery({
    queryKey: ['circuit-events', limit],
    queryFn: () => api<{ events: HealthEvent[] }>(`/circuit/events?limit=${limit}`).then((r) => r.events),
    refetchInterval: 10_000,
  })
}

export function useResetBreaker() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (accountId: number) =>
      api<void>(`/accounts/${accountId}/reset-breaker`, { method: 'POST' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: CIRCUIT_KEY }),
  })
}
