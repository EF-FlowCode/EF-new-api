// eflow: inbound protocol and route of a relay log, admin only.
import type { LogOtherData } from '../types'

export type InboundProtocol = 'Chat' | 'Messages' | 'Responses'

const INBOUND_PROTOCOLS: Record<string, InboundProtocol> = {
  '/v1/chat/completions': 'Chat',
  '/v1/messages': 'Messages',
  '/v1/responses': 'Responses',
  '/v1/responses/compact': 'Responses',
}

export function inboundProtocol(
  path: string | undefined
): InboundProtocol | null {
  if (!path) return null
  const normalized = path.split('?')[0].replace(/^\/pg\//, '/v1/')
  return INBOUND_PROTOCOLS[normalized] ?? null
}

export type ProtocolRoute =
  | { kind: 'native' }
  | { kind: 'chat_fallback' }
  | { kind: 'adaptor_fallback' }
  | { kind: 'converted'; target: string }

/**
 * How the final attempt reached the upstream. Logs with protocol attempts use
 * the last attempt; older logs and unconfirmed channels fall back to the
 * request conversion chain.
 */
export function protocolRoute(
  other: LogOtherData | null | undefined
): ProtocolRoute | null {
  if (!other) return null
  const attempts = other.admin_info?.protocol_attempts
  const last = attempts?.[attempts.length - 1]
  if (
    last?.mode === 'native' ||
    last?.mode === 'chat_fallback' ||
    last?.mode === 'adaptor_fallback'
  ) {
    return { kind: last.mode }
  }
  const chain = Array.isArray(other.request_conversion)
    ? other.request_conversion.filter(Boolean)
    : []
  if (chain.length === 0) return null
  if (chain.length === 1) return { kind: 'native' }
  return { kind: 'converted', target: chain[chain.length - 1] }
}

export function protocolRouteLabel(
  route: ProtocolRoute,
  t: (key: string, opts?: Record<string, unknown>) => string
): string {
  switch (route.kind) {
    case 'native':
      return t('Native')
    case 'chat_fallback':
      return t('Converted to Chat')
    case 'adaptor_fallback':
      return t('converted to Messages')
    case 'converted':
      return t('Converted to {{target}}', { target: route.target })
  }
}
