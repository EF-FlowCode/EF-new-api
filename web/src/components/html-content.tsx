import { useMemo } from 'react'
import DOMPurify from 'dompurify'
import { cn } from '@/lib/utils'

interface HtmlContentProps {
  content: string
  className?: string
  prose?: boolean
}

export function HtmlContent({
  content,
  className,
  prose = true,
}: HtmlContentProps) {
  const sanitizedContent = useMemo(() => DOMPurify.sanitize(content), [content])

  return (
    <div
      className={cn(
        prose && 'prose prose-neutral dark:prose-invert max-w-none',
        className
      )}
      dangerouslySetInnerHTML={{ __html: sanitizedContent }}
    />
  )
}
