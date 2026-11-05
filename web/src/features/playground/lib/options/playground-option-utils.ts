import type { GroupOption, ModelOption } from '../../types'

export function getModelFallback(
  models: ModelOption[],
  currentModel: string
): string | null {
  const hasCurrentModel = models.some((model) => model.value === currentModel)

  if (hasCurrentModel || models.length === 0) {
    return null
  }

  return models[0].value
}

export function shouldClearModelForGroup(
  models: ModelOption[],
  currentModel: string
): boolean {
  if (currentModel === '') {
    return false
  }

  return !models.some((model) => model.value === currentModel)
}

/**
 * 当前地区不可用的分组不进下拉；已选中的分组保留显示，避免选择框突然变空。
 */
export function getVisibleGroups(
  groups: GroupOption[],
  currentGroup: string
): GroupOption[] {
  return groups.filter(
    (group) => group.available !== false || group.value === currentGroup
  )
}

export function getGroupFallback(
  groups: GroupOption[],
  currentGroup: string
): string | null {
  const hasCurrentGroup = groups.some((group) => group.value === currentGroup)

  if (hasCurrentGroup || groups.length === 0) {
    return null
  }

  return (
    groups.find((group) => group.value === 'default')?.value ?? groups[0].value
  )
}

export function getOptionLoadErrorMessage(
  error: unknown,
  fallbackMessage: string
): string {
  return error instanceof Error ? error.message : fallbackMessage
}
