import { resolveModelProvider } from '@/lib/model-provider'

// eflow: 供应商识别规则统一在 @/lib/model-provider（按上游 d92612038），
// 渠道模型分组与使用日志模型徽章共用，避免两处规则漂移。
export function getModelCategory(modelName: string): string {
  return resolveModelProvider(modelName)?.name ?? 'Other'
}

export function categorizeModels(
  models: readonly string[]
): Record<string, string[]> {
  const categories: Record<string, string[]> = {}

  for (const model of models) {
    const category = getModelCategory(model)
    categories[category] ??= []
    categories[category].push(model)
  }

  return categories
}
