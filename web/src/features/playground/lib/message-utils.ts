import { nanoid } from 'nanoid'
import { MESSAGE_ROLES, MESSAGE_STATUS, ERROR_MESSAGES } from '../constants'
import type {
  Message,
  MessageVersion,
  ChatCompletionMessage,
  ContentPart,
  ThinkStreamState,
} from '../types'

/**
 * Create a new message version
 */
export function createMessageVersion(
  content: string | ContentPart[]
): MessageVersion {
  return {
    id: nanoid(),
    content,
  }
}

/**
 * Extract plain text from a version's content payload.
 * String → returned as-is. ContentPart[] → first 'text' part.
 * Used everywhere we need to do string ops (trim, parseThinkTags, +chunk).
 */
export function getVersionText(content: string | ContentPart[]): string {
  if (typeof content === 'string') return content
  return getTextContent(content)
}

/**
 * 当前版本 = 最后一个版本。重生成会向 versions 追加新版本，最后一个即"最新
 * 答案"（流式/定稿/API 上下文都以它为准），旧版本保留供 BranchSelector 对比。
 * 单版本消息时 last === [0]，行为与旧版一致。
 */
export function getCurrentVersion(message: Message): MessageVersion {
  return (
    message.versions[message.versions.length - 1] || {
      id: 'default',
      content: '',
    }
  )
}

/**
 * 更新当前（最后一个）版本的内容，保留之前的所有版本。
 */
export function updateCurrentVersionContent(
  message: Message,
  content: string
): Message {
  const currentVersion = getCurrentVersion(message)
  const prev = message.versions.slice(0, -1)
  return {
    ...message,
    versions: [...prev, { ...currentVersion, content }],
  }
}

/**
 * Create a user message
 */
export function createUserMessage(content: string | ContentPart[]): Message {
  return {
    key: nanoid(),
    from: MESSAGE_ROLES.USER,
    versions: [createMessageVersion(content)],
  }
}

/**
 * Create a loading assistant message
 */
export function createLoadingAssistantMessage(): Message {
  return {
    key: nanoid(),
    from: MESSAGE_ROLES.ASSISTANT,
    versions: [createMessageVersion('')],
    reasoning: undefined,
    isReasoningComplete: false,
    isContentComplete: false,
    isReasoningStreaming: false,
    status: MESSAGE_STATUS.LOADING,
  }
}

/**
 * Build message content with optional images
 */
export function buildMessageContent(
  text: string,
  imageUrls: string[] = []
): string | ContentPart[] {
  const validImages = imageUrls.filter((url) => url.trim() !== '')

  if (validImages.length === 0) {
    return text
  }

  const parts: ContentPart[] = [
    {
      type: 'text',
      text: text || '',
    },
    ...validImages.map((url) => ({
      type: 'image_url' as const,
      image_url: { url: url.trim() },
    })),
  ]

  return parts
}

/**
 * Extract text content from message content
 */
export function getTextContent(content: string | ContentPart[]): string {
  if (typeof content === 'string') {
    return content
  }

  if (Array.isArray(content)) {
    const textPart = content.find((part) => part.type === 'text')
    return textPart?.text || ''
  }

  return ''
}

/**
 * Format message for API request
 */
export function formatMessageForAPI(message: Message): ChatCompletionMessage {
  const currentVersion = getCurrentVersion(message)
  return {
    role: message.from,
    content: currentVersion.content,
  }
}

/**
 * Check if message is valid for API request
 * Excludes loading/streaming assistant messages and empty content
 */
export function isValidMessage(message: Message): boolean {
  if (!message || !message.from || !message.versions.length) return false

  const content = getCurrentVersion(message).content
  if (content === undefined) return false

  // Multimodal user message (ContentPart[]): valid as long as it has any part.
  if (Array.isArray(content)) {
    return content.length > 0
  }

  // Exclude empty assistant messages (loading/streaming placeholders)
  if (message.from === 'assistant' && !content.trim()) return false

  return true
}

/**
 * Parse content to separate thinking from visible text
 * Handles both complete and incomplete <think> tags
 */
