export const BUILTIN_BINDING_TYPE_BY_FIELD = {
  email: 'email',
  github_id: 'github',
  discord_id: 'discord',
  wechat_id: 'wechat',
  oidc_id: 'oidc',
  telegram_id: 'telegram',
  linux_do_id: 'linuxdo',
} as const

export type BuiltinBindingField = keyof typeof BUILTIN_BINDING_TYPE_BY_FIELD
export type BuiltinBindingType =
  (typeof BUILTIN_BINDING_TYPE_BY_FIELD)[BuiltinBindingField]
