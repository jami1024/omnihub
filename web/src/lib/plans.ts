import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'
import { papi } from './portalApi'

export interface Plan {
  id: number
  name: string
  description: string
  price_usd: number
  included_credit_usd: number
  valid_days: number | null
  rpm_limit: number | null
  daily_usd_limit: number | null
  allowed_models: string[]
  price_ratio: number
  allow_payg_overage: boolean
  enabled: boolean
  sort_order: number
  created_at: string
  updated_at: string
}

export interface UserPlanGrant {
  id: number
  user_id: number
  plan_id: number | null
  plan_name_snapshot: string
  starts_at: string
  expires_at: string | null
  credit_granted_usd: number
  credit_remaining_usd: number
  price_ratio_snapshot: number
  allow_payg_overage_snapshot: boolean
  status: string
  created_at: string
  updated_at: string
}

export type PlanInput = Omit<Plan, 'id' | 'created_at' | 'updated_at'> & { id?: number }

const ADMIN_KEY = ['plans'] as const
const PORTAL_KEY = ['portal-plans'] as const
const CURRENT_KEY = ['portal-current-plan'] as const

export function usePlans() {
  return useQuery({ queryKey: ADMIN_KEY, queryFn: () => api<{ plans: Plan[] }>('/plans').then((r) => r.plans) })
}
export function useSavePlan() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: PlanInput) => {
      const { id, ...body } = input
      return api<{ id: number } | void>(id ? `/plans/${id}` : '/plans', { method: id ? 'PATCH' : 'POST', body: JSON.stringify(body) })
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ADMIN_KEY }),
  })
}
export function useGrantPlanToUser() {
  return useMutation({ mutationFn: ({ userId, planId }: { userId: number; planId: number }) => api<{ id: number }>(`/users/${userId}/plan-grants`, { method: 'POST', body: JSON.stringify({ plan_id: planId }) }) })
}
export function usePortalPlans() {
  return useQuery({ queryKey: PORTAL_KEY, queryFn: () => papi<{ plans: Plan[] }>('/plans').then((r) => r.plans) })
}
export function useCurrentPlan() {
  return useQuery({ queryKey: CURRENT_KEY, queryFn: () => papi<{ grant: UserPlanGrant | null }>('/me/plan') })
}
export function useClaimPlan() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (planId: number) => papi<{ id: number }>(`/plans/${planId}/claim`, { method: 'POST' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: CURRENT_KEY })
      qc.invalidateQueries({ queryKey: PORTAL_KEY })
    },
  })
}
