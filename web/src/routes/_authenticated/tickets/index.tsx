import { createFileRoute } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { AppHeader, Main } from '@/components/layout'
import { TicketListPage } from '@/features/tickets/components/ticket-list-page'

function UserTicketsPage() {
  const { t } = useTranslation()
  return (
    <>
      <AppHeader />
      <Main className='overflow-y-auto'>
        <TicketListPage
          admin={false}
          detailPathPrefix='/tickets'
          title={t('My tickets')}
          description={t(
            'Open new tickets, track support replies, and continue past conversations'
          )}
        />
      </Main>
    </>
  )
}

export const Route = createFileRoute('/_authenticated/tickets/')({
  component: UserTicketsPage,
})
