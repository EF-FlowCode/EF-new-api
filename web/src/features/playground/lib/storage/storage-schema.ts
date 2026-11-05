import { z } from 'zod'

export const STORAGE_VERSION = 1
export const MAX_STORED_MESSAGES = 100
export const MAX_STORED_MESSAGES_BYTES = 1024 * 1024
export const MAX_LOADED_MESSAGES_CHARS = 120_000
export const MAX_LOADED_MESSAGE_CHARS = 40_000

export const playgroundConfigSchema = z.object({
  model: z.string().optional(),
  group: z.string().optional(),
  temperature: z.number().optional(),
  top_p: z.number().optional(),
  max_tokens: z.number().optional(),
  frequency_penalty: z.number().optional(),
  presence_penalty: z.number().optional(),
  seed: z.number().nullable().optional(),
  stream: z.boolean().optional(),
})

export const parameterEnabledSchema = z.object({
  temperature: z.boolean().optional(),
  top_p: z.boolean().optional(),
  max_tokens: z.boolean().optional(),
  frequency_penalty: z.boolean().optional(),
  presence_penalty: z.boolean().optional(),
  seed: z.boolean().optional(),
})

const messageRoleSchema = z.enum(['user', 'assistant', 'system'])
const messageStatusSchema = z.enum([
  'loading',
  'streaming',
  'complete',
  'error',
])

const sourceSchema = z.object({
  href: z.string(),
  title: z.string(),
})

const reasoningSchema = z.object({
  content: z.string(),
  duration: z.number(),
  startedAt: z.number().optional(),
  completedAt: z.number().optional(),
  durationMs: z.number().optional(),
})

const answerStateSchema = z.object({
  createdAt: z.number().optional(),
  startedAt: z.number().optional(),
  completedAt: z.number().optional(),
  durationMs: z.number().optional(),
  sources: z.array(sourceSchema).optional(),
  reasoning: reasoningSchema.optional(),
  isReasoningStreaming: z.boolean().optional(),
  isReasoningComplete: z.boolean().optional(),
  isContentComplete: z.boolean().optional(),
  status: messageStatusSchema.optional(),
  errorCode: z.string().nullable().optional(),
})

const messageVersionSchema = z.object({
  id: z.string(),
  content: z.string(),
  state: answerStateSchema.optional(),
})

const attachmentSchema = z.object({
  kind: z.enum(['image', 'file', 'text']),
  name: z.string(),
  mediaType: z.string(),
  url: z.string().optional(),
  text: z.string().optional(),
})

const messageSchema = z.object({
  key: z.string(),
  from: messageRoleSchema,
  versions: z.array(messageVersionSchema).min(1),
  activeVersion: z.number().int().nonnegative().optional(),
  createdAt: z.number().optional(),
  startedAt: z.number().optional(),
  completedAt: z.number().optional(),
  durationMs: z.number().optional(),
  sources: z.array(sourceSchema).optional(),
  reasoning: reasoningSchema.optional(),
  isReasoningStreaming: z.boolean().optional(),
  isReasoningComplete: z.boolean().optional(),
  isContentComplete: z.boolean().optional(),
  status: messageStatusSchema.optional(),
  errorCode: z.string().nullable().optional(),
  attachments: z.array(attachmentSchema).optional(),
})

export const messagesSchema = z.array(messageSchema)

export const storedMessageSchema = messageSchema

export const sessionSchema = z.object({
  id: z.string(),
  title: z.string(),
  createdAt: z.number(),
  updatedAt: z.number(),
  // 消息逐条校验，单条损坏不连累整个会话
  messages: z.array(z.unknown()),
  systemPrompt: z.string().optional(),
})

export const systemPresetSchema = z.object({
  id: z.string(),
  name: z.string(),
  content: z.string(),
})
