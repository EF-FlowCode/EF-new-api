import { createFileRoute, redirect, useParams } from '@tanstack/react-router'
import { useAuthStore } from '@/stores/auth-store'
import { AppHeader, Main } from '@/components/layout'
import { TicketDetailPage } from '@/features/tickets/components/ticket-detail-page'

const STAFF_MIN_ROLE = 5

function AdminTicketDetailRoute() {
  const { id } = useParams({ from: '/_authenticated/admin-tickets/$id' })
  const ticketId = Number(id)
  return (
    <>
      <AppHeader />
      <Main className='overflow-y-auto'>
        <TicketDetailPage
          ticketId={Number.isFinite(ticketId) ? ticketId : 0}
          admin
          backTo='/admin-tickets'
        />
      </Main>
    </>
  )
}

export const Route = createFileRoute('/_authenticated/admin-tickets/$id')({
  beforeLoad: () => {
    const { auth } = useAuthStore.getState()
    if (!auth.user || (auth.user.role ?? 0) < STAFF_MIN_ROLE) {
      throw redirect({ to: '/403' })
    }
  },
  component: AdminTicketDetailRoute,
})
