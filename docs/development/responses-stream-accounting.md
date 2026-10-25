# Responses 流终态与用量

> 定位：现行开发与行为约束。带日期的部署或验证段落保留为当时记录；分类入口见[文档导航](../README.md)。

本规则适用于使用 `OaiResponsesStreamHandler` 的原生 Responses 流转发。Responses 与 Chat/Claude 的协议转换链路不在本次变更范围内。

## 终态和结算

| 上游结果 | 本地处理 |
| --- | --- |
| `response.completed` 携带 usage | 使用上游用量，保留缓存、模态和工具信息；输入或输出明确为零时不做估算覆盖 |
| `response.incomplete` 携带 usage | 作为有效终态使用上游用量，例如输出达到限制；不等同于传输中断 |
| 有效终态省略 usage | 兼容既有兜底：从已收到的输出文本、工具调用参数、推理摘要 / 推理文本与拒答增量估算输出（没有增量时取终态 response 里的输出文本），在有输出时使用输入估算值 |
| `error`、`response.error`、`response.failed`，或畸形事件 | 返回 API 错误，不进入普通文本用量结算 |
| EOF、`[DONE]`、超时或读取异常，且尚未收到有效终态 | 返回终态缺失错误，不将少量文本估算为成功用量 |
| 客户端主动取消 | 继续关闭上游连接，并保留既有按已收到信息结算的策略 |

终态必须包含 response 对象；如包含 status，须与事件类型一致，且不能包含 error。收到有效终态即关闭上游读取，不等待额外 `[DONE]`，也不重复计入其后的事件。

失败会回到现有控制器的重试和预扣退款流程；最终失败时退回预扣额度，既有独立违规费用策略仍适用。这不代表上游一定没有成本，也不会自动退还历史账单。

流已写入客户端后，即使渠道配置了强制重试，也不再重放请求。原生 Responses 在已开始的 SSE 中以 `event: error` 输出错误；未写入响应时使用 HTTP JSON 错误，默认状态码 502，仍受既有状态码映射及错误过滤规则影响。

## SSE 兼容

共享 `StreamScannerHandler` 可组装同一事件中多个 `data:` 字段承载的 JSON，支持 CRLF 和末尾无空行的 EOF。完整单行 JSON 和既有非 JSON 逐行负载仍立即分发，无需新增空行；它不是面向任意多行文本的通用 SSE 解析器。多行 JSON 在空行、`[DONE]` 或 EOF 时分发。

组装后事件受现有 `STREAM_SCANNER_MAX_BUFFER_MB` 上限限制，避免通过大量短行绕过大小限制。心跳注释不延长有效数据空闲超时。原生 Responses 输出为合法 SSE 多行 data 帧，并向调用者返回写入失败。

## 排查边界

0 t/s、0/1 Token 或 EOF 本身不能证明漏单或多扣。需要结合有效终态、最终 usage、链路中断位置、请求 ID 和真实余额流水核对。以上修复有本地合成流测试证明，但不能据此确认某条历史请求的根因；没有原始响应时仍需上线后的受控观察或上游证据。

针对性验证：

以下局部检查从仓库根目录用 Git Bash 执行；专用测试 MySQL 的启停按项目授权规则处理，详见[Docker 检查入口](../development/local-development.md#docker-检查入口)。

```shell
scripts/docker-check.sh go-test ./relay/channel/openai ./relay/helper -count=1 -timeout=120s
scripts/docker-check.sh go-test ./controller -run "'TestResponsesRelayError|TestShouldRetryCommittedStream'" -count=1 -timeout=120s
```
