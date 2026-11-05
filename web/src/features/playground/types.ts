// Message types
export type MessageRole = 'user' | 'assistant' | 'system'

export type MessageStatus = 'loading' | 'streaming' | 'complete' | 'error'

export type PlaygroundMessageLayoutMode = 'alternating' | 'left'

// 用户消息附件：图片和 PDF 以 data URL 原样发送；Word、纯文本在浏览器里
// 提取成文字后作为文本发送
export type MessageAttachmentKind = 'image' | 'file' | 'text'

export interface MessageAttachment {
  kind: MessageAttachmentKind
  name: string
  mediaType: string
  // image / file：data URL；浏览器存储空间不足时本地历史里会丢弃
  url?: string
  // text：提取出的文字
  text?: string
}

// 一次回答自己的状态；当前版本的放在 Message 上，其它版本存进 version.state
export type MessageAnswerState = Pick<
  Message,
  | 'createdAt'
  | 'startedAt'
  | 'completedAt'
  | 'durationMs'
  | 'sources'
  | 'reasoning'
  | 'isReasoningStreaming'
  | 'isReasoningComplete'
  | 'isContentComplete'
  | 'status'
  | 'errorCode'
>

export interface MessageVersion {
  id: string
  content: string
  // 非当前版本的回答状态（耗时、思考过程等），切换回来时恢复
  state?: MessageAnswerState
}

export interface Message {
  key: string
  from: MessageRole
  versions: MessageVersion[]
  // 当前显示的版本下标；缺省为最后一个（重新生成会追加新版本）
  activeVersion?: number
  createdAt?: number
  startedAt?: number
  completedAt?: number
  durationMs?: number
  sources?: { href: string; title: string }[]
  reasoning?: {
    content: string
    duration: number
    startedAt?: number
    completedAt?: number
    durationMs?: number
  }
  isReasoningStreaming?: boolean
  isReasoningComplete?: boolean
  isContentComplete?: boolean
  status?: MessageStatus
  errorCode?: string | null
  // 用户消息附件，发送时与文本组成多模态 content
  attachments?: MessageAttachment[]
}

// 一个独立会话：标题 + 消息列表 + 该会话的系统提示词
export interface Session {
  id: string
  title: string
  createdAt: number
  updatedAt: number
  messages: Message[]
  // 发送时作为首条 system 注入 payload，不进消息列表
  systemPrompt?: string
}

// 系统提示词预设，跨会话共享
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
  type: 'text' | 'image_url' | 'file'
  text?: string
  image_url?: {
    url: string
  }
  file?: {
    filename: string
    file_data: string
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
      reasoning_content?: string | null
      reasoning?: string | null
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
      reasoning_content?: string | null
      reasoning?: string | null
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
  ratio: number | string
  desc?: string
  // 网页分组地区访问控制：当前地区不可用的分组为 false
  available?: boolean
}
