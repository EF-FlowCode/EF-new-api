import z from 'zod'
import { createFileRoute, redirect } from '@tanstack/react-router'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { RiskCenter } from '@/features/risk-center'

const pageParam = z.number().int().positive().optional().catch(1)
const pageSizeParam = z.number().int().positive().optional().catch(10)

const riskCenterSearchSchema = z.object({
  subjectsPage: pageParam,
  subjectsPageSize: pageSizeParam,
  distributionIncidentsPage: pageParam,
  distributionIncidentsPageSize: pageSizeParam,
  enforcementCountersPage: pageParam,
  enforcementCountersPageSize: pageSizeParam,
  enforcementIncidentsPage: pageParam,
  enforcementIncidentsPageSize: pageSizeParam,
  moderationIncidentsPage: pageParam,
  moderationIncidentsPageSize: pageSizeParam,
})

export const Route = createFileRoute('/_authenticated/risk-center/')({
  beforeLoad: () => {
    const { auth } = useAuthStore.getState()
    if (!auth.user || auth.user.role < ROLE.ADMIN) {
      throw redirect({ to: '/403' })
    }
  },
  validateSearch: riskCenterSearchSchema,
  component: RiskCenter,
})
