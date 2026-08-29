# io.net 模型部署集成

new-api 通过 `pkg/ionet` 客户端和管理员接口管理 io.net 部署。本文档描述当前集成，不是针对某个固定集群的抓包记录。

## 配置

在“系统设置 → 集成 → io.net 模型部署”维护：

| option key | 说明 |
| --- | --- |
| `model_deployment.ionet.enabled` | 是否启用 io.net 部署管理。 |
| `model_deployment.ionet.api_key` | io.net API Key。后端只向前端返回是否已配置，不回显密钥。 |

启用且 API Key 非空后，管理员可以测试连接。连接测试使用 Enterprise API 查询可用硬件；失败时返回 io.net 的结构化错误消息。

## API 边界

所有路由位于 `/api/deployments`，并经过 `AdminAuth`：

- 查询、搜索、创建、更新、延长和删除部署；
- 修改部署名称并检查名称可用性；
- 查询硬件类型、位置、可用副本和价格估算；
- 查询部署详情、容器列表、容器详情和日志；
- 查询配置状态和测试连接。

部署和容器 ID 始终来自请求路径或 io.net 响应，代码中不内置固定集群 ID。

## 上游地址

客户端默认使用：

- 普通 API：`https://api.io.solutions/v1/io-cloud/caas`
- Enterprise API：`https://api.io.solutions/enterprise/v1/io-cloud/caas`

上游调用、请求/响应类型和错误解析分别位于 `pkg/ionet/client.go`、`deployment.go`、`hardware.go`、`container.go` 和 `types.go`。控制器只负责鉴权、参数校验和将上游对象映射为 new-api 的管理接口响应。
