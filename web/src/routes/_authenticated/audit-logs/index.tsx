import z from 'zod'
import { createFileRoute } from '@tanstack/react-router'
import { AppHeader, Main } from '@/components/layout'
import { AuditLogs } from '@/features/audit-logs'

const auditLogsSearchSchema = z.object({
  page: z.number().int().positive().optional().catch(1),
  pageSize: z.number().int().positive().optional().catch(20),
  scope: z.enum(['self']).optional().catch(undefined),
  category: z
    .enum(['login', 'security', 'operation', 'access_token'])
    .optional()
    .catch(undefined),
  result: z.enum(['success', 'failed']).optional().catch(undefined),
  username: z.string().optional().catch(undefined),
  startTime: z.number().int().positive().optional().catch(undefined),
  endTime: z.number().int().positive().optional().catch(undefined),
})

function AuditLogsPage() {
  return (
    <>
      <AppHeader />
      <Main className='overflow-y-auto'>
        <AuditLogs />
      </Main>
    </>
  )
}

export const Route = createFileRoute('/_authenticated/audit-logs/')({
  validateSearch: auditLogsSearchSchema,
  component: AuditLogsPage,
})
