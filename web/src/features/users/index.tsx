import { useTranslation } from 'react-i18next'
import { SectionPageLayout } from '@/components/layout'
import { UserProfileSheet } from '@/components/user-profile-sheet'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { UsersDeleteDialog } from './components/users-delete-dialog'
import { UsersMutateDrawer } from './components/users-mutate-drawer'
import { UsersPrimaryButtons } from './components/users-primary-buttons'
import { UsersProvider, useUsers } from './components/users-provider'
import { UsersTable } from './components/users-table'

function UsersContent() {
  const { t } = useTranslation()
  const { open, setOpen, currentRow, profileUserId, setProfileUserId } =
    useUsers()
  // eflow: 客服 (role>=5 但 <10) 只读，所有 mutating 入口与对话框不挂载
  const userRole = useAuthStore((s) => s.auth.user?.role) ?? 0
  const canMutate = userRole >= ROLE.ADMIN

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Users')}</SectionPageLayout.Title>
        <SectionPageLayout.Description>
          {t('Manage users and their permissions')}
        </SectionPageLayout.Description>
        {canMutate && (
          <SectionPageLayout.Actions>
            <UsersPrimaryButtons />
          </SectionPageLayout.Actions>
        )}
        <SectionPageLayout.Content>
          <UsersTable />
        </SectionPageLayout.Content>
      </SectionPageLayout>

      {canMutate && (
        <>
          <UsersMutateDrawer
            open={open === 'create' || open === 'update'}
            onOpenChange={(isOpen) => !isOpen && setOpen(null)}
            currentRow={open === 'update' ? currentRow || undefined : undefined}
          />
          <UsersDeleteDialog />
        </>
      )}

      {/* eflow: 点用户名时弹用户画像 sheet。复用工单详情页右侧那套 */}
      <UserProfileSheet
        userId={profileUserId}
        onOpenChange={(o) => {
          if (!o) setProfileUserId(null)
        }}
      />
    </>
  )
}

export function Users() {
  return (
    <UsersProvider>
      <UsersContent />
    </UsersProvider>
  )
}
