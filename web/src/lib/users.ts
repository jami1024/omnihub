import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'

export interface AdminUser {
  id: number
  username: string
  email: string
  enabled: boolean
  key_count: number
  spend_30d: number
  created_at: string
}

const KEY = ['admin-users'] as const

export function useUsers() {
  return useQuery({ queryKey: KEY, queryFn: () => api<{ users: AdminUser[] }>('/users').then((r) => r.users) })
}
export function useSetUserEnabled() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, enabled }: { id: number; enabled: boolean }) =>
      api<void>(`/users/${id}`, { method: 'PATCH', body: JSON.stringify({ enabled }) }),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
  })
}
export function useDeleteUser() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api<void>(`/users/${id}`, { method: 'DELETE' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: KEY }),
  })
}
