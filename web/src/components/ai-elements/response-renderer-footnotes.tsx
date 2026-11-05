import type { ReactNode } from 'react'
import { t } from 'i18next'
import type { FootnoteNode } from 'stream-markdown-parser'
import type { BlockRendererOptions } from './response-types'

export function renderFootnotes(
  footnotes: FootnoteNode[],
  options: BlockRendererOptions
): ReactNode {
  if (footnotes.length === 0) {
    return null
  }

  return (
    <section className='border-border/70 text-muted-foreground mt-6 border-t pt-3 text-sm'>
      <ol className='list-decimal space-y-2 pl-5'>
        {footnotes.map((footnote) => (
          <li id={`footnote-${footnote.id}`} key={footnote.id}>
            <div className='inline [&>*:first-child]:mt-0 [&>*:last-child]:mb-0'>
              {options.renderChildren(footnote.children)}
            </div>
            <a
              aria-label={t('Back to footnote {{id}} reference', {
                id: footnote.id,
              })}
              className='text-primary ml-2 underline-offset-2 hover:underline'
              href={`#footnote-ref-${footnote.id}`}
            >
              {t('Back')}
            </a>
          </li>
        ))}
      </ol>
    </section>
  )
}
