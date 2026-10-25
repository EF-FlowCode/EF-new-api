/** A scoped dashboard access token (the plaintext is never listed). */
export interface AccessTokenItem {
  id: number
  name: string
  token_ref: string
  token_hint: string
  scopes: string[]
  /** Unix seconds; 0 means the token never expires. */
  expires_at: number
  last_used_at: number
  last_used_ip: string
  created_at: number
}

/** The legacy system access token, listed only during its transition period. */
export interface LegacyAccessToken {
  token_hint: string
  token_ref: string
  last_used_at?: number | null
  last_used_ip?: string
  retire_at: number
}

export interface AccessTokenList {
  items: AccessTokenItem[]
  legacy: LegacyAccessToken | null
}

export interface AccessTokenAction {
  action: string
  label_key: string
  description_key?: string
}

export interface AccessTokenResource {
  resource: string
  label_key: string
  actions: AccessTokenAction[]
}

export interface AccessTokenCatalogGroup {
  group: 'personal' | 'admin' | 'system'
  resources: AccessTokenResource[]
}

export interface AccessTokenCatalog {
  groups: AccessTokenCatalogGroup[]
  max_tokens: number
  default_expiry_days: number
}

export interface CreatedAccessToken {
  token: string
  item: AccessTokenItem
}
