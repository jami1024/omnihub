import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'
import { papi } from './portalApi'

export interface Announcement {
  id: number
  title: string
  body: string
  kind: 'info' | 'maintenance' | 'pricing' | 'model' | string
  status: 'draft' | 'published' | 'archived' | string
  placement: 'portal_home' | 'login' | 'banner' | string
  priority: number
  starts_at: string | null
  ends_at: string | null
  dismissible: boolean
  created_at: string
  updated_at: string
}

export type AnnouncementInput = Omit<Announcement, 'id' | 'created_at' | 'updated_at'> & { id?: number }

const ADMIN_KEY = ['announcements'] as const

export function useAnnouncements() {
  return useQuery({ queryKey: ADMIN_KEY, queryFn: () => api<{ announcements: Announcement[] }>('/announcements').then((r) => r.announcements) })
}

export function usePortalAnnouncements(placement = 'portal_home') {
  return useQuery({ queryKey: ['portal-announcements', placement], queryFn: () => papi<{ announcements: Announcement[] }>(`/announcements?placement=${encodeURIComponent(placement)}`).then((r) => r.announcements) })
}

export function useSaveAnnouncement() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: AnnouncementInput) => {
      const { id, ...body } = input
      return api<{ id: number } | void>(id ? `/announcements/${id}` : '/announcements', { method: id ? 'PATCH' : 'POST', body: JSON.stringify(body) })
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ADMIN_KEY }),
  })
}

export function useDeleteAnnouncement() {
  const qc = useQueryClient()
  return useMutation({ mutationFn: (id: number) => api<void>(`/announcements/${id}`, { method: 'DELETE' }), onSuccess: () => qc.invalidateQueries({ queryKey: ADMIN_KEY }) })
}
