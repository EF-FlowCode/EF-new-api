import { isModelFetchableChannel } from '../constants'
import type {
  Channel,
  FetchModelsPreviewParams,
  FetchModelsResponse,
} from '../types'
import { hasAdvancedCustomModelListRoute } from './advanced-custom'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
  transformFormDataToUpdatePayload,
  type ChannelFormValues,
} from './channel-form'
import {
  findMissingModelsInMapping,
  formatModelsArray,
  hasModelConfigChanged,
  parseModelsString,
  validateModelMappingJson,
} from './model-mapping-validation'
import {
  collectInvalidStatusCodeEntries,
  collectNewDisallowedStatusCodeRedirects,
} from './status-code-risk-guard'

export type ChannelMutationBaseline = {
  models: string[]
  modelMapping: string
  statusCodeMapping: string
  // Status the form was opened with; an unchanged status is not sent so a
  // channel auto-disabled while the form was open is not re-enabled on save.
  status?: number
}

export type ChannelMutationHydration = {
  formValues: ChannelFormValues
  baseline: ChannelMutationBaseline
}

function createDefaultFormValues(): ChannelFormValues {
  return {
    ...CHANNEL_FORM_DEFAULT_VALUES,
    group: [...CHANNEL_FORM_DEFAULT_VALUES.group],
    rate_limit: CHANNEL_FORM_DEFAULT_VALUES.rate_limit
      ? { ...CHANNEL_FORM_DEFAULT_VALUES.rate_limit }
      : undefined,
    risk_control_headers:
      CHANNEL_FORM_DEFAULT_VALUES.risk_control_headers?.map((rule) => ({
        ...rule,
      })) ?? [],
    error_filter_rules:
      CHANNEL_FORM_DEFAULT_VALUES.error_filter_rules?.map((rule) => ({
        ...rule,
      })) ?? [],
  }
}

export function hydrateChannelMutation(
  channel?: Channel | null
): ChannelMutationHydration {
  if (!channel) {
    return {
      formValues: createDefaultFormValues(),
      baseline: {
        models: [],
        modelMapping: '',
        statusCodeMapping: '',
      },
    }
  }

  return {
    formValues: transformChannelToFormDefaults(channel),
    baseline: {
      models: parseModelsString(channel.models || ''),
      modelMapping: channel.model_mapping || '',
      statusCodeMapping: channel.status_code_mapping || '',
      status: channel.status,
    },
  }
}

export type ChannelKeyResponse = {
  success: boolean
  message?: string
  data?: { key: string }
}

export type ChannelKeyRequestScope = {
  isCurrent: () => boolean
}

export type ChannelKeyRequestGuard = {
  setOwner: (channelId: number | null) => void
  begin: (channelId: number) => ChannelKeyRequestScope
}

export function createChannelKeyRequestGuard(): ChannelKeyRequestGuard {
  let ownerChannelId: number | null = null
  let sequence = 0

  return {
    setOwner(channelId) {
      if (ownerChannelId === channelId) return
      ownerChannelId = channelId
      sequence += 1
    },
    begin(channelId) {
      sequence += 1
      const requestSequence = sequence
      return {
        isCurrent: () =>
          ownerChannelId === channelId && requestSequence === sequence,
      }
    },
  }
}

export async function loadUnlockedChannelKey(
  channelId: number | null | undefined,
  loadChannelKey: (id: number) => Promise<ChannelKeyResponse>
): Promise<string> {
  if (!channelId) {
    throw new Error('Channel is not selected')
  }

  const response = await loadChannelKey(channelId)
  if (!response.success) {
    throw new Error(response.message || 'Failed to fetch channel key')
  }

  return response.data?.key ?? ''
}

type ModelDiscoveryFormValues = Pick<
  ChannelFormValues,
  'type' | 'key' | 'base_url' | 'advanced_custom' | 'header_override' | 'proxy'
>

type ModelDiscoveryChannel = Pick<Channel, 'id' | 'type' | 'base_url'>

export type ModelDiscoveryBlockReason =
  | 'unsupported-channel'
  | 'create-key-required'
  | 'changed-connection-key-required'

export type ModelDiscoveryPlan =
  | { kind: 'blocked'; reason: ModelDiscoveryBlockReason }
  | { kind: 'preview'; request: FetchModelsPreviewParams | null }
  | { kind: 'fetch'; request: FetchModelsPreviewParams }

