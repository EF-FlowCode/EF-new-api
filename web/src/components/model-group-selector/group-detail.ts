import { translateServerText } from '@/lib/server-error-message'

export interface GroupDetailSource {
  ratio?: number | string
  desc?: string
  description?: string
}

/**
 * The line under a group name: its description and billing ratio. Built-in
 * descriptions arrive as English source keys, and the auto group has a
 * non-numeric ratio.
 */
export function formatGroupDetail(
  group: GroupDetailSource,
  t: (key: string, options?: Record<string, unknown>) => string
): string {
  const description = group.desc
    ? translateServerText(t, group.desc)
    : (group.description ?? '')
  if (!group.ratio) return description
  const value = typeof group.ratio === 'number' ? group.ratio : t('Auto')
  return `${description} · ${t('Ratio: {{value}}', { value })}`
}
