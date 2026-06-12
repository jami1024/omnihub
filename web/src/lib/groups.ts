// Data layer for the /admin/api/groups surface: provider groups bundle
// accounts under a shared cost multiplier.
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'

export interface ProviderGroup {
  id: number
  name: string
  cost_multiplier: number
  description: string
  routing_policy: string
  account_count: number
}

export interface GroupInput {
  name: string
  cost_multiplier: number
  description: string
  routing_policy: string
}

const GROUPS_KEY = ['groups'] as const

export function useGroups() {
  return useQuery({
    queryKey: GROUPS_KEY,
    queryFn: () => api<{ groups: ProviderGroup[] }>('/groups').then((r) => r.groups),
  })
}

export function useCreateGroup() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: GroupInput) =>
      api<ProviderGroup>('/groups', { method: 'POST', body: JSON.stringify(input) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: GROUPS_KEY }),
  })
}

export function useUpdateGroup() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: GroupInput }) =>
      api<ProviderGroup>(`/groups/${id}`, { method: 'PATCH', body: JSON.stringify(input) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: GROUPS_KEY }),
  })
}

export function useDeleteGroup() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api<void>(`/groups/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: GROUPS_KEY })
      qc.invalidateQueries({ queryKey: ['accounts'] })
    },
  })
}
