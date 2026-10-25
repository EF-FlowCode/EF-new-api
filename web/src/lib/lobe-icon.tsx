/**
 * LobeHub Icon Loader
 * 按需加载并渲染 @lobehub/icons 图标：原先 `import * as LobeIcons` 会把全部约 300 个
 * 供应商图标（含 Avatar 等依赖）打进使用它的页面；现在只同步读取图标目录（toc），
 * 具体图标变体在首次渲染时按需加载，加载期间与失败时显示同尺寸的首字母占位。
 *
 * Supports:
 * - Basic: "OpenAI", "OpenAI.Color"
 * - Chained properties: "OpenAI.Avatar.type={'platform'}"
 * - Size parameter: getLobeIcon("OpenAI", 20)
 */
import {
  lazy,
  Suspense,
  type ComponentType,
  type LazyExoticComponent,
  type ReactNode,
} from 'react'
import toc from '@lobehub/icons/es/toc'
import { parseLobeIconSpec, type LobeIconVariant } from './lobe-icon-spec'

type IconComponent = ComponentType<Record<string, unknown>>
type IconModule = { default: IconComponent }

const ICON_IDS = new Set(toc.map((icon) => icon.id))
const LAZY_ICONS = new Map<string, LazyExoticComponent<IconComponent>>()

function renderLobeIconFallback(
  name: string,
  size: number | string
): ReactNode {
  return (
    <div
      className='bg-muted text-muted-foreground flex items-center justify-center rounded-full text-xs font-medium'
      style={{ width: size, height: size }}
    >
      {name.charAt(0).toUpperCase() || '?'}
    </div>
  )
}

function importIconVariant(
  baseKey: string,
  variant: LobeIconVariant
): Promise<IconModule> {
  // 只加载所选 SVG 变体，避免图标 index 顺带引入 Avatar / UI 依赖。
  return import(
    /* webpackInclude: /[\\/]components[\\/](Avatar|Brand|BrandColor|BrandMono|Color|Combine|Inner|Mono|Morden|Simple|Text|TextCn|TextColor)\.js$/ */
    `@lobehub/icons/es/${baseKey}/components/${variant}.js`
  ) as Promise<IconModule>
}

function getLazyIcon(
  baseKey: string,
  variant: LobeIconVariant
): LazyExoticComponent<IconComponent> {
  const cacheKey = `${baseKey}.${variant}`
  let Icon = LAZY_ICONS.get(cacheKey)
  if (!Icon) {
    Icon = lazy(() =>
      importIconVariant(baseKey, variant)
        // 该图标没有所请求的变体（如没有 Color 版）时回退到基础图标。
        .catch(() =>
          variant === 'Mono'
            ? Promise.reject(new Error('icon unavailable'))
            : importIconVariant(baseKey, 'Mono')
        )
        .catch(
          (): IconModule => ({
            default: (props) =>
              renderLobeIconFallback(
                baseKey,
                typeof props.size === 'number' || typeof props.size === 'string'
                  ? props.size
                  : 20
              ),
          })
        )
    )
    LAZY_ICONS.set(cacheKey, Icon)
  }
  return Icon
}

/**
 * Get LobeHub icon component by name
 * @param iconName - Icon name/description (e.g., "OpenAI", "OpenAI.Color", "Claude.Avatar")
 * @param size - Icon size (default: 20)
 * @returns Icon component or fallback
 *
 * @example
 * getLobeIcon("OpenAI", 24)
 * getLobeIcon("OpenAI.Color", 20)
 * getLobeIcon("Claude.Avatar.type={'platform'}", 32)
 */
export function getLobeIcon(
  iconName: string | undefined | null,
  size: number = 20
): ReactNode {
  const trimmedName = typeof iconName === 'string' ? iconName.trim() : ''
  if (!trimmedName) return renderLobeIconFallback('', size)

  const spec = parseLobeIconSpec(trimmedName, size, (id) => ICON_IDS.has(id))
  if (!spec) return renderLobeIconFallback(trimmedName, size)

  const Icon = getLazyIcon(spec.baseKey, spec.variant)
  const fallbackSize =
    typeof spec.props.size === 'number' || typeof spec.props.size === 'string'
      ? spec.props.size
      : size
  return (
    <Suspense fallback={renderLobeIconFallback(spec.baseKey, fallbackSize)}>
      <Icon {...spec.props} />
    </Suspense>
  )
}
