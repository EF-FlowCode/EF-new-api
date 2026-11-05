import { DEFAULT_CONFIG, DEFAULT_PARAMETER_ENABLED } from '../../constants'
import type { Message, ParameterEnabled, PlaygroundConfig } from '../../types'
import { loadConfig, loadParameterEnabled } from '../storage/storage'

export type MessageStateUpdater =
  | Message[]
  | ((previousMessages: Message[]) => Message[])

export function getInitialPlaygroundConfig(): PlaygroundConfig {
  return { ...DEFAULT_CONFIG, ...loadConfig() }
}

export function getInitialParameterEnabled(): ParameterEnabled {
  return { ...DEFAULT_PARAMETER_ENABLED, ...loadParameterEnabled() }
}

export function applyMessageStateUpdate(
  previousMessages: Message[],
  updater: MessageStateUpdater
): Message[] {
  return typeof updater === 'function' ? updater(previousMessages) : updater
}
