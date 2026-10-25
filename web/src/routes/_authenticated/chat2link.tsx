import { useEffect, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useChatPresets } from '@/features/chat/hooks/use-chat-presets'
import { resolveChatUrl } from '@/features/chat/lib/chat-links'
import { fetchActiveChatKey } from '@/features/chat/hooks/use-active-chat-key'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/_authenticated/chat2link')({
  component: Chat2LinkPage,
})

function Chat2LinkPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { chatPresets, serverAddress } = useChatPresets()

  const firstWebPreset = useMemo(
    () => chatPresets.find((p) => p.type === 'web'),
    [chatPresets]
  )

  const userId = useAuthStore((state) => state.auth.user?.id)
  const { data: activeKey } = useQuery({
    queryKey: ['chat2link-active-key', userId],
    queryFn: () => fetchActiveChatKey().catch(() => null),
    enabled: userId != null,
    staleTime: 5 * 60 * 1000,
  })

  useEffect(() => {
    if (!firstWebPreset) {
      if (chatPresets.length > 0) {
        toast.error(t('No available Web chat links'))
      }
      return
    }

    if (activeKey === undefined) return

    if (!activeKey) {
      toast.error(t('No enabled tokens available'))
      navigate({ to: '/keys' })
      return
    }

    const url = resolveChatUrl({
      template: firstWebPreset.url,
      apiKey: activeKey,
      serverAddress,
    })

    if (url) {
      window.location.href = url
    }
  }, [
    firstWebPreset,
    activeKey,
    serverAddress,
    chatPresets.length,
    navigate,
    t,
  ])

  return (
    <div className='flex h-full flex-col items-center justify-center gap-3'>
      <Loader2 className='text-muted-foreground h-8 w-8 animate-spin' />
      <p className='text-muted-foreground text-sm'>
        {t('Redirecting to chat page...')}
      </p>
    </div>
  )
}
