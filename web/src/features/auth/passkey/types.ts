export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export interface PasskeyStatus {
  enabled: boolean
  last_used_at?: string | null
  backup_eligible?: boolean
  backup_state?: boolean
  [key: string]: unknown
}

export interface PasskeyOptionsPayload {
  options?: unknown
  /** Names the server-side ceremony the finish request completes. */
  flow_token?: string
  expires_at?: number
  publicKey?: unknown
  response?: unknown
  Response?: unknown
}
