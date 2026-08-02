import { createFileRoute } from '@tanstack/react-router'
import { AppHeader, Main } from '@/components/layout'
import { TopupHistory } from '@/features/topup-history'

function TopupHistoryPage() {
  return (
    <>
      <AppHeader />
      <Main className='overflow-y-auto'>
        <TopupHistory />
      </Main>
    </>
  )
}

export const Route = createFileRoute('/_authenticated/topup-history/')({
  component: TopupHistoryPage,
})
