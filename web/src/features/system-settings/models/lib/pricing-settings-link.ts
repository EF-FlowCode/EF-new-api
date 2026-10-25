// Links into 系统设置 → 模型 → 定价比例. A model name opens that model's price
// editor, so "price not configured" errors can jump straight to the fix.
export const PRICING_SETTINGS_PATH = '/system-settings/models/ratio'

export function pricingSettingsUrl(model?: string): string {
  const name = model?.trim()
  return name
    ? `${PRICING_SETTINGS_PATH}?model=${encodeURIComponent(name)}`
    : PRICING_SETTINGS_PATH
}

/** The model to open, read once from the current URL. */
export function pricingModelFromSearch(search: string): string | null {
  const name = new URLSearchParams(search).get('model')?.trim()
  return name || null
}
