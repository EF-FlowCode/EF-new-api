import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'
import { getUserGroups, getUserModels } from '../api'
import {
  getGroupFallback,
  getModelFallback,
  getVisibleGroups,
  shouldClearModelForGroup,
} from '../lib'
import type { GroupOption, ModelOption, PlaygroundConfig } from '../types'

type UsePlaygroundOptionsParams = {
  currentGroup: string
  currentModel: string
  setGroups: (groups: GroupOption[]) => void
  setModels: (models: ModelOption[]) => void
  updateConfig: <K extends keyof PlaygroundConfig>(
    key: K,
    value: PlaygroundConfig[K]
  ) => void
}

export function usePlaygroundOptions({
  currentGroup,
  currentModel,
  setGroups,
  setModels,
  updateConfig,
}: UsePlaygroundOptionsParams) {
  const {
    data: modelsData,
    error: modelsError,
    isError: isModelsError,
    isLoading: isLoadingModels,
  } = useQuery({
    queryKey: ['playground-models', currentGroup],
    queryFn: async () =>
      requireServerSuccess(await getUserModels(currentGroup)),
    enabled: currentGroup !== '',
  })

  const {
    data: groupsData,
    error: groupsError,
    isError: isGroupsError,
  } = useQuery({
    queryKey: ['playground-groups'],
    queryFn: async () => requireServerSuccess(await getUserGroups()),
  })

  useEffect(() => {
    if (!isModelsError) return

    handleServerError(modelsError)
  }, [isModelsError, modelsError])

  useEffect(() => {
    if (!isGroupsError) return

    handleServerError(groupsError)
  }, [isGroupsError, groupsError])

  useEffect(() => {
    if (!modelsData) return

    setModels(modelsData)
    const fallback = getModelFallback(modelsData, currentModel)

    if (fallback) {
      updateConfig('model', fallback)
      return
    }

    if (shouldClearModelForGroup(modelsData, currentModel)) {
      updateConfig('model', '')
    }
  }, [modelsData, currentModel, setModels, updateConfig])

  useEffect(() => {
    if (!groupsData) return

    const visibleGroups = getVisibleGroups(groupsData, currentGroup)
    setGroups(visibleGroups)
    const fallback = getGroupFallback(visibleGroups, currentGroup)

    if (fallback) {
      updateConfig('group', fallback)
    }
  }, [groupsData, currentGroup, setGroups, updateConfig])

  return {
    isLoadingModels,
  }
}
