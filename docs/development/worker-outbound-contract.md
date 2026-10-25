# Worker 出站请求协议

> 定位：现行开发与行为约束。带日期的部署或验证段落保留为当时记录；分类入口见[文档导航](../README.md)。

new-api 的 Worker adapter 用于获取下载资源、发送 Webhook 和通知。这些 URL
可能来自非受信输入，因此 Worker 必须与 new-api 共同执行逐跳重定向和目标地址绑定。

本机对域名的预解析只能说明解析当时的结果安全，不能约束远端 Worker 随后的 DNS
结果。仅把原 URL 交给 Worker 会留下 DNS rebinding 窗口。

## 协议版本

协议按请求字段渐进协商，不需要额外探测请求：

| 请求形态 | Worker 要求 | 兼容性 |
| --- | --- | --- |
| 包含 `redirect`、`redirect_token`，不含 `target` | v1 手动重定向 | 已支持 v1 的 Worker 可继续工作 |
| 额外包含 `target` | v2 目标绑定 | Worker 必须完成 IP 绑定并返回目标证明 |

当 SSRF 防护已开启、`apply_ip_filter_for_domain` 为 `true` 且目标是域名时，new-api
发送 v2 请求。SSRF 防护关闭、域名 IP 过滤关闭或目标本身是 IP 地址时，不需要远端
DNS 绑定，仍发送 v1 请求。每个重定向跳转都会重新校验 URL，并生成新的 token 和
目标绑定。

## v1 手动重定向

Worker 必须支持以下请求字段：

| 字段 | 值 | 要求 |
| --- | --- | --- |
| `redirect` | `manual` | 调用目标时不得在 Worker 内自动跟随重定向 |
| `redirect_token` | 每次请求生成的一次性字符串 | 只用于协议回执，不得转发给目标 URL |

Worker 必须原样返回目标服务的状态码，尤其是 301、302、303、307、308 及其
`Location` 响应头，不得把非 2xx 状态统一改写成 502。new-api 校验下一跳 URL 后，
会按 HTTP 语义修改方法、请求体和敏感请求头，再发起下一次 Worker 请求。

Worker 在复制目标响应头后，必须覆盖设置：

```text
X-New-API-Worker-Redirect-Token: <redirect_token>
```

new-api 会校验并删除该内部回执。回执缺失或不匹配时，请求安全失败。这能识别仍在
自动跟随重定向的 v0 Worker，也防止目标服务伪造固定能力标记。

## v2 目标绑定

v2 请求增加 `target`：

```json
{
  "target": {
    "version": "2",
    "scheme": "https",
    "host": "files.example.com",
    "port": 443,
    "ips": ["203.0.113.10", "2001:db8::10"]
  }
}
```

`ips` 是 new-api 本次解析得到并全部通过 SSRF policy 的规范化、去重、有序地址集。
Worker 必须：

1. 校验 `url` 的 scheme、规范化 hostname 和有效端口与 `target` 一致。
2. 从 `target.ips` 选择一个地址直接建连，不得再次通过 `target.host` 查询 DNS。
3. HTTP 请求继续使用 `target.host` 作为 Host；HTTPS 同时使用它作为 TLS SNI，并按
   该主机名校验证书。不能把 URL 简单替换成 IP 而跳过证书校验。
4. 记录实际连接的 IP；该值必须对应 `target.ips` 中的一个字符串。
5. 不把 `target`、`redirect_token` 或证明材料转发给目标服务。

成功取得目标响应后，Worker 在复制目标响应头之后覆盖设置三个内部头：

```text
X-New-API-Worker-Redirect-Token: <redirect_token>
X-New-API-Worker-Target-IP: <target.ips 中实际连接的值>
X-New-API-Worker-Target-Proof: <proof>
```

`proof` 使用 `redirect_token` 作为 HMAC key，对以下 UTF-8 文本执行 HMAC-SHA256，
最后编码为无填充 base64url：

```text
<target.version>\n<target.scheme>\n<target.host>\n<target.port 十进制>\n<target.ips 按请求顺序用逗号连接>\n<实际连接 IP>
```

示例实现：

```js
async function createTargetProof(redirectToken, target, connectedIp) {
  const input = [
    target.version,
    target.scheme,
    target.host,
    String(target.port),
    target.ips.join(','),
    connectedIp,
  ].join('\n')
  const key = await crypto.subtle.importKey(
    'raw',
    new TextEncoder().encode(redirectToken),
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['sign']
  )
  const bytes = new Uint8Array(
    await crypto.subtle.sign('HMAC', key, new TextEncoder().encode(input))
  )
  return btoa(String.fromCharCode(...bytes))
    .replaceAll('+', '-')
    .replaceAll('/', '_')
    .replace(/=+$/, '')
}
```

