# 文档导航

这里收录 eflow 分支的使用指南、实现约束和接口参考。先按任务选择入口。

## 使用和配置

| 我想做什么 | 文档 |
| --- | --- |
| 配置模型价格、时间规则和展示单位 | [模型定价](guides/model-pricing.md) |
| 配置向量、Rerank 和 Batch 价格 | [知识库定价](guides/knowledge-base-pricing.md) |
| 使用 Embedding 批处理文件和任务 | [Embedding Batch File](guides/knowledge-batch.md) |
| 管理客户等级、令牌分组和充值优惠 | [客户等级](guides/customer-tiers.md) |
| 设置额度、推荐奖励 | [额度设置](guides/quota-settings.md) |
| 配置用户请求次数与成功次数限制 | [请求次数限流](guides/model-request-rate-limit.md) |
| 限制网页分组的地区访问 | [分组地区访问控制](guides/group-region-access.md) |
| 配置渠道额外参数 | [渠道额外设置](guides/channel-other-settings.md) |
| 接入 io.net | [io.net 客户端](guides/ionet-client.md) |
| 使用 AQBot 聊天预设 | [聊天客户端预设](guides/chat-clients.md) |
| 接入 Jev / TypeSafe | [SystemOne 中间件](guides/systemone-proxy.md) |
| 配置邮件模板及排查投递 | [邮件投递](guides/email-delivery.md) |
| 处理开票申请 | [开票申请](guides/invoice-requests.md) |
| 核对并返还消费余额 | [批量返还](guides/batch-usage-refund.md) |

## 开发和行为约束

| 我想了解什么 | 文档 |
| --- | --- |
| 开发环境、Docker 检查、计费编辑器局部验收 | [本地开发](development/local-development.md) |
| 单前端及 RelayKit 的职责边界 | [架构约束](development/architecture.md) |
| Chat / Responses / Claude / Gemini 转换兼容范围 | [协议兼容](development/relay-protocol-compatibility.md) |
| 图片数量校验及流式透传 | [图片请求与流式响应](development/image-streaming.md) |
| 原生 Responses 终态、用量与结算 | [Responses 流结算](development/responses-stream-accounting.md) |
| 日志权限、查询和性能 | [日志查询](development/log-query-performance.md) |
| 延迟、吞吐量等指标的计算口径 | [性能指标](development/performance-metrics.md) |
| worker 出站请求边界 | [出站协议](development/worker-outbound-contract.md) |
| 翻译用词一致性 | [通用术语](development/translation-glossary.md)、[法语](development/translation-glossary.fr.md)、[俄语](development/translation-glossary.ru.md) |

## 安装

| 我想做什么 | 文档 |
| --- | --- |
| 参考上游宝塔面板安装教程 | [宝塔安装](operations/bt-installation.md) |

宝塔教程是上游通用参考。

## Task Plugin 和接口参考

从 [Task Plugin 总览](plugin-api/README.md) 开始；本地能力与上游协议参考分别查阅。

| 内容 | 文档 |
| --- | --- |
| Root 管理、版本、开关和绑定 | [管理接口](plugin-api/management.md) |
| 未知受理、幂等和人工核对 | [恢复合同](plugin-api/recovery.md) |
| 持久身份、源码快照和查询准备 | [任务身份](plugin-api/task-identity.md) |
| 固定上游 v1 协议参考 | [协议文档](plugin-api/v1.md)、[类型定义](plugin-api/v1.d.ts)、[schema](plugin-api/v1.schema.json) |
| OpenAPI 文件 | [管理 API](openapi/api.json)、[Relay API](openapi/relay.json) |

协议声明和 OpenAPI 文件不代替本地实现及验收范围。`plugin-api/v1.*`、`openapi/` 和 [图片资源](images/) 保持原路径，便于代码注释、工具和根 README 引用。

新增文档放入对应用途子目录，并更新本索引。
