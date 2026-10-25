# 渠道额外设置说明

> 定位：现行使用与配置指南。带日期的部署或验证段落保留为当时记录；分类入口见[文档导航](../README.md)。

渠道模型包含两个不同的 JSON 字段，不能混用：

- `setting`：请求转发与网络行为，对应 `relaykit/dto.ChannelSettings`。
- `settings`：渠道类型能力、上游模型同步和 Custom 路由，对应 `dto.ChannelOtherSettings`。

后台渠道表单会负责读写这些字段。直接调用 API 时，字段值都是 JSON 字符串。

Jev / TypeSafe 可通过 [SystemOne 独立中间件](systemone-proxy.md) 接入普通 OpenAI 渠道，沿用本文现有字段，不新增 `systemone` 设置键。

## 已有多 Key 渠道的选择策略

编辑已有多 Key 渠道时，可直接选择随机或轮询策略。保存使用顶层 `multi_key_mode`
（`random` / `polling`），无需解锁或重填密钥；单 Key 渠道不发送此字段。
此操作保留渠道原有的 Key 状态、禁用记录和轮询游标，不上传整个 `channel_info` 覆盖运行状态。

## HTTP 连接配置

上游响应头等待默认最多 1800 秒，收到响应头后的响应体与流式生成不受该计时器限制。
配置优先读取 `RELAY_RESPONSE_HEADER_TIMEOUT`；未设置新变量时兼容旧 `RESPONSE_HEADER_TIMEOUT`。
显式 `0` 或负数表示关闭，非法值回落 1800，正值转换为时长前做上限钳制，避免整数溢出。
更改环境变量需要重启对应应用进程后生效。开启跳过 TLS 验证时，各 HTTP 传输对象使用独立配置副本。

## `setting`

| 键 | 类型与默认值 | 行为 |
| --- | --- | --- |
| `force_format` | `boolean`, `false` | 强制按 OpenAI 兼容格式处理。 |
| `thinking_to_content` | `boolean`, `false` | 将 `reasoning_content` 以 `<think>` 内容返回。 |
| `proxy` | `string` | 上游代理，支持 `http`、`https`、`socks5`、`socks5h`；地址不能包含 path、query 或 fragment。 |
| `pass_through_body_enabled` | `boolean`, `false` | 保留请求体中默认会被安全过滤的字段。启用前应评估额外计费和隐私风险。 |
| `system_prompt` | `string` | 渠道级系统提示词。 |
| `system_prompt_override` | `boolean`, `false` | 以渠道系统提示词覆盖而不是追加。 |
| `http_protocol` | `auto` / `http1`, 默认 `auto` | 自动协商 HTTP/1.1、HTTP/2，或强制 HTTP/1.1。 |
| `http2_connection_shards` | `0` 到 `8`, 默认 `0` | `0` 或未设置等价于单连接池；显式分片数为 `1` 到 `8`。`http1` 下不得大于 `1`。 |

## `settings`

| 键 | 用途 |
| --- | --- |
| `azure_responses_version` | 覆盖 Azure Responses API version。 |
| `vertex_key_type` | Vertex 凭据类型：`json` 或 `api_key`。 |
| `vertex_project_id` | 仅 `vertex_key_type=api_key` 时使用的 GCP 项目 ID，可选。不填时请求走 express-mode 地址（路径不含项目与区域）；填写后请求带 `/projects/<id>/locations/<区域>`，区域取渠道“部署区域”配置。JSON 模式仍从 service account 解析项目。 |
| `aws_key_type` | AWS 凭据类型：`ak_sk` 或 `api_key`。 |
| `aws_api_type` | AWS 接口：`invoke_model`（默认，留空同此）或 `mantle`。`mantle` 走 Bedrock Mantle 原生 Messages API（`https://bedrock-mantle.<区域>.api.aws/anthropic/v1/messages`，自定义 Base URL 须为 HTTPS），密钥写成“API Key 与区域”两段时发 `x-api-key`，写成“AccessKey、SecretAccessKey、区域”三段时用 SigV4 签名（分隔符同 InvokeModel）；只支持 Claude 模型，不做 InvokeModel 的跨区域前缀，Nova 请继续用 `invoke_model`。 |
| `openrouter_enterprise` | 启用 OpenRouter Enterprise 行为。 |
| `supported_endpoint_types` | 可选的请求入口手动覆盖，只对普通渠道生效。配置后替代按渠道类型自动推导的入口；为空、未设置或只包含未知值时使用自动推导。可用值见下方示例。 |
| `claude_beta_query` | Claude 请求强制追加 `?beta=true`。 |
| `allow_service_tier` | 允许透传 `service_tier`。 |
| `allow_inference_geo` | 允许 Claude 透传 `inference_geo`。 |
| `allow_speed` | 允许 Claude 透传 `speed`。 |
| `allow_safety_identifier` | 允许透传 `safety_identifier`。 |
| `disable_store` | 禁止透传 `store`。 |
| `allow_include_obfuscation` | 允许透传 `stream_options.include_obfuscation`。 |
| `ratio_sync_access_token` | 上游价格同步接口的访问令牌。 |
| `ratio_sync_user_id` | 价格同步请求的 `New-Api-User`。 |
| `upstream_model_update_check_enabled` | 启用上游模型变更检测。 |
| `upstream_model_update_auto_sync_enabled` | 自动应用检测到的模型变更。 |
| `upstream_model_update_ignored_models` | 忽略的上游模型列表。 |
| `advanced_custom` | Custom 渠道的转发、协议转换、模型列表与余额路由。 |

