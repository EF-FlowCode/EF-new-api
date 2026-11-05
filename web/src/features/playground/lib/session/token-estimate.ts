import { MESSAGE_ROLES } from '../../constants'
import type { Message } from '../../types'
import {
  getMessageAttachments,
  getMessageContent,
} from '../message/message-utils'

// 轻量、无依赖的 token 估算，仅供 UI 参考（非精确分词）：
// CJK 字符 ≈ 1 token/字，其余 ≈ 1 token / 4 字符，外加每条消息角色开销。
const CJK_RE = /[㐀-鿿豈-﫿぀-ヿ가-힯]/g
const PER_MESSAGE_OVERHEAD = 4

export type ConversationUsage = {
  // 估算文本 token（含系统提示词和提取出的文件文字，不含图片 / PDF）
  tokens: number
  messages: number
  // 图片、PDF 的成本依模型与内容而定，难以估算，单独计数
  files: number
}

export function estimateTextTokens(text: string): number {
  if (!text) return 0
  const cjk = text.match(CJK_RE)?.length ?? 0
  return Math.ceil(cjk + (text.length - cjk) / 4)
}

export function estimateConversationUsage(
  messages: Message[],
  systemPrompt = ''
): ConversationUsage {
  let tokens = systemPrompt.trim()
    ? estimateTextTokens(systemPrompt) + PER_MESSAGE_OVERHEAD
    : 0
  let files = 0

  for (const message of messages) {
    tokens +=
      estimateTextTokens(getMessageContent(message)) + PER_MESSAGE_OVERHEAD
    for (const attachment of getMessageAttachments(message)) {
      if (attachment.kind === 'text') {
        tokens += estimateTextTokens(attachment.text ?? '')
      } else {
        files++
      }
    }
  }

  return { tokens, messages: messages.length, files }
}

/**
 * Keep system messages plus the most recent `keepRecent` messages.
 */
export function trimConversation(
  messages: Message[],
  keepRecent: number
): Message[] {
  const keepFrom = messages.length - keepRecent
  return messages.filter(
    (message, index) =>
      message.from === MESSAGE_ROLES.SYSTEM || index >= keepFrom
  )
}