export function parseThinkTags(content: string): {
  visibleContent: string
  reasoning: string
  hasUnclosedTag: boolean
} {
  if (!content.includes('<think>')) {
    return { visibleContent: content, reasoning: '', hasUnclosedTag: false }
  }

  const visibleParts: string[] = []
  const reasoningParts: string[] = []
  let currentPos = 0
  let hasUnclosed = false

  while (true) {
    // Find next <think> tag
    const openPos = content.indexOf('<think>', currentPos)

    if (openPos === -1) {
      // No more think tags, add remaining content
      if (currentPos < content.length) {
        visibleParts.push(content.substring(currentPos))
      }
      break
    }

    // Add visible content before this tag
    if (openPos > currentPos) {
      visibleParts.push(content.substring(currentPos, openPos))
    }

    // Look for matching </think> tag
    const closePos = content.indexOf('</think>', openPos + 7)

    if (closePos === -1) {
      // Unclosed tag: rest is reasoning buffer
      reasoningParts.push(content.substring(openPos + 7))
      hasUnclosed = true
      break
    }

    // Extract reasoning content between tags
    reasoningParts.push(content.substring(openPos + 7, closePos))
    currentPos = closePos + 8
  }

  return {
    visibleContent: visibleParts.join('').trim(),
    reasoning: reasoningParts.join('\n\n').trim(),
    hasUnclosedTag: hasUnclosed,
  }
}

/**
 * Update the last assistant message with an error
 * @param messages - Current messages array
 * @param errorMessage - Error message to display
 * @returns Updated messages array
 */
export function updateAssistantMessageWithError(
  messages: Message[],
  errorMessage: string,
  errorCode?: string
): Message[] {
  return updateLastAssistantMessage(messages, (message) => {
    const updatedMessage = updateCurrentVersionContent(
      message,
      `${ERROR_MESSAGES.API_REQUEST_ERROR}: ${errorMessage}`
    )
    return {
      ...updatedMessage,
      status: MESSAGE_STATUS.ERROR,
      isReasoningStreaming: false,
      errorCode: errorCode || null,
    }
  })
}

/**
 * Helper function to update the last assistant message
 * @param messages - Current messages array
 * @param updater - Function to update the message
 * @returns Updated messages array or original if no assistant message found
 */
export function updateLastAssistantMessage(
  messages: Message[],
  updater: (message: Message) => Message
): Message[] {
  if (messages.length === 0) return messages
  const last = messages[messages.length - 1]
  if (!last || last.from !== MESSAGE_ROLES.ASSISTANT) return messages

  const updated = [...messages]
  updated[updated.length - 1] = updater(last)
  return updated
}

const THINK_OPEN = '<think>'
const THINK_CLOSE = '</think>'

/**
 * 求 `rest` 末尾最长、且是 `tag` 真前缀的后缀长度（用于把被 chunk 边界截断的
 * 标签暂存，避免误判）。返回 0..min(rest.len, tag.len-1)。
 */
function thinkPartialSuffixLen(rest: string, tag: string): number {
  const max = Math.min(rest.length, tag.length - 1)
  for (let k = max; k > 0; k--) {
    if (rest.slice(rest.length - k) === tag.slice(0, k)) return k
  }
  return 0
}

/**
 * 增量喂入一个 chunk：只处理新增内容（+ 上次暂存的 pending），就地累积
 * visible / reasoning，维护 inThink / pending。整段流式总成本 O(n) 而非 O(n²)。
 */
function feedThinkParser(state: ThinkStreamState, chunk: string): void {
  const data = state.pending + chunk
  state.pending = ''
  let i = 0
  while (i < data.length) {
    if (!state.inThink) {
      const idx = data.indexOf(THINK_OPEN, i)
      if (idx === -1) {
        const rest = data.slice(i)
        const keep = thinkPartialSuffixLen(rest, THINK_OPEN)
        state.visible += rest.slice(0, rest.length - keep)
        state.pending = rest.slice(rest.length - keep)
        break
      }
      state.visible += data.slice(i, idx)
      i = idx + THINK_OPEN.length
      state.inThink = true
    } else {
      const idx = data.indexOf(THINK_CLOSE, i)
      if (idx === -1) {
        const rest = data.slice(i)
        const keep = thinkPartialSuffixLen(rest, THINK_CLOSE)
        state.reasoning += rest.slice(0, rest.length - keep)
        state.pending = rest.slice(rest.length - keep)
        break
      }
      state.reasoning += data.slice(i, idx)
      i = idx + THINK_CLOSE.length
      state.inThink = false
    }
  }
}

