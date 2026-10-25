# 单前端与 RelayKit 架构约束

> 定位：现行开发约束。由已完成的重构记录整理；协议增量的验证范围见[协议兼容](relay-protocol-compatibility.md)。

## 前端

唯一前端位于 `web/`，使用单一 `web/bun.lock`，生产嵌入目录为 `web/dist`。不恢复 `default/classic` 双前端选择、构建分支或 `theme.frontend` 设置。浅色/深色状态和六套配色仍属于页面外观能力。

`web/default/public/logo.png` 仅为根 README 保留品牌图片兼容路径；该目录不承载另一套前端实现。旧 `/console/*` 路径兼容与页面主题选择无关。

## 协议层与宿主分工

RelayKit 是独立 Go 模块 `eflow.local/new-api/relaykit`，宿主通过 `replace => ./relaykit` 接入。它负责 OpenAI Chat、OpenAI Responses、Claude 和 Gemini 的协议 DTO、请求转换、非流式响应转换及流式转换状态机。

HTTP、SSE 读写、Gin、鉴权、渠道调度、数据库、日志和最终计费留在宿主。模型名、渠道元数据、推理设置、安全设置和 token 估算通过 `convmeta.Meta` / `Options` 注入。转换接口为 `ConvertRequest`、`ConvertResponse`、`NewResponseStreamState`、`ConvertStreamResponseChunk` 和 `FinalizeStreamResponse`。

每条流独立维护转换状态并完成 finalize。原生 Responses 的终态和禁止重放规则见[流结算](responses-stream-accounting.md)，不能由转换层自行改写。

宿主 `dto` 对字节语义等价的协议类型保留 RelayKit type alias；宿主特有的 wire shape 与兼容适配仍由宿主负责。不恢复迁移期 JSON 往返桥或第二套 canonical DTO。Gemini `usageMetadata` 到 canonical `Usage` 的纯映射由 RelayKit 单一定义，宿主负责最终结算。

## 必须保留的兼容语义

- 可选标量的显式 `0`、`0.0`、`false`，以及字段缺失与权威零用量的区别。
- Qwen/QwQ `thinking_budget`、Claude billing header 清理、`VideoTokens`、Responses 图片生成计费辅助数据和 usage/cache 字段。
- OpenRouter 方言、Gemini safety/imagine、推理预算与显式可见性，以及 `client_metadata`、reasoning `mode/context`、`BillingUsage`。
- 请求级工具映射经过宿主快照、请求转换和响应还原；通用转换与 Ali 专用字段过滤分开。

Gemini 采用官方 Intent、renderer、validator 和 defaults；预算及可见性经内部状态保留于 Chat/Responses 多跳转换，不作为额外字段发送给供应商。宿主保留渠道映射、计费模型名及资金处理。该专题不代表全站模型修改器或其他供应商推理 renderer 已迁移完成。

## 验证入口

局部检查统一在 Docker/Linux 命名卷内执行，入口见[本地开发](local-development.md#docker-检查入口)。重构记录中的阶段基线、旧命令和回滚点用于追溯，不作为当前执行指令。真实供应商能力和部署状态按各专题的日期及验证范围判断。
