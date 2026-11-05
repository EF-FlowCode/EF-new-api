import type { ReactNode } from 'react'
import type { FootnoteNode, ParsedNode } from 'stream-markdown-parser'
import type { FadeRun } from './response-fade'

export type ResponseProps = {
  children?: ReactNode
  className?: string
  final?: boolean
  /** Distinct stream-markdown-parser cache id when multiple Responses stream concurrently */
  parserId?: string
}

export type AlertKind = 'note' | 'tip' | 'important' | 'warning' | 'caution'

export type AlertConfig = {
  label: string
  className: string
  markerClassName: string
}

export type ParsedResponseContent = {
  bodyNodes: ParsedNode[]
  footnotes: FootnoteNode[]
}

export type RenderChildren = (nodes: ParsedNode[]) => ReactNode

export type BlockRendererOptions = {
  renderChildren: RenderChildren
  fadeRun?: FadeRun
}
