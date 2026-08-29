# 渠道额外设置说明

渠道模型包含两个不同的 JSON 字段，不能混用：

- `setting`：请求转发与网络行为，对应 `relaykit/dto.ChannelSettings`。
- `settings`：渠道类型能力、上游模型同步和 Custom 路由，对应 `dto.ChannelOtherSettings`。

后台渠道表单会负责读写这些字段。直接调用 API 时，字段值都是 JSON 字符串。

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
| `aws_key_type` | AWS 凭据类型：`ak_sk` 或 `api_key`。 |
| `openrouter_enterprise` | 启用 OpenRouter Enterprise 行为。 |
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
