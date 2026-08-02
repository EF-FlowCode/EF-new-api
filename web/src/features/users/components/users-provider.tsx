import React, { useState } from 'react'
import useDialogState from '@/hooks/use-dialog'
import { type User, type UsersDialogType } from '../types'

type UsersContextType = {
  open: UsersDialogType | null
  setOpen: (str: UsersDialogType | null) => void
  currentRow: User | null
  setCurrentRow: React.Dispatch<React.SetStateAction<User | null>>
  refreshTrigger: number
  triggerRefresh: () => void
  // eflow: 当前展开的用户画像 sheet 的 user_id（null 表示关闭）
  profileUserId: number | null
  setProfileUserId: (id: number | null) => void
}

const UsersContext = React.createContext<UsersContextType | null>(null)

export function UsersProvider({ children }: { children: React.ReactNode }) {
  const [open, setOpen] = useDialogState<UsersDialogType>(null)
  const [currentRow, setCurrentRow] = useState<User | null>(null)
  const [refreshTrigger, setRefreshTrigger] = useState(0)
  const [profileUserId, setProfileUserId] = useState<number | null>(null)

  const triggerRefresh = () => setRefreshTrigger((prev) => prev + 1)

  return (
    <UsersContext
      value={{
        open,
        setOpen,
        currentRow,
        setCurrentRow,
        refreshTrigger,
        triggerRefresh,
        profileUserId,
        setProfileUserId,
      }}
    >
      {children}
    </UsersContext>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export const useUsers = () => {
  const usersContext = React.useContext(UsersContext)

  if (!usersContext) {
    throw new Error('useUsers has to be used within <UsersContext>')
  }

  return usersContext
}
