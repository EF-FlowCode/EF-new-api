// Message types
export type MessageRole = 'user' | 'assistant' | 'system'

export type MessageStatus = 'loading' | 'streaming' | 'complete' | 'error'

export interface MessageVersion {
  id: string
  // string: 纯文本（assistant 永远走这里）
  // ContentPart[]: 多模态用户消息（文本 + 图片），由 buildMessageContent 生成
  content: string | ContentPart[]
}

// 流式 <think> 增量解析器的状态。挂在 Message 上，逐 token 只处理新增 chunk，
// 避免每 token 对全量内容重跑 parseThinkTags（O(n²)）。完成/加载时清除。
export interface ThinkStreamState {
  // 当前是否处于未闭合的 <think> 块内
  inThink: boolean
  // 尾部尚不能判定归属的部分（可能是被 chunk 边界截断的 <think> / </think>）
  pending: string
  // 已累积的思考内容
  reasoning: string
  // 已累积的可见正文
  visible: string
}

export interface Message {
  key: string
  from: MessageRole
  versions: MessageVersion[]
  sources?: { href: string; title: string }[]
  reasoning?: {
    content: string
    duration: number
  }
  isReasoningStreaming?: boolean
  isReasoningComplete?: boolean
  isContentComplete?: boolean
  status?: MessageStatus
  errorCode?: string | null
  // 流式期间的瞬态字段；finalizeMessage / 加载时清除，不进入最终持久态语义
  thinkStream?: ThinkStreamState
  streamingVisible?: string
}

// 一个独立会话：标题 + 消息列表。多会话持久化的基本单元。
export interface Session {
  id: string
  title: string
  createdAt: number
  updatedAt: number
  messages: Message[]
  // 该会话的系统提示词；发送时作为首条 system 注入 payload（不进消息列表 UI）
  systemPrompt?: string
}

// 系统提示词预设（跨会话共享，独立持久化）
export interface SystemPreset {
  id: string
  name: string
  content: string
}

// API payload types
export interface ChatCompletionMessage {
  role: MessageRole
  content: string | ContentPart[]
}

export interface ContentPart {
  type: 'text' | 'image_url'
  text?: string
  image_url?: {
    url: string
  }
}

export interface ChatCompletionRequest {
  model: string
  group?: string
  messages: ChatCompletionMessage[]
  stream: boolean
  temperature?: number
  top_p?: number
  max_tokens?: number
  frequency_penalty?: number
  presence_penalty?: number
  seed?: number
}

export interface ChatCompletionChunk {
  id: string
  object: string
  created: number
  model: string
  choices: Array<{
    index: number
    delta: {
      role?: MessageRole
      content?: string
      reasoning_content?: string
      // grok-4 / OpenAI Responses-API-to-chat 等推理模型走 `reasoning`
      // 字段（与 `reasoning_content` 同义），后端原样透传，前端兜底识别。
      reasoning?: string
    }
    finish_reason: string | null
  }>
}

export interface ChatCompletionResponse {
  id: string
  object: string
  created: number
  model: string
  choices: Array<{
    index: number
    message: {
      role: MessageRole
      content: string
      reasoning_content?: string
      // 同 ChatCompletionChunk.delta：兼容 grok-4 等推理模型的 `reasoning`
      reasoning?: string
    }
    finish_reason: string
  }>
  usage?: {
    prompt_tokens: number
    completion_tokens: number
    total_tokens: number
  }
}

// Configuration types
export interface PlaygroundConfig {
  model: string
  group: string
  temperature: number
  top_p: number
  max_tokens: number
  frequency_penalty: number
  presence_penalty: number
  seed: number | null
  stream: boolean
}

export interface ParameterEnabled {
  temperature: boolean
  top_p: boolean
  max_tokens: boolean
  frequency_penalty: boolean
  presence_penalty: boolean
  seed: boolean
}

// Model and group options
export interface ModelOption {
  label: string
  value: string
}

export interface GroupOption {
  label: string
  value: string
  ratio: number
  desc?: string
}