/**
 * Process content chunk during streaming
 * Separates <think> reasoning from visible content in real-time
 * Note: versions[0].content keeps the full raw content (with tags) during streaming.
 * 解析走增量：每 token 只处理新增 chunk，不再对全量内容重跑 parseThinkTags。
 */
export function processStreamingContent(
  message: Message,
  contentChunk?: string
): Message {
  const currentVersion = getCurrentVersion(message)
  // 流式 chunk 只走 assistant，正常情况下 content 永远是 string；
  // 如有人手动把 assistant 改成多模态（不该），用 getVersionText 兜底取文本。
  const prevText = getVersionText(currentVersion.content)
  const fullContent = contentChunk ? prevText + contentChunk : prevText

  let state = message.thinkStream
  if (!state) {
    // 无状态（首 chunk，或历史消息续传）：用已有 prevText 一次性建态（一次
    // O(prevLen)，之后逐 chunk 增量），保证总成本仍是 O(n)。
    state = { inThink: false, pending: '', reasoning: '', visible: '' }
    if (prevText) feedThinkParser(state, prevText)
  }
  if (contentChunk) feedThinkParser(state, contentChunk)

  // 有 <think> 抽出的 reasoning 优先；否则保留既有（如 API reasoning_content）
  const finalReasoning = state.reasoning
    ? { content: state.reasoning, duration: 0 }
    : message.reasoning

  return {
    ...updateCurrentVersionContent(message, fullContent),
    reasoning: finalReasoning,
    isReasoningStreaming: state.inThink,
    thinkStream: state,
    streamingVisible: state.visible,
  }
}

/**
 * Finalize message after streaming completes
 * Cleans content and consolidates reasoning from all sources
 */
export function finalizeMessage(
  message: Message,
  apiReasoningContent?: string
): Message {
  const currentVersion = getCurrentVersion(message)
  const { visibleContent, reasoning } = parseThinkTags(
    getVersionText(currentVersion.content)
  )

  // Priority:
  // 1. API reasoning_content passed as parameter (non-streaming response)
  // 2. Existing message.reasoning (from streaming reasoning_content)
  // 3. Extracted think tags from content
  const finalReasoning =
    apiReasoningContent || message.reasoning?.content || reasoning || ''

  return {
    ...updateCurrentVersionContent(message, visibleContent),
    reasoning: finalReasoning
      ? { content: finalReasoning, duration: message.reasoning?.duration || 0 }
      : undefined,
    isReasoningStreaming: false,
    // 清除流式瞬态，最终态只保留权威的 parseThinkTags 结果
    thinkStream: undefined,
    streamingVisible: undefined,
  }
}

/**
 * Sanitize messages loaded from storage
 * Converts stuck loading/streaming messages to stable state
 */
export function sanitizeMessagesOnLoad(messages: Message[]): Message[] {
  let targetIndex = -1
  for (let i = messages.length - 1; i >= 0; i--) {
    const m = messages[i]
    if (
      m?.from === MESSAGE_ROLES.ASSISTANT &&
      (m?.status === MESSAGE_STATUS.LOADING ||
        m?.status === MESSAGE_STATUS.STREAMING)
    ) {
      targetIndex = i
      break
    }
  }

  if (targetIndex === -1) return messages

  const finalized = finalizeMessage(messages[targetIndex])
  const hasContent = getVersionText(
    finalized.versions?.[0]?.content ?? ''
  ).trim()
  const hasReasoning = finalized.reasoning?.content?.trim()

  const sanitized: Message =
    hasContent || hasReasoning
      ? {
          ...finalized,
          status: MESSAGE_STATUS.COMPLETE,
          isReasoningStreaming: false,
        }
      : {
          ...updateCurrentVersionContent(
            finalized,
            `${ERROR_MESSAGES.API_REQUEST_ERROR}: ${ERROR_MESSAGES.INTERRUPTED}`
          ),
          status: MESSAGE_STATUS.ERROR,
          isReasoningStreaming: false,
        }

  const result = [...messages]
  result[targetIndex] = sanitized
  return result
}
