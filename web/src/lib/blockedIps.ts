// Data layer for the /admin/api/blocked-ips surface. Rows are keyed by
// IP (the table's primary key), so update/delete address the IP in the
// path. A row with all three limits null is a hard block (403); any
// non-null limit makes it a soft cap (429 when exceeded).
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'

export interface BlockedIP {
  ip: string
  reason: string
  rpm_limit: number | null
  tpm_limit: number | null
  concurrent_limit: number | null
  blocked: boolean
  created_at: string
  created_by: string
}

// BlockedIPInput is the create/update body. On update the IP comes from
// the path, so `ip` is only meaningful on create.
export interface BlockedIPInput {
  ip?: string
  reason: string
  rpm_limit: number | null
  tpm_limit: number | null
  concurrent_limit: number | null
}

const BLOCKED_IPS_KEY = ['blocked-ips'] as const

export function useBlockedIPs() {
  return useQuery({
    queryKey: BLOCKED_IPS_KEY,
    queryFn: () => api<{ blocked_ips: BlockedIP[] }>('/blocked-ips').then((r) => r.blocked_ips),
  })
}

export function useCreateBlockedIP() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: BlockedIPInput) =>
      api<{ ip: string }>('/blocked-ips', { method: 'POST', body: JSON.stringify(input) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: BLOCKED_IPS_KEY }),
  })
}

export function useUpdateBlockedIP() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ ip, input }: { ip: string; input: BlockedIPInput }) =>
      api<void>(`/blocked-ips/${encodeURIComponent(ip)}`, {
        method: 'PATCH',
        body: JSON.stringify(input),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: BLOCKED_IPS_KEY }),
  })
}

export function useDeleteBlockedIP() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (ip: string) =>
      api<void>(`/blocked-ips/${encodeURIComponent(ip)}`, { method: 'DELETE' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: BLOCKED_IPS_KEY }),
  })
}
