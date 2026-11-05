import { useTranslation } from 'react-i18next'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SectionPageLayout } from '@/components/layout'
import { DistributionTab } from './components/distribution-tab'
import { EnforcementTab } from './components/enforcement-tab'
import { ModerationTab } from './components/moderation-tab'

export function RiskCenter() {
  const { t } = useTranslation()

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Risk Center')}</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t('Distribution detection, content moderation, and enforcement')}
      </SectionPageLayout.Description>
      <SectionPageLayout.Content>
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
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
