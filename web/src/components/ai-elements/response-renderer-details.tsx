import type { ReactNode } from 'react'
import { t } from 'i18next'
import type { HtmlBlockNode, ParsedNode } from 'stream-markdown-parser'
import { hasParsedChildren, isHtmlBlockNode } from './response-node-guards'
import type { BlockRendererOptions } from './response-types'

export function renderDetails(
  node: HtmlBlockNode,
  key: string,
  options: BlockRendererOptions
): ReactNode {
  const children = Array.isArray(node.children) ? node.children : []
  const summaryNode = children.find(isSummaryHtmlNode)
  const contentNodes = children.filter((child) => child !== summaryNode)
  const summary = getDetailsSummary(summaryNode, options)

  return (
    <details
      className='border-border/70 my-4 rounded-lg border px-4 py-3'
      key={key}
    >
      <summary className='text-foreground cursor-pointer text-sm font-semibold'>
        {summary}
      </summary>
      <div className='border-border/70 mt-3 border-l pl-4 [&>*:first-child]:mt-0 [&>*:last-child]:mb-0'>
        {options.renderChildren(contentNodes)}
      </div>
    </details>
  )
}

function isSummaryHtmlNode(node: ParsedNode): node is HtmlBlockNode {
  return isHtmlBlockNode(node) && node.tag === 'summary'
}

function getDetailsSummary(
  node: HtmlBlockNode | undefined,
  options: BlockRendererOptions
): ReactNode {
  if (!node || !hasParsedChildren(node)) {
    return t('Details')
  }

  return options.renderChildren(node.children)
}
