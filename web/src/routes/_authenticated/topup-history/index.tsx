import z from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { TopupHistory } from '@/features/topup-history'

const topupHistorySearchSchema = z.object({
  page: z.number().int().positive().optional().catch(1),
  pageSize: z.number().int().positive().optional().catch(10),
  filter: z.string().optional().catch(''),
  status: z
    .enum(['success', 'pending', 'failed', 'expired'])
    .optional()
    .catch(undefined),
  startTime: z.number().int().positive().optional().catch(undefined),
  endTime: z.number().int().positive().optional().catch(undefined),
})

export const Route = createFileRoute('/_authenticated/topup-history/')({
  validateSearch: topupHistorySearchSchema,
  component: TopupHistory,
})
