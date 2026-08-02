import { useEffect, useState } from 'react'
import { ApiKeysDeleteDialog } from './api-keys-delete-dialog'
import { ApiKeysMutateDrawer } from './api-keys-mutate-drawer'
import { useApiKeys } from './api-keys-provider'
import { CCSwitchDialog } from './dialogs/cc-switch-dialog'

export function ApiKeysDialogs() {
  const { open, setOpen, currentRow, resolvedKey } = useApiKeys()
  const [lastMutateSide, setLastMutateSide] = useState<'left' | 'right'>(
    'right'
  )
  const mutateSide =
    open === 'create' ? 'left' : open === 'update' ? 'right' : lastMutateSide

  useEffect(() => {
    // 记录最后一次 drawer 滑入方向，关闭动画用——合法的 prop→state 同步
    if (open === 'create') {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setLastMutateSide('left')
    } else if (open === 'update') {
      setLastMutateSide('right')
    }
  }, [open])

  return (
    <>
      <ApiKeysMutateDrawer
        open={open === 'create' || open === 'update'}
        onOpenChange={(isOpen) => !isOpen && setOpen(null)}
        currentRow={open === 'update' ? currentRow || undefined : undefined}
        side={mutateSide}
      />
      <ApiKeysDeleteDialog />
      <CCSwitchDialog
        open={open === 'cc-switch'}
        onOpenChange={(isOpen) => !isOpen && setOpen(null)}
        tokenKey={resolvedKey}
      />
    </>
  )
}
