import { memo, useEffect, useState } from 'react'
import {
  AlertTriangleIcon,
  CheckIcon,
  CodeIcon,
  CopyIcon,
  DownloadIcon,
  Maximize2Icon,
  MinusIcon,
  PlusIcon,
  RotateCcwIcon,
  WorkflowIcon,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { useTheme } from '@/context/theme-provider'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { CodeBlock } from '@/components/ai-elements/code-block'

type MermaidApi = (typeof import('mermaid'))['default']

const SVG_CACHE_LIMIT = 50
const ZOOM_STEP = 0.25
const MIN_ZOOM = 0.5
const MAX_ZOOM = 3

let mermaidPromise: Promise<MermaidApi> | null = null
let renderQueue: Promise<unknown> = Promise.resolve()
let renderCount = 0
const svgCache = new Map<string, string>()

// mermaid 体积较大，第一次遇到图表时才加载
function loadMermaid(): Promise<MermaidApi> {
  mermaidPromise ??= import('mermaid').then((module) => module.default)
  return mermaidPromise
}

/**
 * Render one diagram. mermaid keeps global state (initialize, temporary DOM
 * nodes), so renders run one at a time.
 */
function renderMermaid(code: string, dark: boolean): Promise<string> {
  const cacheKey = `${dark ? 'dark' : 'light'}\n${code}`
  const cached = svgCache.get(cacheKey)
  if (cached) return Promise.resolve(cached)

  const run = renderQueue.then(async () => {
    const mermaid = await loadMermaid()
    mermaid.initialize({
      startOnLoad: false,
      // strict: 图里的 HTML 与点击事件不执行，SVG 经 DOMPurify 清洗
      securityLevel: 'strict',
      suppressErrorRendering: true,
      theme: dark ? 'dark' : 'default',
      fontFamily: 'inherit',
    })
    renderCount += 1
    const { svg } = await mermaid.render(`mermaid-svg-${renderCount}`, code)
    svgCache.set(cacheKey, svg)
    if (svgCache.size > SVG_CACHE_LIMIT) {
      const oldest = svgCache.keys().next().value
      if (oldest !== undefined) svgCache.delete(oldest)
    }
    return svg
  })
  renderQueue = run.catch(() => undefined)
  return run
}

function downloadSvg(svg: string) {
  const blob = new Blob([svg], { type: 'image/svg+xml' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = 'diagram.svg'
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

type RenderState =
  | { status: 'pending' }
  | { status: 'ready'; svg: string }
  | { status: 'error'; message: string }

function useMermaidSvg(code: string, enabled: boolean): RenderState {
  const { resolvedTheme } = useTheme()
  const dark = resolvedTheme === 'midnight'
  const [state, setState] = useState<RenderState>({ status: 'pending' })

  useEffect(() => {
    if (!enabled || !code.trim()) return
    let cancelled = false
    renderMermaid(code, dark).then(
      (svg) => {
        if (!cancelled) setState({ status: 'ready', svg })
      },
      (error: unknown) => {
        if (!cancelled) {
          setState({
            status: 'error',
            message: error instanceof Error ? error.message : String(error),
          })
        }
      }
    )
    return () => {
      cancelled = true
    }
  }, [code, dark, enabled])

  return enabled ? state : { status: 'pending' }
}

type ToolbarButtonProps = {
  icon: typeof CopyIcon
  label: string
  onClick: () => void
  disabled?: boolean
}

function ToolbarButton({
  icon: Icon,
  label,
  onClick,
  disabled,
}: ToolbarButtonProps) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          aria-label={label}
          className='size-8'
          disabled={disabled}
          onClick={onClick}
          size='icon-sm'
          type='button'
          variant='ghost'
        >
          <Icon className='size-4' />
        </Button>
      </TooltipTrigger>
      <TooltipContent>
        <p>{label}</p>
      </TooltipContent>
    </Tooltip>
  )
}

type MermaidBlockProps = {
  code: string
  /** Code fence still streaming in; keep showing the source. */
  loading?: boolean
}

export const MermaidBlock = memo(function MermaidBlock({
  code,
  loading = false,
}: MermaidBlockProps) {
  const { t } = useTranslation()
  const [showSource, setShowSource] = useState(false)
  const [fullscreen, setFullscreen] = useState(false)
  const [zoom, setZoom] = useState(1)
  const { copiedText, copyToClipboard } = useCopyToClipboard({ notify: false })
  const state = useMermaidSvg(code, !loading)
  const svg = state.status === 'ready' ? state.svg : null
  const showDiagram = !showSource && svg !== null

  const source = (
    <CodeBlock
      className='my-0 rounded-none border-0'
      code={code}
      collapsedLines={14}
      defaultCollapsed={code.split('\n').length > 14}
      language='mermaid'
      maxExpandedLines={44}
      showLineNumbers
    />
  )

  return (
    <div className='border-border bg-background my-4 overflow-hidden rounded-lg border'>
      <div className='border-border/70 bg-muted/30 flex items-center justify-between gap-2 border-b px-3 py-1'>
        <span className='text-muted-foreground flex items-center gap-1.5 text-xs font-medium'>
          <WorkflowIcon className='size-3.5' />
          Mermaid
          {loading && <span className='font-normal'>· {t('Generating…')}</span>}
        </span>
        <div className='flex items-center'>
          <ToolbarButton
            disabled={svg === null}
            icon={showSource ? WorkflowIcon : CodeIcon}
            label={showSource ? t('Show diagram') : t('Show source')}
            onClick={() => setShowSource((value) => !value)}
          />
          <ToolbarButton
            icon={copiedText === code ? CheckIcon : CopyIcon}
            label={copiedText === code ? t('Copied!') : t('Copy code')}
            onClick={() => copyToClipboard(code)}
          />
          <ToolbarButton
            disabled={svg === null}
            icon={DownloadIcon}
            label={t('Download SVG')}
            onClick={() => svg && downloadSvg(svg)}
          />
          <ToolbarButton
            disabled={svg === null}
            icon={Maximize2Icon}
            label={t('Full screen')}
            onClick={() => {
              setZoom(1)
              setFullscreen(true)
            }}
          />
        </div>
      </div>

      {state.status === 'error' && !loading && (
        <div className='border-border/70 bg-destructive/5 text-destructive flex items-start gap-2 border-b px-3 py-2 text-xs'>
          <AlertTriangleIcon className='mt-0.5 size-3.5 shrink-0' />
          <span className='min-w-0 break-words'>
            {t('Failed to render diagram')}: {state.message}
          </span>
        </div>
      )}

      {showDiagram ? (
        <div
          className='flex justify-center overflow-x-auto p-4 [&_svg]:h-auto [&_svg]:max-w-full'
          // securityLevel strict: mermaid 已用 DOMPurify 清洗 SVG
          dangerouslySetInnerHTML={{ __html: svg }}
        />
      ) : (
        source
      )}

      <Dialog onOpenChange={setFullscreen} open={fullscreen}>
        <DialogContent className='flex h-[90vh] max-w-[min(96vw,90rem)] flex-col gap-2 p-3 sm:max-w-[min(96vw,90rem)]'>
          <DialogTitle className='sr-only'>Mermaid</DialogTitle>
          <div className='flex items-center gap-1 pr-8'>
            <ToolbarButton
              disabled={zoom <= MIN_ZOOM}
              icon={MinusIcon}
              label={t('Zoom out')}
              onClick={() =>
                setZoom((value) => Math.max(MIN_ZOOM, value - ZOOM_STEP))
              }
            />
            <span className='text-muted-foreground w-12 text-center text-xs tabular-nums'>
              {Math.round(zoom * 100)}%
            </span>
            <ToolbarButton
              disabled={zoom >= MAX_ZOOM}
              icon={PlusIcon}
              label={t('Zoom in')}
              onClick={() =>
                setZoom((value) => Math.min(MAX_ZOOM, value + ZOOM_STEP))
              }
            />
            <ToolbarButton
              icon={RotateCcwIcon}
              label={t('Reset zoom')}
              onClick={() => setZoom(1)}
            />
          </div>
          <div className='bg-muted/20 min-h-0 flex-1 overflow-auto rounded-md border'>
            {svg && (
              <div
                className={cn(
                  'flex min-h-full min-w-full origin-top-left items-center justify-center p-6',
                  '[&_svg]:h-auto [&_svg]:w-full [&_svg]:!max-w-none'
                )}
                style={{ width: `${zoom * 100}%` }}
                dangerouslySetInnerHTML={{ __html: svg }}
              />
            )}
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
})
