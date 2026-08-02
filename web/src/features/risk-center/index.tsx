import { useTranslation } from 'react-i18next'
import { ShieldAlert } from 'lucide-react'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { DistributionTab } from './components/distribution-tab'
import { ModerationTab } from './components/moderation-tab'
import { EnforcementTab } from './components/enforcement-tab'

export function RiskCenter() {
  const { t } = useTranslation()

  return (
    <div className='px-4 py-6 sm:px-8'>
      <div className='mx-auto w-full max-w-[1440px] space-y-5'>
        <div className='flex items-center gap-3'>
          <span className='bg-primary/10 text-primary flex h-10 w-10 items-center justify-center rounded-xl'>
            <ShieldAlert className='h-5 w-5' />
          </span>
          <div>
            <h1 className='text-xl font-semibold tracking-tight'>
              {t('Risk Center')}
            </h1>
            <p className='text-muted-foreground text-sm'>
              {t('Distribution detection, content moderation, and enforcement')}
            </p>
          </div>
        </div>

        <Tabs defaultValue='distribution' className='space-y-4'>
          <TabsList>
            <TabsTrigger value='distribution'>
              {t('Distribution detection')}
            </TabsTrigger>
            <TabsTrigger value='moderation'>
              {t('Content moderation')}
            </TabsTrigger>
            <TabsTrigger value='enforcement'>{t('Enforcement')}</TabsTrigger>
          </TabsList>
          <TabsContent value='distribution'>
            <DistributionTab />
          </TabsContent>
          <TabsContent value='moderation'>
            <ModerationTab />
          </TabsContent>
          <TabsContent value='enforcement'>
            <EnforcementTab />
          </TabsContent>
        </Tabs>
      </div>
    </div>
  )
}
