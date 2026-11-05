import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { FileWarning } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useSystemConfig } from '@/hooks/use-system-config'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Markdown } from '@/components/ui/markdown'
import { Skeleton } from '@/components/ui/skeleton'
import { HtmlContent } from '@/components/html-content'
import { ProjectAttribution } from '@/components/layout/components/footer'
import type { LegalDocumentResponse } from './types'

// eflow: 法务页（隐私政策 / 用户协议）保持独立全宽，不进侧栏壳——
// 它们多在登录前从注册页跳转访问。顶部与登录页同款品牌链接（可回首页），
// 不铺实底色，透出与登录页一致的主题柔光；左右留白避免手机上卡片贴边。
function PublicLayout({ children }: { children: React.ReactNode }) {
  const { t } = useTranslation()
  const { systemName, logo } = useSystemConfig()
  return (
    <div className='text-foreground min-h-svh px-4 pt-4 sm:px-8 sm:pt-8'>
      <Link
        to='/'
        className='inline-flex items-center gap-2 transition-opacity hover:opacity-80'
      >
        <img
          src={logo}
          alt={t('Logo')}
          className='h-8 w-8 rounded-full object-cover'
        />
        <span className='text-xl font-medium'>{systemName}</span>
      </Link>
      {children}
      <footer className='border-t py-5'>
        <ProjectAttribution currentYear={new Date().getFullYear()} />
      </footer>
    </div>
  )
}

type LegalDocumentProps = {
  title: string
  queryKey: string
  fetchDocument: () => Promise<LegalDocumentResponse>
  emptyMessage: string
}

function isValidUrl(value: string) {
  try {
    const url = new URL(value)
    return url.protocol === 'http:' || url.protocol === 'https:'
  } catch {
    return false
  }
}

function isLikelyHtml(value: string) {
  return /<\/?[a-z][\s\S]*>/i.test(value)
}

export function LegalDocument({
  title,
  queryKey,
  fetchDocument,
  emptyMessage,
}: LegalDocumentProps) {
  const { t } = useTranslation()
  const { data, isLoading } = useQuery({
    queryKey: [queryKey],
    queryFn: fetchDocument,
    staleTime: 10 * 60 * 1000,
  })

  const rawContent = data?.data?.trim() ?? ''
  const hasContent = rawContent.length > 0
  const isUrl = hasContent && isValidUrl(rawContent)
  const isHtml = hasContent && !isUrl && isLikelyHtml(rawContent)
  const success = data?.success ?? false

  if (isLoading) {
    return (
      <PublicLayout>
        <div className='mx-auto flex max-w-4xl flex-col gap-4 py-12'>
          <Skeleton className='h-8 w-[45%]' />
          <Skeleton className='h-4 w-full' />
          <Skeleton className='h-4 w-[90%]' />
          <Skeleton className='h-4 w-[80%]' />
        </div>
      </PublicLayout>
    )
  }

  if (!success || !hasContent) {
    return (
      <PublicLayout>
        <div className='mx-auto max-w-2xl py-12'>
          <Card className='border-dashed'>
            <CardHeader className='flex flex-row items-center gap-4'>
              <div className='bg-muted rounded-full p-2'>
                <FileWarning className='text-muted-foreground h-5 w-5' />
              </div>
              <div className='space-y-1'>
                <CardTitle className='text-lg font-semibold'>{title}</CardTitle>
                <p className='text-muted-foreground text-sm'>
                  {data?.message || emptyMessage}
                </p>
              </div>
            </CardHeader>
          </Card>
        </div>
      </PublicLayout>
    )
  }

  if (isUrl) {
    return (
      <PublicLayout>
        <div className='mx-auto max-w-2xl py-12'>
          <Card>
            <CardHeader>
              <CardTitle>{title}</CardTitle>
            </CardHeader>
            <CardContent className='space-y-4'>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'The administrator configured an external link for this document.'
                )}
              </p>
              <Button asChild>
                <a href={rawContent} target='_blank' rel='noopener noreferrer'>
                  {t('View document')}
                </a>
              </Button>
            </CardContent>
          </Card>
        </div>
      </PublicLayout>
    )
  }

  return (
    <PublicLayout>
      <div className='mx-auto max-w-4xl space-y-6 py-12'>
        <div className='space-y-2'>
          <h1 className='text-3xl font-semibold tracking-tight'>{title}</h1>
        </div>

        {isHtml ? (
          <HtmlContent content={rawContent} />
        ) : (
          <Markdown className='prose-neutral dark:prose-invert max-w-none'>
            {rawContent}
          </Markdown>
        )}
      </div>
    </PublicLayout>
  )
}
