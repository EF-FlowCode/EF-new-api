import type { AuditCategory, AuditLog } from '../api'

type Translate = (key: string, options?: Record<string, unknown>) => string

export const AUDIT_CATEGORIES: AuditCategory[] = [
  'login',
  'security',
  'operation',
  'access_token',
]

const CATEGORY_LABELS: Record<AuditCategory, string> = {
  login: 'Sign-in',
  security: 'Account security',
  operation: 'Administration',
  access_token: 'Access token',
}

// Stable action ids from the backend audit (controller/audit.go,
// middleware/audit.go). Unknown actions fall back to the stored content.
const ACTION_LABELS: Record<string, string> = {
  'user.login': 'Signed in',
  'user.create': 'Created a user',
  'user.update': 'Updated a user',
  'user.delete': 'Deleted a user',
  'user.manage': 'Managed a user',
  'user.topup_complete': 'Completed a top-up order',
  'user.account_delete': 'Deleted the account',
  'user.binding_clear': 'Cleared a sign-in binding',
  'user.oauth_unbind': 'Removed a third-party binding',
  'user.2fa_disable': 'Disabled two-factor authentication for a user',
  'user.reset_passkey': 'Reset a user passkey',
  'user.passkey_register': 'Registered a passkey',
  'user.passkey_delete': 'Deleted a passkey',
  'user.2fa_setup': 'Started two-factor setup',
  'user.2fa_enable': 'Enabled two-factor authentication',
  'user.2fa_disable_self': 'Disabled two-factor authentication',
  'user.2fa_backup_codes': 'Regenerated backup codes',
  'user.security_verify': 'Completed security verification',
  'user.password_change': 'Changed the password',
  'user.binding_start': 'Requested an account binding',
  'user.binding_bind': 'Bound an account',
  'user.binding_unbind': 'Unbound an account',
  'user.email_binding_resend': 'Resent the email confirmation code',
  'user.session_revoke': 'Signed out a device',
  'user.session_revoke_other': 'Signed out other devices',
  'access_token.generate': 'Created an access token',
  'access_token.revoke': 'Revoked an access token',
  'access_token.rename': 'Renamed an access token',
  'access_token.update': 'Changed access token permissions',
  'access_token.use': 'Used an access token',
  'channel.create': 'Created a channel',
  'channel.update': 'Updated a channel',
  'channel.delete': 'Deleted a channel',
  'channel.delete_batch': 'Deleted channels',
  'channel.delete_disabled': 'Deleted disabled channels',
  'channel.key_view': 'Viewed a channel key',
  'channel.copy': 'Copied a channel',
  'channel.multi_key_manage': 'Managed channel keys',
  'option.update': 'Changed a system setting',
  'option.payment_compliance': 'Confirmed payment compliance',
  'option.reset_ratio': 'Reset model ratios',
  'option.clear_affinity_cache': 'Cleared the channel affinity cache',
  'custom_oauth.create': 'Created an OAuth provider',
  'custom_oauth.update': 'Updated an OAuth provider',
  'custom_oauth.delete': 'Deleted an OAuth provider',
  'redemption.create': 'Created redemption codes',
  'redemption.update': 'Updated a redemption code',
  'redemption.delete': 'Deleted a redemption code',
  'redemption.delete_invalid': 'Deleted invalid redemption codes',
  'redemption.delete_batch': 'Batch deleted redemption codes',
  'model.create': 'Created a model',
  'model.update': 'Updated a model',
  'model.delete': 'Deleted a model',
  'deployment.create': 'Created a deployment',
  'deployment.update': 'Updated a deployment',
  'deployment.delete': 'Deleted a deployment',
  'usage_refund.execute': 'Executed a usage refund batch',
  'invitation_code.create': 'Created invitation codes',
  'invitation_code.delete': 'Deleted an invitation code',
  'ticket.status_update': 'Changed a ticket status',
  'ticket.reply': 'Replied to a ticket',
}

export function auditCategoryLabel(category: string, t: Translate): string {
  const key = CATEGORY_LABELS[category as AuditCategory]
  return key ? t(key) : category
}

export function loginMethodLabel(method: string, t: Translate): string {
  const twoFactor = method.endsWith('+2fa')
  const base = twoFactor ? method.slice(0, -'+2fa'.length) : method
  let label: string
  if (base === 'password') label = t('Password')
  else if (base === 'passkey') label = t('Passkey')
  else if (base === 'wechat') label = t('WeChat')
  else if (base === 'telegram') label = 'Telegram'
  else if (base.startsWith('oauth:')) label = base.slice('oauth:'.length)
  else label = base
  return twoFactor ? t('{{method}} + two-factor', { method: label }) : label
}

export function auditActionLabel(log: AuditLog, t: Translate): string {
  const action = log.other?.op?.action || log.action
  if (action === 'user.login' && log.other?.login_method) {
    return t('Signed in with {{method}}', {
      method: loginMethodLabel(log.other.login_method, t),
    })
  }
  if (action === 'generic') {
    return `${log.method} ${log.route}`.trim()
  }
  const key = ACTION_LABELS[action]
  return key ? t(key) : log.content || action
}

export function auditAuthMethodLabel(method: string | undefined, t: Translate) {
  if (method === 'access_token') return t('Access token')
  if (method === 'session') return t('Browser session')
  return method || '—'
}
