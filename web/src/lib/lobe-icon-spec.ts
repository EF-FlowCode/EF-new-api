/**
 * 解析 LobeHub 图标描述串（不依赖 React / @lobehub/icons，便于测试）。
 *
 * 支持：
 * - 基础："OpenAI"、"OpenAI.Color"
 * - 链式属性："OpenAI.Avatar.type={'platform'}"、"Claude.Color.size={32}"
 */

/** @lobehub/icons 各图标目录 components/ 下实际发布的变体文件名。 */
export const LOBE_ICON_VARIANTS = [
  'Avatar',
  'Brand',
  'BrandColor',
  'BrandMono',
  'Color',
  'Combine',
  'Inner',
  'Mono',
  'Morden',
  'Simple',
  'Text',
  'TextCn',
  'TextColor',
] as const

export type LobeIconVariant = (typeof LOBE_ICON_VARIANTS)[number]

const VARIANT_SET = new Set<string>(LOBE_ICON_VARIANTS)

export type LobeIconProps = Record<string, string | number | boolean>

export interface LobeIconSpec {
  baseKey: string
  variant: LobeIconVariant
  props: LobeIconProps
}

/**
 * Parse a property value from string to appropriate type
 * @param raw - Raw string value
 * @returns Parsed value (boolean, number, or string)
 */
export function parseValue(
  raw: string | undefined | null
): string | number | boolean {
  if (raw == null) return true

  let v = String(raw).trim()

  // Remove curly braces
  if (v.startsWith('{') && v.endsWith('}')) {
    v = v.slice(1, -1).trim()
  }

  // Remove quotes
  if (
    (v.startsWith('"') && v.endsWith('"')) ||
    (v.startsWith("'") && v.endsWith("'"))
  ) {
    return v.slice(1, -1)
  }

  // Boolean
  if (v === 'true') return true
  if (v === 'false') return false

  // Number
  if (/^-?\d+(?:\.\d+)?$/.test(v)) return Number(v)

  // Return as string
  return v
}

/**
 * 把图标描述串解析成「图标 id + 变体 + 属性」。
 * - 未知图标 id 返回 null（调用方显示首字母占位）；
 * - 第二段是大写开头的未知变体时按 Mono（基础图标）渲染，与原先
 *   `LobeIcons[baseKey]` 的回退一致；
 * - 未在描述串里指定 size 时使用 defaultSize。
 */
export function parseLobeIconSpec(
  iconName: string,
  defaultSize: number,
  isKnownIcon: (id: string) => boolean
): LobeIconSpec | null {
  const segments = iconName.trim().split('.')
  const baseKey = segments[0]
  if (!baseKey || !isKnownIcon(baseKey)) return null

  const second = segments[1]
  const hasVariantSegment = second !== undefined && /^[A-Z]/.test(second)
  const variant: LobeIconVariant =
    hasVariantSegment && VARIANT_SET.has(second)
      ? (second as LobeIconVariant)
      : 'Mono'

  // Parse chained properties (e.g., "type={'platform'}", "shape='square'")
  const props: LobeIconProps = {}
  for (let i = hasVariantSegment ? 2 : 1; i < segments.length; i++) {
    const seg = segments[i]
    if (!seg) continue

    const eqIdx = seg.indexOf('=')
    if (eqIdx === -1) {
      props[seg.trim()] = true
      continue
    }

    const key = seg.slice(0, eqIdx).trim()
    const valRaw = seg.slice(eqIdx + 1).trim()
    props[key] = parseValue(valRaw)
  }

  // Set size if not explicitly specified in the string
  if (props.size == null && defaultSize != null) {
    props.size = defaultSize
  }

  return { baseKey, variant, props }
}