export function planModelDiscovery(input: {
  formValues: ModelDiscoveryFormValues
  currentChannel?: ModelDiscoveryChannel | null
}): ModelDiscoveryPlan {
  const { formValues, currentChannel } = input
  const type = formValues.type
  const advancedCustom = formValues.advanced_custom?.trim() || ''

  if (!isModelFetchableChannel(type, formValues.advanced_custom)) {
    return { kind: 'blocked', reason: 'unsupported-channel' }
  }

  if (currentChannel) {
    const key = formValues.key?.trim() || ''
    const baseUrl = formValues.base_url?.trim() || ''
    const savedBaseUrl = currentChannel.base_url?.trim() || ''
    const connectionChanged =
      type !== currentChannel.type || baseUrl !== savedBaseUrl
    const advancedCustomPreview =
      type === 8 && hasAdvancedCustomModelListRoute(advancedCustom)

    if (connectionChanged && !key && !advancedCustomPreview) {
      return {
        kind: 'blocked',
        reason: 'changed-connection-key-required',
      }
    }

    const savedAdvancedCustomPreview =
      advancedCustomPreview && currentChannel.type === 8

    return {
      kind: 'preview',
      request:
        key || advancedCustomPreview || connectionChanged
          ? {
              type,
              key: key || undefined,
              base_url: baseUrl,
              channel_id: savedAdvancedCustomPreview
                ? currentChannel.id
                : undefined,
              advanced_custom: advancedCustomPreview
                ? advancedCustom
                : undefined,
              header_override: advancedCustomPreview
                ? formValues.header_override || ''
                : undefined,
              proxy: advancedCustomPreview ? formValues.proxy || '' : undefined,
            }
          : null,
    }
  }

  const advancedCustomPreview =
    type === 8 && hasAdvancedCustomModelListRoute(advancedCustom)
  if (!formValues.key?.trim() && !advancedCustomPreview) {
    return { kind: 'blocked', reason: 'create-key-required' }
  }

  return {
    kind: 'fetch',
    request: {
      type,
      key: formValues.key,
      base_url: formValues.base_url || '',
      advanced_custom: advancedCustomPreview ? advancedCustom : undefined,
      header_override: advancedCustomPreview
        ? formValues.header_override || ''
        : undefined,
      proxy: advancedCustomPreview ? formValues.proxy || '' : undefined,
    },
  }
}

export type ModelDiscoveryResult =
  | { kind: 'fetched'; models: string[] }
  | { kind: 'empty' }

export async function executeModelDiscovery(
  plan: Extract<ModelDiscoveryPlan, { kind: 'fetch' }>,
  fetchAvailableModels: (
    request: FetchModelsPreviewParams
  ) => Promise<FetchModelsResponse>
): Promise<ModelDiscoveryResult> {
  const response = await fetchAvailableModels(plan.request)
  if (response.success && response.data) {
    return { kind: 'fetched', models: response.data }
  }
  return { kind: 'empty' }
}

export type MissingModelsResolution = 'cancel' | 'submit' | 'add'

export function settlePendingConfirmation<T>(
  pending: { current: ((value: T) => void) | null },
  value: T
): void {
  const resolve = pending.current
  pending.current = null
  resolve?.(value)
}

export function cancelPendingChannelMutationConfirmations(
  missingModels: {
    current: ((value: MissingModelsResolution) => void) | null
  },
  statusCodeRisk: { current: ((value: boolean) => void) | null }
): void {
  settlePendingConfirmation(missingModels, 'cancel')
  settlePendingConfirmation(statusCodeRisk, false)
}

export type ChannelMutationTarget =
  | { kind: 'create' }
  | { kind: 'update'; channelId: number; isMultiKey: boolean }

export type ChannelMutationUpdatePayload = Partial<Channel> & {
  multi_key_mode?: 'random' | 'polling'
  key_mode?: 'append' | 'replace'
}

type ChannelMutationResponse = {
  success: boolean
  message?: string
}

export type ChannelMutationDependencies = {
  confirmStatusCodeRisk: (detailItems: string[]) => Promise<boolean>
  confirmMissingModels: (
    missingModels: string[]
  ) => Promise<MissingModelsResolution>
  createChannel: (
    payload: ReturnType<typeof transformFormDataToCreatePayload>
  ) => Promise<ChannelMutationResponse>
  updateChannel: (
    channelId: number,
    payload: ChannelMutationUpdatePayload
  ) => Promise<ChannelMutationResponse>
  refreshChannels: () => void
  onModelsChanged?: (models: string) => void
  onRequestStateChange?: (pending: boolean) => void
}

