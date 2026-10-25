import { useId, useState } from 'react'
import { isAxiosError } from 'axios'
import { useQuery } from '@tanstack/react-query'
import { Check, ChevronsUpDown, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { getUserGroups } from '@/lib/api'
import { cn } from '@/lib/utils'
import { useIsAdmin } from '@/hooks/use-admin'
import { Button } from '@/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { getGroups } from '@/features/users/api'

interface LogGroupFilterProps {
  value: string
  onValueChange: (value: string) => void
  logGroups: string[]
  sensitiveVisible: boolean
}

export function LogGroupFilter({
  value,
  onValueChange,
  logGroups,
  sensitiveVisible,
}: LogGroupFilterProps) {
  const { t } = useTranslation()
  const listId = useId()
  const [open, setOpen] = useState(false)
  const isAdmin = useIsAdmin()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const { data, isPending, isError, refetch } = useQuery({
    queryKey: ['usage-log-groups', userId, isAdmin],
    queryFn: async () => {
      try {
        if (isAdmin) {
          const result = await getGroups()
          if (!result.success) throw new Error(result.message)
          return result.data || []
        }
        const result = await getUserGroups()
        if (!result.success) throw new Error(result.message)
        return Object.keys(result.data || {})
      } catch (error) {
        // Keep an optional group lookup failure out of the global 500 redirect.
        // The API layer already reports it; authentication errors still propagate.
        if (isAxiosError(error) && error.response?.status === 500) {
          throw new Error(error.message, { cause: error })
        }
        throw error
      }
    },
    enabled: !!userId,
    staleTime: 60_000,
    retry: false,
  })

  // Keep historical values from the current results and bookmarked filters selectable.
  const groups = [...new Set([...(data || []), ...logGroups, value])]
    .filter(Boolean)
    .sort((a, b) => a.localeCompare(b))
  const sensitiveClass = sensitiveVisible
    ? undefined
    : '[-webkit-text-security:disc]'
  const selectGroup = (group: string) => {
    onValueChange(group === value ? '' : group)
    setOpen(false)
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          type='button'
          variant='outline'
          role='combobox'
          aria-label={t('Group')}
          aria-expanded={open}
          aria-controls={open ? listId : undefined}
          className='h-9 w-full min-w-0 justify-between'
        >
          <span className={cn('truncate', value && sensitiveClass)}>
            {value || t('All Groups')}
          </span>
          {isPending ? (
            <Loader2 data-icon='inline-end' className='shrink-0 animate-spin' />
          ) : (
            <ChevronsUpDown data-icon='inline-end' className='shrink-0' />
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent
        align='start'
        className='w-[max(200px,var(--radix-popover-trigger-width))] max-w-[calc(100vw-2rem)] p-0'
      >
        <Command>
          <CommandInput
            aria-label={t('Group')}
            placeholder={t('Search...')}
            className={sensitiveClass}
          />
          <CommandList id={listId}>
            <CommandEmpty>{t('No results found.')}</CommandEmpty>
            <CommandGroup>
              <CommandItem
                value='all-groups'
                keywords={[t('All Groups')]}
                onSelect={() => selectGroup('')}
              >
                <Check className={cn('shrink-0', value && 'invisible')} />
                <span className='truncate'>{t('All Groups')}</span>
              </CommandItem>
            </CommandGroup>
            <CommandGroup>
              {groups.map((group) => (
                <CommandItem
                  key={group}
                  value={`group:${group}`}
                  keywords={[group]}
                  onSelect={() => selectGroup(group)}
                >
                  <Check
                    className={cn('shrink-0', value !== group && 'invisible')}
                  />
                  <span className={cn('min-w-0 truncate', sensitiveClass)}>
                    {group}
                  </span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
        {isError && (
          <Button
            type='button'
            variant='ghost'
            className='w-full'
            onClick={() => void refetch()}
          >
            {t('Retry')}
          </Button>
        )}
      </PopoverContent>
    </Popover>
  )
}
