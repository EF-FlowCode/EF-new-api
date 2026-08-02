import type { Message, ContentPart } from '../types'
import { getVersionText } from './message-utils'

// 轻量、无依赖的 token 估算，仅供 UI 参考（非精确分词）：
// CJK 字符 ≈ 1 token/字，其余 ≈ 1 token / 4 字符，外加每条消息角色开销。
const CJK_RE =
  /[㐀-鿿豈-﫿぀-ヿ가-힯]/g
const PER_MESSAGE_OVERHEAD = 4

export function estimateTextTokens(text: string): number {
  if (!text) return 0
  const cjk = (text.match(CJK_RE) || []).length
  const rest = text.length - cjk
  return Math.ceil(cjk + rest / 4)
}

export interface ConversationUsage {
  // 估算文本 token（不含图片视觉成本）
  tokens: number
  // 计入的消息条数
  messages: number
  // 图片数（vision 成本依分辨率，难估，单列不混入 tokens）
  images: number
}

export function estimateConversationUsage(
  messages: Message[]
): ConversationUsage {
  let tokens = 0
  let images = 0
  for (const m of messages) {
    const content = m.versions?.[0]?.content
    if (Array.isArray(content)) {
      for (const p of content as ContentPart[]) {
        if (p.type === 'image_url' && p.image_url?.url) images++
      }
    }
    tokens +=
      estimateTextTokens(getVersionText(content ?? '')) + PER_MESSAGE_OVERHEAD
  }
  return { tokens, messages: messages.length, images }
}