以上方算法和下列输入为例：

```text
redirect_token = one-time-token
target          = {"version":"2","scheme":"https","host":"safe.example","port":443,"ips":["1.1.1.1","8.8.8.8"]}
connectedIp     = 1.1.1.1
proof           = PxgQsqWmBeh6-WFUaWJXeV4r1Y4exb4I7DdS_nTMyyE
```

HMAC 把实际 IP、主机和本次一次性 token 绑定在一起。即使目标响应伪造两个
`X-New-API-Worker-Target-*` 头，不知道 token 的目标服务也无法生成有效证明。new-api
还会独立确认回报 IP 属于本次允许集合；任一字段缺失或不匹配都会关闭响应并失败。

### Worker 运行时要求

普通的 `fetch(originUrl, { redirect: "manual" })` 会在 Worker 所在环境重新解析域名，
通常也不暴露或固定实际连接 IP，因此不能实现 v2。先查询 DNS、再调用普通 `fetch`
同样存在检查与连接之间的竞态，也不符合合同。

Worker 运行时必须提供“按指定 IP 建连，同时保留原始 Host、TLS SNI 和证书校验”的
底层能力，并返回实际连接 IP。实现可以封装为类似下面的接口：

```js
const { response, connectedIp } = await fetchBoundTarget({
  url: originUrl,
  method,
  headers,
  body,
  redirect: 'manual',
  allowedIps: target.ips,
  serverName: target.host,
  port: target.port,
})
```

这里的 `fetchBoundTarget` 不是标准 Fetch API。运行时无法提供这种能力时，收到
`target` 后必须返回明确的协议不支持错误，不得忽略 `target` 后继续请求。

## 配置快照

一次出站操作开始时，new-api 会同时捕获 `WorkerUrl`、`WorkerValidKey` 和
`WorkerAllowHttpImageRequestEnabled` 的同一配置版本。即使管理员在重定向期间更新
设置，后续跳转仍使用该操作开始时的 Worker 地址、密钥和 HTTP 许可；新配置只影响
之后发起的操作。

## 升级与兼容性

- v0 Worker 不返回一次性重定向回执且可能自动跟随跳转，无法在不削弱安全策略的
  前提下兼容；请求会失败并提示不支持手动重定向合同。
- v1 Worker 继续处理不含 `target` 的请求。收到 `target` 后不能只返回 v1 token；
  new-api 会要求完整的 v2 目标证明。
- v2 Worker 应同时实现 v1 手动重定向和 v2 目标绑定。三个内部响应头都必须在复制
  目标响应头之后使用 `set` 覆盖。
- `WorkerUrl` 必须是直接处理 POST 的最终地址，不能依赖 CDN 或应用层 3xx 跳转；
  new-api 不会自动跟随 Worker 端点自身的重定向。
- 不使用 Worker 的 direct adapter 不受协议升级影响。未配置转发代理时，其受保护
  dialer 会在建连前校验并直接连接本机解析出的允许 IP。严格域名 IP 过滤开启时，
  环境 HTTP(S) 代理无法证明出口解析与实际目标地址，new-api 会在连接代理前安全
  失败；域名 IP 过滤关闭或目标是字面 IP 时仍保留现有代理行为。

## 渠道凭据请求不跟随重定向

本文 Worker 协议只覆盖下载、Webhook 与通知；带渠道凭据的上游请求另有约束。
net/http 跨主机重定向只剥 `Authorization`/`Cookie`，`x-api-key`、`x-goog-api-key`、
`api-key`、`mj-api-secret` 及头覆盖写入的自定义鉴权头会被带到新主机，因此这类请求
一律不自动跟随：中转主链路 `relay/channel` 的 `doRequest`（含渠道测试）在缓存客户端
的浅拷贝上设 `http.ErrUseLastResponse`，上游 3xx 原样作为错误状态码交给 relay；
余额查询与模型列表拉取（`GetResponseBody`、Advanced Custom 管理路由、Gemini 模型列表）
和 Midjourney 请求经 `service.NewRedirectlessHTTPClient` 收到 3xx 即失败。视频内容代理
需要跟随到签名存储地址，改用 `service.NewCredentialStrippingRedirectClient`：仍逐跳做
SSRF 校验，离开初始源后去掉 `Authorization` 与 `x-goog-api-key`。共享缓存客户端的全局
`CheckRedirect`（SSRF 校验后跟随，最多 10 次）保持不变，供不带渠道凭据的抓取使用。
