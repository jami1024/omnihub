import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'

export interface PortalSettings {
  signup_enabled: boolean
  key_daily_usd_default: number | null
  key_daily_usd_max: number | null
  key_rpm_max: number | null
}

const KEY = ['portal-settings'] as const

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
