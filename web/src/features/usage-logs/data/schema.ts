/**
 * Zod schemas for common logs
 * This file should only contain Zod schemas and types inferred from them
 */
import { z } from 'zod'

// Usage log schema
export const usageLogSchema = z.object({
  id: z.number(),
  user_id: z.number(),
  created_at: z.number(),
  type: z.number(),
  content: z.string(),
  username: z.string().default(''),
  token_name: z.string().default(''),
  model_name: z.string().default(''),
  quota: z.number().default(0),
  prompt_tokens: z.number().default(0),
  completion_tokens: z.number().default(0),
  use_time: z.number().default(0),
  is_stream: z.boolean().default(false),
  channel: z.number().default(0),
  channel_name: z.string().nullish().default(''),
  token_id: z.number().default(0),
  group: z.string().default(''),
  ip: z.string().default(''),
  other: z.string().default(''),
  request_id: z.string().default(''),
  // eflow: 计费异常标记（仅管理员接口下发）：1 输入估算 / 2 输出估算 / 3 上游无用量免单
  billing_anomaly: z.number().optional(),
  usage_refund: z
    .object({
      batch_id: z.string(),
      quota: z.number(),
      refunded_at: z.number(),
    })
    .optional(),
})

export type UsageLog = z.infer<typeof usageLogSchema>
