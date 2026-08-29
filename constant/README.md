# constant 包（`/constant`）

该目录存放跨包共享的常量、静态映射，以及不依赖项目内其他包的轻量查询辅助。

## 当前文件

| 文件 | 说明 |
| --- | --- |
| `api_type.go` | API 类型常量与名称映射。 |
| `azure.go` | Azure 兼容行为相关常量。 |
| `cache_key.go` | 缓存键格式和 Token 字段常量。 |
| `channel.go` | 渠道类型、能力和流式支持等静态映射与查询辅助。 |
| `context_key.go` | Gin 上下文键类型与常量。 |
| `email_template.go` | 邮件模板类型、默认模板及构造辅助。 |
| `endpoint_type.go` | Relay 端点类型常量。 |
| `env.go` | 由启动环境初始化的全局运行参数。 |
| `finish_reason.go` | OpenAI/GPT `finish_reason` 常量。 |
| `midjourney.go` | Midjourney 错误码、动作和模型映射。 |
| `multi_key_mode.go` | 多 Key 调度模式常量。 |
| `setup.go` | 系统是否已完成初始化的状态。 |
| `task.go` | 异步任务平台、动作和模型映射。 |
| `waffo_pay_method.go` | Waffo 支付方式常量与映射。 |

## 使用约定

1. `constant` 包禁止引用项目内其他自定义包；需要依赖时仅使用 Go 标准库。
2. 不在此目录实现业务流程、数据库操作或第三方服务调用。
3. 轻量查询辅助应只基于本包静态数据，不产生外部副作用。
4. 新增或删除文件时同步维护本清单。
