// eflow: 渠道按上游地址分类，与后端 model/channel_address.go 同规则。
// base_url 的主机是 IP 字面量（含 IP:port）记为 ip，其余记为 domain；
// 留空的渠道走类型默认地址（官方域名），也算 domain。
export type ChannelAddressKind = 'ip' | 'domain'

const IPV4 = /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/

function parseHost(baseUrl: string): string | null {
  const raw = baseUrl.trim()
  if (!raw) return null
  try {
    return new URL(raw.includes('://') ? raw : `http://${raw}`).hostname
  } catch {
    return null
  }
}

export function classifyChannelAddress(
  baseUrl: string | null | undefined
): ChannelAddressKind {
  const host = parseHost(baseUrl || '')
  if (!host) return 'domain'
  // URL.hostname 会给 IPv6 带上方括号
  if (host.startsWith('[') || IPV4.test(host)) return 'ip'
  return 'domain'
}

/** 列表里展示用的地址：去掉协议和末尾斜杠，保留主机、端口和路径 */
export function formatChannelAddress(
  baseUrl: string | null | undefined
): string {
  return (baseUrl || '')
    .trim()
    .replace(/^https?:\/\//i, '')
    .replace(/\/+$/, '')
}
