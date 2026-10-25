/**
 * 错误对象携带的 HTTP 状态码（axios 风格的 `error.response.status`）。
 */
export function getHttpStatus(error: unknown): number | undefined {
  if (typeof error !== 'object' || error === null) return undefined
  const response = (error as Record<string, unknown>).response
  if (typeof response !== 'object' || response === null) return undefined
  const status = (response as Record<string, unknown>).status
  return typeof status === 'number' ? status : undefined
}

/**
 * 通用错误页显示的大号状态码。
 * - 没有 error：作为 /500、/errors/internal-server-error 等显式错误页渲染，显示 500；
 * - 有 error：只显示它真实携带的 HTTP 状态码；渲染异常等没有状态码的错误
 *   返回 undefined，不再一律冒充「500」。
 */
export function getErrorPageStatus(error: unknown): number | undefined {
  return error === undefined ? 500 : getHttpStatus(error)
}
