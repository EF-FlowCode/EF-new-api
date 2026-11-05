import { PRICING_SETTINGS_PATH } from '@/features/system-settings/models/lib/pricing-settings-link'
import { MESSAGE_STATUS } from '../../constants'
import type { Message } from '../../types'
import { getMessageContent } from './message-utils'

// eflow 的模型价格在 系统设置 → 模型 → 定价比例。
export const MODEL_PRICING_SETTINGS_PATH = PRICING_SETTINGS_PATH

const MODEL_PRICE_ERROR_CODE = 'model_price_error'
export const FALLBACK_ERROR_CONTENT = 'An unknown error occurred'

type MessageErrorState = {
  content: string
  kind: 'generic' | 'model-price'
  showSettingsLink: boolean
}

export function isAdminRole(role?: number | null): boolean {
  return role != null && role >= 10
}

export function isErrorMessage(message: Message): boolean {
  return message.status === MESSAGE_STATUS.ERROR
}

export function getMessageErrorState(
  message: Message,
  isAdmin: boolean
): MessageErrorState | null {
  if (!isErrorMessage(message)) {
    return null
  }

  const content = getMessageContent(message) || FALLBACK_ERROR_CONTENT
  const isModelPriceError = message.errorCode === MODEL_PRICE_ERROR_CODE

  return {
    content,
    kind: isModelPriceError ? 'model-price' : 'generic',
    showSettingsLink: isModelPriceError && isAdmin,
  }
}
