import { createFileRoute, useParams } from '@tanstack/react-router'
import { AppHeader, Main } from '@/components/layout'
import { TicketDetailPage } from '@/features/tickets/components/ticket-detail-page'

function UserTicketDetailRoute() {
  const { id } = useParams({ from: '/_authenticated/tickets/$id' })
  const ticketId = Number(id)
  return (
    <>
      <AppHeader />
      <Main className='overflow-y-auto'>
        <TicketDetailPage
          ticketId={Number.isFinite(ticketId) ? ticketId : 0}
          admin={false}
          backTo='/tickets'
        />
      </Main>
    </>
  )
}

export const Route = createFileRoute('/_authenticated/tickets/$id')({
  component: UserTicketDetailRoute,
})
