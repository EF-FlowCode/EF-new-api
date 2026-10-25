export const isObjectRecord = (
  data: unknown
): data is Record<string, unknown> =>
  typeof data === 'object' && data !== null && !Array.isArray(data)

export const isArray = (data: unknown): data is unknown[] => Array.isArray(data)
