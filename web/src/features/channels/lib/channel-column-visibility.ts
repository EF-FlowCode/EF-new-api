import type { VisibilityState } from '@tanstack/react-table'

export const CHANNEL_COLUMN_VISIBILITY_STORAGE_KEY =
  'channels-column-visibility'

/**
 * Keep the channel table defaults explicit so newly added columns do not
 * unexpectedly become visible or hidden for existing users.
 */
export const DEFAULT_CHANNEL_COLUMN_VISIBILITY: VisibilityState = {
  id: true,
  name: true,
  type: true,
  status: true,
  models: false,
  group: true,
  address: true,
  tag: false,
  priority: true,
  weight: true,
  balance: true,
  response_time: true,
  test_time: false,
}

function mergeChannelColumnVisibility(value: unknown): VisibilityState {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return { ...DEFAULT_CHANNEL_COLUMN_VISIBILITY }
  }

  const saved = value as Record<string, unknown>
  return Object.fromEntries(
    Object.entries(DEFAULT_CHANNEL_COLUMN_VISIBILITY).map(
      ([columnId, fallback]) => [
        columnId,
        typeof saved[columnId] === 'boolean' ? saved[columnId] : fallback,
      ]
    )
  )
}

export function parseChannelColumnVisibility(
  rawValue: string | null
): VisibilityState {
  if (!rawValue) return { ...DEFAULT_CHANNEL_COLUMN_VISIBILITY }

  try {
    return mergeChannelColumnVisibility(JSON.parse(rawValue))
  } catch {
    return { ...DEFAULT_CHANNEL_COLUMN_VISIBILITY }
  }
}

export function loadChannelColumnVisibility(): VisibilityState {
  if (typeof window === 'undefined') {
    return { ...DEFAULT_CHANNEL_COLUMN_VISIBILITY }
  }

  try {
    return parseChannelColumnVisibility(
      window.localStorage.getItem(CHANNEL_COLUMN_VISIBILITY_STORAGE_KEY)
    )
  } catch {
    return { ...DEFAULT_CHANNEL_COLUMN_VISIBILITY }
  }
}

export function saveChannelColumnVisibility(
  columnVisibility: VisibilityState
): void {
  if (typeof window === 'undefined') return

  try {
    window.localStorage.setItem(
      CHANNEL_COLUMN_VISIBILITY_STORAGE_KEY,
      JSON.stringify(mergeChannelColumnVisibility(columnVisibility))
    )
  } catch {
    // Storage may be unavailable in private browsing or when over quota.
  }
}
