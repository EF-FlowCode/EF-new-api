import { createFileRoute } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { useIsStaff } from '@/hooks/use-admin'
import { useStatus } from '@/hooks/use-status'
import { Skeleton } from '@/components/ui/skeleton'
import { FeatureUnavailable } from '@/components/feature-unavailable'
import { AppHeader, Main } from '@/components/layout'
import { MyInvitationCodes } from '@/features/invitation-codes/my-codes'

export const Route = createFileRoute('/_authenticated/my-invitation-codes/')({
  component: MyInvitationCodesPage,
})

function MyInvitationCodesPage() {
  const { t } = useTranslation()
  const { status, loading } = useStatus()
  const isStaff = useIsStaff()

  let content = <MyInvitationCodes />
  if (loading && !status) {
    content = <Skeleton className='mx-auto my-10 h-48 w-[min(90%,56rem)]' />
  } else if (
    status?.invitation_code_user_generate_enabled !== true &&
    !isStaff
  ) {
    content = (
      <FeatureUnavailable
        title={t('Invitation code generation is not available')}
        description={t(
          'The administrator has disabled self-service invitation code generation.'
        )}
      />
    )
  }

  return (
    <>
      <AppHeader />
      <Main className='overflow-y-auto'>{content}</Main>
    </>
  )
}