以下字段由系统维护，不应由普通保存请求手工伪造：`upstream_model_update_last_check_time`、`upstream_model_update_last_detected_models`、`upstream_model_update_last_removed_models`。

### 请求入口能力

选择渠道时会先按请求入口过滤，只保留能处理该入口的渠道，避免请求分到转换不了的渠道后直接失败（转换失败不重试）：

- 普通渠道：按渠道类型自动推导（`constant/channel_endpoint_capability.go`）。例如 Anthropic、AWS 只接 Chat Completions 和 Messages，不接 Responses；Gemini、Vertex 不接 Responses；Codex 只接 Responses（命中全局 Chat→Responses 策略时也接 Chat 和 Messages）。表里没有的类型（OpenAI 等）接受所有入口。
- Custom（8）且配置了转发路由：按 `advanced_custom.advanced_routes` 匹配；没有转发路由时接受所有入口。
- vLLM（62）、SGLang（63）：按内置路由预设。
- `supported_endpoint_types`：普通渠道的手动覆盖，用于上游支持的入口比渠道类型少的情况（如上游只开放 Responses 的 OpenAI 类型渠道）；Custom、vLLM、SGLang 渠道忽略此字段，前端保存时也会去掉。

自动推导只覆盖 Chat Completions、Responses、Responses Compact、Messages、Gemini、Rerank、图片生成和 Embeddings；音频、视频、实时等其他入口不过滤。

```json
{
  "supported_endpoint_types": ["openai-response", "anthropic"]
}
```

可用值包括：`openai`（Chat Completions/Completions）、`openai-response`（Responses）、`openai-response-compact`、`anthropic`（Messages）、`gemini`、`jina-rerank`、`image-generation`、`embeddings` 和 `openai-alpha-search`。

## Custom 路由

`advanced_custom.advanced_routes` 至少包含一条路由。每条路由支持：

- `incoming_path`：必须以 `/` 开头，支持 Gemini `{model}` 模板。
- `upstream_path`：以 `/` 开头的相对路径，或完整 `http://` / `https://` URL。
- `models`：可选模型列表；空列表是同一路径的 catch-all，需放在最后；`re:` 前缀表示正则。
- `converter`：`none` 或已注册的 OpenAI Chat、OpenAI Responses、Claude Messages、Gemini Generate Content 转换器。
- `auth`：默认发送 `Authorization: Bearer <api_key>`；也可用 `none`、`header` 或 `query`，值中的 `{api_key}` 会替换为渠道密钥。

管理路由必须精确配置：`/v1/models` 用于拉取模型列表，`/v1/dashboard/billing/credit_grants` 用于查询余额。

```json
{
  "advanced_custom": {
    "advanced_routes": [
      {
        "incoming_path": "/v1/chat/completions",
        "upstream_path": "/v1/chat/completions",
        "converter": "none",
        "models": ["gpt-4o"],
        "auth": {
          "type": "header",
          "name": "Authorization",
          "value": "Bearer {api_key}"
        }
      }
    ]
  }
}
```

### vLLM / SGLang 预置路由

vLLM（类型 62）与 SGLang（类型 63）沿用上游编号，按固定的 Custom 路由转发，读取渠道时总是用预置路由替换 `advanced_custom` 中保存的内容，手工配置不会生效。预置路由把 `/v1/chat/completions`、`/v1/completions`、`/v1/responses`、`/v1/embeddings`、`/v1/messages` 原样转发到同名上游路径，`/v1/models` 用于拉取模型列表，均以 `Authorization: Bearer <api_key>` 鉴权（未开启鉴权的服务可填 `EMPTY`）。这两类渠道没有内置地址，新建时必须填写 API 地址（不带 `/v1`）。SGLang 的 `/v1/rerank` 需要响应转换，暂未提供；上游的推理服务状态面板也未移植。
