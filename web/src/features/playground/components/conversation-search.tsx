import { useMemo, useState } from 'react'
import { ChevronDownIcon, ChevronRightIcon, ChevronUpIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { getCurrentVersion, getVersionText } from '../lib'
import type { Message as MessageType } from '../types'

interface ConversationSearchProps {
  messages: MessageType[]
}

const FLASH_CLASSES = ['ring-2', 'ring-amber-500', 'rounded-md']

export function ConversationSearch({ messages }: ConversationSearchProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [pos, setPos] = useState(0)

  // 命中的消息 key 列表（按对话顺序）
  const matches = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return [] as string[]
    return messages
      .filter((m) =>
        getVersionText(getCurrentVersion(m).content)
          .toLowerCase()
          .includes(q)
      )
      .map((m) => m.key)
  }, [messages, query])

  const go = (nextPos: number) => {
    if (matches.length === 0) return
    const p = (nextPos + matches.length) % matches.length
    setPos(p)
    const el = document.querySelector<HTMLElement>(
      `[data-msg-key="${matches[p]}"]`
    )
    if (!el) return
    el.scrollIntoView({ block: 'center', behavior: 'smooth' })
    el.classList.add(...FLASH_CLASSES)
    window.setTimeout(() => el.classList.remove(...FLASH_CLASSES), 1400)
  }

  return (
    <div className='border-b'>
      <button
        type='button'
        className='text-muted-foreground hover:text-foreground flex w-full items-center gap-1 px-4 py-1.5 text-xs'
        onClick={() => setOpen((v) => !v)}
      >
        {open ? (
          <ChevronDownIcon className='size-3' />
        ) : (
          <ChevronRightIcon className='size-3' />
        )}
        {t('Search in conversation')}
      </button>
      {open && (
        <div className='flex items-center gap-2 px-4 pb-2'>
          <Input
            value={query}
            onChange={(e) => {
              setQuery(e.target.value)
              setPos(0)
            }}
            onKeyDown={(e) => {
              if (e.key === 'Enter') go(e.shiftKey ? pos - 1 : pos + 1)
            }}
            placeholder={t('Search')}
            className='h-7 text-sm'
          />
          <span className='text-muted-foreground w-14 shrink-0 text-center text-xs tabular-nums'>
            {matches.length ? `${pos + 1}/${matches.length}` : '0/0'}
          </span>
          <Button
            size='sm'
            variant='ghost'
            className='size-7 p-0'
            disabled={matches.length === 0}
            onClick={() => go(pos - 1)}
            aria-label={t('Previous')}
          >
            <ChevronUpIcon className='size-4' />
          </Button>
          <Button
            size='sm'
            variant='ghost'
            className='size-7 p-0'
            disabled={matches.length === 0}
            onClick={() => go(pos + 1)}
            aria-label={t('Next')}
          >
            <ChevronDownIcon className='size-4' />
          </Button>
        </div>
      )}
    </div>
  )
}
