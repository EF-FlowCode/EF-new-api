/**
 * Value parsing shared by the parameter override editor.
 */

export const parseLooseValue = (valueText: string): unknown => {
  const raw = String(valueText ?? '').trim()
  if (raw === '') return ''
  try {
    return JSON.parse(raw)
  } catch {
    return raw
  }
}

/**
 * Regex patterns are literal strings, even when they look like JSON values
 * (for example `123` or `"quoted"`). Other modes keep the loose JSON parsing.
 */
export const parseConditionValue = (condition: {
  mode: string
  value_text: string
}): unknown => {
  if (condition.mode === 'regex') return condition.value_text
  return parseLooseValue(condition.value_text)
}
