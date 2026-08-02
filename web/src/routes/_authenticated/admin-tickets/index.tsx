import { createFileRoute, redirect } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { AppHeader, Main } from '@/components/layout'
import { TicketListPage } from '@/features/tickets/components/ticket-list-page'

const STAFF_MIN_ROLE = 5 // role>=5 means customer support per backend

function AdminTicketsPage() {
  const { t } = useTranslation()
  return (
    <>
      <AppHeader />
      <Main className='overflow-y-auto'>
        <TicketListPage
          admin
          detailPathPrefix='/admin-tickets'
          title={t('Ticket admin')}
          description={t(
            'Triage, reply to, and resolve tickets across all users'
          )}
          showCreate={false}
        />
      </Main>
    </>
  )
}

export const Route = createFileRoute('/_authenticated/admin-tickets/')({
  beforeLoad: () => {
    const { auth } = useAuthStore.getState()
    if (!auth.user || (auth.user.role ?? 0) < STAFF_MIN_ROLE) {
      throw redirect({ to: '/403' })
    }
  },
  component: AdminTicketsPage,
})
