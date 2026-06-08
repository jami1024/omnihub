import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'

export interface PortalSettings {
  signup_enabled: boolean
  key_daily_usd_default: number | null
  key_daily_usd_max: number | null
  key_rpm_max: number | null
  signup_bonus_usd: number
}

export interface GatewaySettings {
  health_probe_enabled: boolean
  health_probe_interval_ms: number
  health_probe_concurrency: number
  health_probe_red_threshold: number
  health_probe_green_threshold: number
  health_probe_timeout_ms: number
  health_probe_slow_threshold_ms: number
  circuit_failure_threshold: number
  circuit_open_duration_ms: number
  circuit_half_open_success: number
  failover_max_attempts: number
}

const KEY = ['portal-settings'] as const
const GATEWAY_KEY = ['gateway-settings'] as const

export function useSettings() {
  return useQuery({ queryKey: KEY, queryFn: () => api<PortalSettings>('/settings') })
}
export function useUpdateSettings() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (s: PortalSettings) => api<PortalSettings>('/settings', { method: 'PUT', body: JSON.stringify(s) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
  })
}

export function useGatewaySettings() {
  return useQuery({ queryKey: GATEWAY_KEY, queryFn: () => api<GatewaySettings>('/gateway-settings') })
}

export function useUpdateGatewaySettings() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (s: GatewaySettings) => api<GatewaySettings>('/gateway-settings', { method: 'PUT', body: JSON.stringify(s) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: GATEWAY_KEY }),
  })
}
