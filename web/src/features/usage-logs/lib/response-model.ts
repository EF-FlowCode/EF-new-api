import type { LogOtherData } from '../types'

export type ResponseModelObservation = NonNullable<
  LogOtherData['response_model']
>

/**
 * eflow: 按上游 ed7c4e35d + 972aed197 —— 判断上游返回的模型名是否值得警示。
 *
 * 与 relay/common/response_model.go 同口径：忽略大小写，返回名以请求模型或
 * 上游请求模型开头（日期版本、变体）或结尾（"deepseek/deepseek-v4.1-flash"
 * 这类供应商路径）都算兼容。日志不存标志位，每行都按当前规则现算。
 */
export function isResponseModelMismatch(
  observation: ResponseModelObservation | undefined | null
): boolean {
  if (!observation) return false
  const returned = (observation.returned_model ?? '').toLowerCase()
  if (returned.trim() === '') return false
  for (const candidate of [
    observation.requested_model,
    observation.upstream_model,
  ]) {
    const expected = (candidate ?? '').toLowerCase()
    if (expected === '') continue
    if (returned.startsWith(expected) || returned.endsWith(expected)) {
      return false
    }
  }
  return true
}

/**
 * 模型徽章是否需要可展开的详情：有模型映射，或上游返回/上游请求的模型与
 * 请求模型不同（含不一致）。
 */
export function hasResponseModelDetails(
  observation: ResponseModelObservation | undefined | null
): boolean {
  if (!observation) return false
  return (
    isResponseModelMismatch(observation) ||
    observation.returned_model !== observation.requested_model ||
    (!!observation.upstream_model &&
      observation.upstream_model !== observation.requested_model)
  )
}
