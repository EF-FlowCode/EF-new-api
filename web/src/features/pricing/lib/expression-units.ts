export function expressionVersion(expression: string): {
  version: number
  body: string
} {
  const text = expression.trim()
  const match = /^v(\d+):([\s\S]*)$/.exec(text)
  return match
    ? { version: Number(match[1]), body: match[2] }
    : { version: 1, body: text }
}

// Index of the `)` matching the `(` at `open`, skipping strings and comments.
function matchingParen(text: string, open: number): number {
  let depth = 0
  let quote = ''
  let lineComment = false
  let blockComment = false
  for (let index = open; index < text.length; index += 1) {
    const ch = text[index]
    const next = text[index + 1]
    if (lineComment) {
      if (ch === '\n') lineComment = false
      continue
    }
    if (blockComment) {
      if (ch === '*' && next === '/') {
        blockComment = false
        index += 1
      }
      continue
    }
    if (quote) {
      if (ch === '\\') index += 1
      else if (ch === quote) quote = ''
      continue
    }
    if (ch === '/' && next === '/') {
      lineComment = true
      index += 1
    } else if (ch === '/' && next === '*') {
      blockComment = true
      index += 1
    } else if (ch === '"' || ch === "'" || ch === '`') quote = ch
    else if (ch === '(') depth += 1
    else if (ch === ')') {
      depth -= 1
      if (depth === 0) return index
    }
  }
  return -1
}

// Drop parentheses that wrap the whole expression; the backend parser
// (expr-lang) does not keep them in the AST either.
export function unwrapExprParens(expression: string): string {
  let text = expression.trim()
  while (text.startsWith('(') && matchingParen(text, 0) === text.length - 1) {
    text = text.slice(1, -1).trim()
  }
  return text
}

const EXPR_NUMBER_LITERAL =
  /^(?:0[xX][\da-fA-F_]+|0[oO][0-7_]+|0[bB][01_]+|\d[\d_]*(?:\.[\d_]*)?(?:[eE][+-]?[\d_]+)?)$/

// Value of an expr-lang number literal (`1000000`, `1e6`, `1_000_000`, `0xF4240`).
export function exprNumberLiteral(text: string): number | null {
  const literal = text.trim()
  if (!EXPR_NUMBER_LITERAL.test(literal)) return null
  const value = Number(literal.replace(/_/g, ''))
  return Number.isFinite(value) ? value : null
}

// Return the legacy (per-million) body of a v2 amount expression, mirroring
// pkg/billingexpr ParseFlatTokenBillingExpr: the top-level operation must be a
// division of one operand (a parenthesized group or a single call such as
// tier(...)) by a number literal equal to 1e6. Anything else is not unwrapped;
// units are never inferred from coefficients.
export function legacyExpressionView(expression: string): string | null {
  const { version, body } = expressionVersion(expression)
  if (version === 1) return body
  if (version !== 2) return null
  const text = unwrapExprParens(body)
  const call = /^[A-Za-z_$][\w$]*\s*\(/.exec(text)
  const grouped = text.startsWith('(')
  if (!grouped && !call) return null
  const close = matchingParen(text, grouped ? 0 : call![0].length - 1)
  if (close < 0) return null
  const divisor = /^\s*\/\s*(\S+)\s*$/.exec(
    text.slice(close + 1).replace(/\/\/[^\n]*|\/\*[\s\S]*?\*\//g, ' ')
  )
  if (!divisor || exprNumberLiteral(divisor[1]) !== 1_000_000) return null
  return grouped ? text.slice(1, close).trim() : text.slice(0, close + 1)
}

export function toAmountExpression(expression: string): string {
  if (!expression.trim()) return expression
  const { version, body } = expressionVersion(expression)
  if (version !== 1) return expression
  return `v2:(${body}\n) / 1000000`
}

export function expressionAmount(value: number, version: number): number {
  if (version === 2) return value
  if (version === 1) return value / 1_000_000
  throw new Error(`Unsupported billing expression version: ${version}`)
}
