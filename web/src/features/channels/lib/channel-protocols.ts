// eflow: protocol routing helpers.
import type { UpstreamProtocol, UpstreamProtocolProbeResult } from '../types'

export const UPSTREAM_PROTOCOLS: UpstreamProtocol[] = [
  'messages',
  'chat',
  'responses',
]

export const UPSTREAM_PROTOCOL_PATHS: Record<UpstreamProtocol, string> = {
  messages: '/v1/messages',
  chat: '/v1/chat/completions',
  responses: '/v1/responses',
}

// Matches constant.SupportsProtocolRouting: OpenAI (1) and Anthropic (14).
const PROTOCOL_ROUTING_TYPES = new Set([1, 14])

export function supportsProtocolRouting(type: number): boolean {
  return PROTOCOL_ROUTING_TYPES.has(type)
}

export function normalizeUpstreamProtocols(
  values: readonly string[] | undefined
): UpstreamProtocol[] {
  const set = new Set(values ?? [])
  return UPSTREAM_PROTOCOLS.filter((protocol) => set.has(protocol))
}

/** Protocols pre-checked in the confirm dialog: only probe verdict "supported". */
export function defaultProtocolSelection(
  probe: UpstreamProtocolProbeResult | undefined
): UpstreamProtocol[] {
  if (!probe) return []
  return normalizeUpstreamProtocols(
    probe.items
      .filter((item) => item.verdict === 'supported')
      .map((item) => item.protocol)
  )
}

export type ProtocolRouteOutcome =
  | 'native'
  | 'chat_fallback'
  | 'messages_fallback'
  | 'rejected'

// Matches constant.LegacyConversionUpstreamProtocol: without Chat, an Anthropic
// channel's own adaptor converts Chat requests to Messages (never Responses).
const ANTHROPIC_CHANNEL_TYPE = 14

/** How a request on the given inbound protocol will reach the upstream. */
export function protocolRouteOutcome(
  protocol: UpstreamProtocol,
  selected: readonly UpstreamProtocol[],
  passThrough: boolean,
  channelType?: number
): ProtocolRouteOutcome {
  if (selected.includes(protocol)) return 'native'
  if (passThrough) return 'rejected'
  if (protocol !== 'chat' && selected.includes('chat')) return 'chat_fallback'
  if (
    channelType === ANTHROPIC_CHANNEL_TYPE &&
    protocol === 'chat' &&
    selected.includes('messages')
  ) {
    return 'messages_fallback'
  }
  return 'rejected'
}

export type ProtocolChange = {
  protocol: UpstreamProtocol
  before: ProtocolRouteOutcome | 'legacy'
  after: ProtocolRouteOutcome | 'legacy'
}

/** Per-inbound-protocol differences between the saved and the new selection. */
export function describeProtocolChanges(
  current: readonly UpstreamProtocol[],
  next: readonly UpstreamProtocol[],
  passThrough: boolean,
  channelType?: number
): ProtocolChange[] {
  const outcome = (
    selection: readonly UpstreamProtocol[],
    p: UpstreamProtocol
  ): ProtocolRouteOutcome | 'legacy' =>
    selection.length === 0
      ? 'legacy'
      : protocolRouteOutcome(p, selection, passThrough, channelType)
  return UPSTREAM_PROTOCOLS.map((protocol) => ({
    protocol,
    before: outcome(current, protocol),
    after: outcome(next, protocol),
  })).filter((change) => change.before !== change.after)
}
