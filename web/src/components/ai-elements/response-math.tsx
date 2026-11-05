import { memo, useMemo } from 'react'
import katex from 'katex'
import 'katex/dist/katex.min.css'
import { cn } from '@/lib/utils'
import { isLikelyCurrencyMath } from './response-math-utils'

type MathProps = {
  content: string
  /** Delimiter the parser saw: `$`, `$$`, `\(\)`, `\[\]`. */
  markup?: string
  /** Formula still streaming in; show the source until it is complete. */
  loading?: boolean
}

function renderKatex(content: string, displayMode: boolean): string | null {
  try {
    return katex.renderToString(content, {
      displayMode,
      // Invalid input is shown in red instead of throwing.
      throwOnError: false,
      strict: 'ignore',
      // trust stays false: \href, \htmlClass etc. are not rendered as HTML.
      trust: false,
      output: 'htmlAndMathml',
    })
  } catch {
    return null
  }
}

export const MathBlock = memo(function MathBlock({
  content,
  loading = false,
}: MathProps) {
  const html = useMemo(
    () => (loading ? null : renderKatex(content, true)),
    [content, loading]
  )

  if (html === null) {
    return (
      <pre className='border-border bg-muted/40 my-4 overflow-x-auto rounded-lg border p-4 font-mono text-sm'>
        {content}
      </pre>
    )
  }

  return (
    <div
      className='my-4 overflow-x-auto overflow-y-hidden py-1 [&_.katex-display]:my-0'
      // KaTeX escapes input and trust is off, so the markup is safe.
      dangerouslySetInnerHTML={{ __html: html }}
    />
  )
})

export const MathInline = memo(function MathInline({
  content,
  markup,
  loading = false,
}: MathProps) {
  const isCurrency = isLikelyCurrencyMath(content, markup)
  const html = useMemo(
    () => (loading || isCurrency ? null : renderKatex(content, false)),
    [content, loading, isCurrency]
  )

  if (isCurrency) {
    return (
      <span>
        {markup}
        {content}
        {markup}
      </span>
    )
  }

  if (html === null) {
    return (
      <code
        className={cn(
          'bg-muted/70 text-foreground rounded px-1 py-0.5 font-mono text-[0.9em]'
        )}
      >
        {content}
      </code>
    )
  }

  return (
    <span
      className='[&_.katex]:text-[1.05em]'
      dangerouslySetInnerHTML={{ __html: html }}
    />
  )
})