export type ChannelMutationOutcome =
  | {
      status: 'field-error'
      field: 'key'
      message: string
    }
  | {
      status: 'validation-error'
      reason: 'invalid-status-code-mapping'
      entries: string[]
    }
  | {
      status: 'validation-error'
      reason: 'invalid-model-mapping'
      message: string
    }
  | {
      status: 'cancelled'
      reason: 'status-code-risk' | 'missing-model-mapping'
    }
  | {
      status: 'rejected'
      operation: 'create' | 'update'
      message?: string
    }
  | {
      status: 'request-error'
      operation: 'create' | 'update'
      error: unknown
    }
  | { status: 'created' }
  | { status: 'updated' }

export async function executeChannelMutation(
  input: {
    formValues: ChannelFormValues
    baseline: ChannelMutationBaseline
    target: ChannelMutationTarget
  },
  dependencies: ChannelMutationDependencies
): Promise<ChannelMutationOutcome> {
  const { baseline, target } = input
  const formValues = { ...input.formValues }

  if (target.kind === 'create' && !formValues.key?.trim()) {
    return {
      status: 'field-error',
      field: 'key',
      message: 'API key is required',
    }
  }

  if (formValues.status_code_mapping?.trim()) {
    const invalidEntries = collectInvalidStatusCodeEntries(
      formValues.status_code_mapping
    )
    if (invalidEntries.length > 0) {
      return {
        status: 'validation-error',
        reason: 'invalid-status-code-mapping',
        entries: invalidEntries,
      }
    }

    const riskyRedirects = collectNewDisallowedStatusCodeRedirects(
      baseline.statusCodeMapping,
      formValues.status_code_mapping
    )
    if (
      riskyRedirects.length > 0 &&
      !(await dependencies.confirmStatusCodeRisk(riskyRedirects))
    ) {
      return { status: 'cancelled', reason: 'status-code-risk' }
    }
  }

  const modelMapping = formValues.model_mapping?.trim() || ''
  if (modelMapping) {
    const validation = validateModelMappingJson(modelMapping)
    if (!validation.valid) {
      return {
        status: 'validation-error',
        reason: 'invalid-model-mapping',
        message: validation.error || 'Invalid model mapping',
      }
    }

    const normalizedModels = parseModelsString(formValues.models || '')
    const missingModels = findMissingModelsInMapping(
      modelMapping,
      normalizedModels
    )
    const shouldPromptMissing =
      missingModels.length > 0 &&
      hasModelConfigChanged(
        normalizedModels,
        modelMapping,
        baseline.models,
        baseline.modelMapping
      )

    if (shouldPromptMissing) {
      const resolution = await dependencies.confirmMissingModels(missingModels)
      if (resolution === 'cancel') {
        return { status: 'cancelled', reason: 'missing-model-mapping' }
      }
      if (resolution === 'add') {
        formValues.models = formatModelsArray([
          ...normalizedModels,
          ...missingModels,
        ])
        dependencies.onModelsChanged?.(formValues.models)
      }
    }
  }

  const operation = target.kind === 'create' ? 'create' : 'update'
  dependencies.onRequestStateChange?.(true)
  try {
    const response =
      target.kind === 'create'
        ? await dependencies.createChannel(
            transformFormDataToCreatePayload(formValues)
          )
        : await dependencies.updateChannel(
            target.channelId,
            target.isMultiKey
              ? {
                  ...transformFormDataToUpdatePayload(
                    formValues,
                    target.channelId,
                    baseline.status
                  ),
                  key_mode: formValues.key_mode,
                  multi_key_mode: formValues.multi_key_type,
                }
              : transformFormDataToUpdatePayload(
                  formValues,
                  target.channelId,
                  baseline.status
                )
          )

    if (!response.success) {
      return { status: 'rejected', operation, message: response.message }
    }

    dependencies.refreshChannels()
    return target.kind === 'create'
      ? { status: 'created' }
      : { status: 'updated' }
  } catch (error: unknown) {
    return { status: 'request-error', operation, error }
  } finally {
    dependencies.onRequestStateChange?.(false)
  }
}
