// Data layer for the /admin/api/alert-channels surface. Rows are keyed by
// a numeric id. The url is a write-only secret: the list never returns it,
// and an update with a blank url leaves the stored value untouched.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'

export type AlertChannelType = 'webhook' | 'feishu' | 'dingtalk'

export interface AlertChannel {
  id: number
  type: AlertChannelType
  name: string
  enabled: boolean
  created_at: string
  created_by: string
}

// AlertChannelInput is the create/update body. On update `url` may be
// blank to keep the existing secret.
export interface AlertChannelInput {
  type: AlertChannelType
  name: string
  url: string
  enabled: boolean
}

const ALERT_CHANNELS_KEY = ['alert-channels'] as const

export function useAlertChannels() {
  return useQuery({
    queryKey: ALERT_CHANNELS_KEY,
    queryFn: () =>
      api<{ alert_channels: AlertChannel[] }>('/alert-channels').then((r) => r.alert_channels),
  })
}

export function useCreateAlertChannel() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: AlertChannelInput) =>
      api<{ id: number }>('/alert-channels', { method: 'POST', body: JSON.stringify(input) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ALERT_CHANNELS_KEY }),
  })
}

export function useUpdateAlertChannel() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: AlertChannelInput }) =>
      api<void>(`/alert-channels/${id}`, { method: 'PATCH', body: JSON.stringify(input) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ALERT_CHANNELS_KEY }),
  })
}

export function useDeleteAlertChannel() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api<void>(`/alert-channels/${id}`, { method: 'DELETE' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ALERT_CHANNELS_KEY }),
  })
}

// useTestAlertChannel delivers a synthetic alert through one channel. Not
// a cache mutation — it returns the delivery result for the caller to show.
export function useTestAlertChannel() {
  return useMutation({
    mutationFn: (id: number) =>
      api<{ delivered: boolean }>(`/alert-channels/${id}/test`, { method: 'POST' }),
  })
}
