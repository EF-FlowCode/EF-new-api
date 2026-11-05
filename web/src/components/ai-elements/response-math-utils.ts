/**
 * `$5 and $10` is money, not a formula. Like Pandoc, a single-dollar formula
 * must not start or end with whitespace.
 */
export function isLikelyCurrencyMath(content: string, markup?: string) {
  return markup === '$' && (/^\s/.test(content) || /\s$/.test(content))
}
