import {
  ADVANCED_CUSTOM_PRESET_REQUEST_ENDPOINTS,
  CHANNEL_TYPE_REQUEST_ENDPOINTS,
  CHANNEL_TYPE_SGLANG,
  CHANNEL_TYPE_VLLM,
} from '../constants'
import {
  getAdvancedCustomEndpointType,
  getAdvancedCustomRelayRoutes,
} from './advanced-custom'

/**
 * Where a channel's inbound endpoint support comes from, mirroring
 * model.ChannelSupportsRequestPath:
 * - preset: vLLM / SGLang fixed Advanced Custom routes
 * - routes: Custom channel with Advanced Custom relay routes
 * - legacy-custom: Custom channel without relay routes (accepts everything)
 * - type: ordinary channel, derived from its type unless overridden
 */
export type EndpointSupportSource =
  | 'preset'
  | 'routes'
  | 'legacy-custom'
  | 'type'

export type EndpointSupport = {
  source: EndpointSupportSource
  /** Derived endpoint types; null means every endpoint is accepted. */
  endpoints: string[] | null
}

export function getChannelEndpointSupport(
  type: number,
  advancedCustom: string | undefined
): EndpointSupport {
  if (type === CHANNEL_TYPE_VLLM || type === CHANNEL_TYPE_SGLANG) {
    return {
      source: 'preset',
      endpoints: ADVANCED_CUSTOM_PRESET_REQUEST_ENDPOINTS,
    }
  }
  if (type === 8) {
    const routes = getAdvancedCustomRelayRoutes(advancedCustom)
    if (routes.length === 0) return { source: 'legacy-custom', endpoints: null }
    const endpoints: string[] = []
    for (const route of routes) {
      const endpoint = getAdvancedCustomEndpointType(route.incoming_path)
      if (endpoint && !endpoints.includes(endpoint)) endpoints.push(endpoint)
    }
    return { source: 'routes', endpoints }
  }
  return {
    source: 'type',
    endpoints: CHANNEL_TYPE_REQUEST_ENDPOINTS[type] ?? null,
  }
}

/** Only ordinary channels honor the manual endpoint override. */
export function supportsEndpointOverride(type: number): boolean {
  return getChannelEndpointSupport(type, undefined).source === 'type'
}
