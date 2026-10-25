import { useMemo } from 'react'
import { cn } from '@/lib/utils'

// eflow: 排行榜行内迷你波形——内联 SVG polyline,不引图表库。
//  - tone='perf':延迟趋势,沿用「数值越高画得越高」的常规读法(线上扬 = 变慢)。
//  - tone='rank':名次走势,invert 把小名次(更靠前)画在顶部(线上扬 = 排名上升)。
// 两者都只吃相对/已公开的数值,不涉及任何绝对用量。

type Tone = 'perf' | 'rank'

export interface MiniSparklineProps {
  values: number[]
  tone: Tone
  // 小值置顶(名次用)。默认 false:大值置顶(延迟用)。
  invert?: boolean
  width?: number
  height?: number
  className?: string
  ariaLabel?: string
}

const TONE_CLASS: Record<Tone, string> = {
  perf: 'text-sky-500 dark:text-sky-400',
  rank: 'text-amber-500 dark:text-amber-400',
}

export function MiniSparkline({
  values,
  tone,
  invert = false,
  width = 44,
  height = 18,
  className,
  ariaLabel,
}: MiniSparklineProps) {
  const geom = useMemo(() => {
    const pts = values.filter((v) => Number.isFinite(v))
    if (pts.length < 2) return null
    const pad = 2
    const min = Math.min(...pts)
    const max = Math.max(...pts)
    const span = max - min
    const innerW = width - pad * 2
    const innerH = height - pad * 2
    const stepX = innerW / (pts.length - 1)
    const coords = pts.map((v, i) => {
      // span===0(全相等)时画中线
      const frac =
        span === 0 ? 0.5 : invert ? (v - min) / span : (max - v) / span
      const x = pad + i * stepX
      const y = pad + frac * innerH
      return [x, y] as const
    })
    const line = coords
      .map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`)
      .join(' ')
    const [lastX, lastY] = coords[coords.length - 1]
    return { line, lastX, lastY }
  }, [values, invert, width, height])

  if (!geom) {
    return (
      <span
        className={cn(
          'text-muted-foreground/30 inline-flex items-center justify-center text-[10px]',
          className
        )}
        style={{ width, height }}
        aria-hidden
      >
        —
      </span>
    )
  }

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      className={cn('overflow-visible', TONE_CLASS[tone], className)}
      role='img'
      aria-label={ariaLabel}
      fill='none'
    >
      <polyline
        points={geom.line}
        stroke='currentColor'
        strokeWidth={1.25}
        strokeLinecap='round'
        strokeLinejoin='round'
        className='opacity-80'
      />
      <circle cx={geom.lastX} cy={geom.lastY} r={1.6} fill='currentColor' />
    </svg>
  )
}
