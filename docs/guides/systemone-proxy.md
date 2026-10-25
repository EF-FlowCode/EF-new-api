# SystemOne 独立中间件

> 定位：现行使用与配置指南。带日期的部署或验证段落保留为当时记录；分类入口见[文档导航](../README.md)。

`cmd/systemone-proxy` 为 Jev / TypeSafe 提供 `POST /v1/systemone`。它是独立进程，生产代码只依赖 Go 标准库，不改 New API 核心、数据库、定价结构或前端。

```text
客户端 state + questions（New API 用户密钥）
  → 中间件公共入口 :8091/v1/systemone
  → New API /v1/chat/completions：鉴权、权限、分组、渠道选择、限流、模型映射、计费
  → 中间件内部入口 :8092/<target>/v1/chat/completions（渠道上游密钥）
  → 配置的完整原生上游端点
```

原生载荷通过签名消息往返；响应从 Chat 的 `message.content` 还原，用户不会看到 `messages` / `choices` 包装。原生响应字节保留，包括 `answers`、`probabilities`、`confidence`、`legend`、模型名及扩展字段。网关可开启“强制响应格式”。

## 配置

参考 [非敏感配置示例](../../examples/systemone-proxy.json)。所有 `endpoint` 字段均填写**完整接口 URL**，程序不拼接 `/v1`。`gateway_chat_endpoint` 必须直连 New API 的 Chat 接口，不能指回中间件公共入口。

| 目标 | protocol | 完整 endpoint |
| --- | --- | --- |
| 第三方 SystemOne 供应商 | `typesafe` | `https://systemone-provider.example/v1/systemone` |
| TypeSafe 官方 | `typesafe` | `https://api.typesafe.ai/v1/systemone` |
| OpenRouter Decisions | `openrouter` | `https://openrouter.ai/api/alpha/decisions` |

三者支持的 `state + questions` 公共子集使用原生结构；OpenRouter 使用自己的 Decisions 路径，不将它当作 Chat 接口或仅替换域名。支持自定义完整 endpoint；目标只由管理员配置，客户端不能传 URL。

### 备用 endpoint（`fallback_endpoints`）

同一目标可配置最多 3 个备用完整 endpoint，用于同一供应商、同一密钥的不同入口（例如经 Cloudflare 的域名与直连源站域名）。主 endpoint 失败后，只在**能确定请求未到达供应商源站**时按顺序换下一个，避免同一决策被执行或计费两次：

| 情况 | 是否换下一个 |
| --- | --- |
| DNS / TCP / TLS 失败，请求头尚未写出 | 换 |
| 响应带 `cf-mitigated` 头（Cloudflare 质询） | 换 |
| `Server: cloudflare` 的 HTML 页面，状态 403 / 429 / 503 / 521 / 522 / 523 / 525 / 526 / 530 | 换 |
| 供应商返回的 JSON 业务错误（400 / 401 / 422 / 429 / 5xx 等） | 不换，按原规则返回 |
| Cloudflare 520 / 524、请求写出后的超时或断开 | 不换：源站可能已处理 |

所有 endpoint 都被 Cloudflare 边缘拦截时返回 502，`message` 为 `SystemOne typesafe upstream was blocked at the Cloudflare edge`，在 New API 错误日志里可与供应商业务错误区分。每次切换在容器日志记一行 `SystemOne target <名称>: <原因> via <域名>`，不含密钥和请求内容。连接与 TLS 握手各限 10 秒，单次供应商调用总超时仍为 120 秒。对外请求的 User-Agent 固定为 `eflow-systemone-proxy/1`，不使用 Go 默认值。

独立进程通过下节 Docker 镜像运行；构建和服务启停须已有授权。不在 Windows 宿主机临时 `go run`。

### 独立 Docker 镜像与本地接入

`Dockerfile.systemone` 只构建中间件，不构建前端或 New API 主程序；运行层为非 root、只读的最小镜像。已有开发环境启动后，在仓库根目录执行：

```powershell
docker build -f Dockerfile.systemone -t new-api-systemone:20260919-local -t new-api-systemone:local .
$env:SYSTEMONE_IMAGE='new-api-systemone:20260919-local'
docker compose -f docker-compose.systemone.yml up -d --no-build
```

配置为 [Docker 配置示例](../../examples/systemone-proxy.docker.json)，连接已有 `new-api_dev-network` 网络。公共入口只发布到 `http://127.0.0.1:8091/v1/systemone`；内部端口 8092 不映射到宿主机。此 Compose 不创建或替换 New API / MySQL / Redis。现有本地网关的 `localhost:3000/v1/systemone` 没有新增路由，调用使用 8091；同域路径转发需要在外层反向代理另行配置。

该容器中的供应商渠道 Base URL 填 `http://new-api-systemone-proxy:8092/provider`。新建渠道后，现有网关的路由缓存可能需要等待一次同步（本地约一分钟）；创建 API 成功不代表已经进入当前路由快照，无需因此重启网关。

在 New API 现有渠道面板配置：

1. 类型选择 **OpenAI**，即使真实供应商是 OpenRouter；Chat 包装由中间件处理。
2. Base URL 填中间件内部地址，如 `http://127.0.0.1:8092/provider`。这里是 **Base URL**，不要追加 `/v1/chat/completions`。
3. 渠道密钥填写实际供应商密钥；中间件配置文件不放任何密钥。
4. 添加 `jev-1.13` 或对外别名，使用现有模型映射。价格及分组倍率继续在现有定价入口自行配置。
5. 如配置入口白名单，选择 Chat（`openai`）。关闭全局及渠道“请求体透传”，保证现有 Chat 模型映射能写入请求。该渠道不要启用 Chat → Responses 转换或覆盖 `messages` / `stream`。

Docker 中的 `127.0.0.1` 指容器自身。跨容器应使用服务名；New API 在 Docker、中间件在 Windows 宿主机时，可使用 `http://host.docker.internal:8092/provider`，同时把内部监听绑定到容器可达地址，并通过防火墙仅允许网关访问。

反向代理只把外部的 **精确路径** `/v1/systemone` 转发到公共入口；其他路径继续送往原有 New API。内部端口不能暴露到公网。两入口必须到达同一进程，因为签名密钥每次启动随机生成；当前不支持将两侧分别轮询到不同副本。停止时先排空公共入口，再关闭内部入口。

## 调用

调用使用 **New API 用户令牌**，不是供应商密钥。请求示例：

```json
{
  "model": "jev-1.13",
  "state": "Please refund the duplicate charge.",
  "questions": {
    "refund_requested": {
      "type": "noul",
      "instructions": "Does the customer explicitly request a refund?"
    }
  }
}
```

`state` 和 `instructions` 支持字符串、对象或数组。`questions` 是非空对象，支持：

- `noul`：可不传 `criteria`；传入时为 `true` / `false` 的字符串描述。
- `choice`：`criteria` 为非空对象，如 `{"billing":"Billing questions","other":null}`。
- `score`：`criteria` 为至少两级字符串数组，如 `["low","high"]`。

仅支持非流式。保留额外原生字段，但上游是否接受这些字段取决于供应商。

## 用量与边界

分别归一化输入、输出，不累加两套字段：优先非零 `input_tokens` / `output_tokens`；原生字段为零、兼容字段非零时回退到 `prompt_tokens` / `completion_tokens`。两套均非零但不同，以原生字段为准。原生响应中的 usage 不改，网关计费用的是单独归一化后的 Chat usage。

例如 `prompt_tokens=14,input_tokens=0,completion_tokens=3,output_tokens=0`，按输入 14、输出 3 结算，不计入传输包装。网关日志仍记录 `/v1/chat/completions` 和对外模型名；价格、分组、重试及额度生命周期均沿用现有网关。

**当前 Chat 在输入为零时会自行估算，无法无侵入地保留权威零输入。** 因此输入归一化为零、任一维度缺失、用量非法或响应没有有效 `answers` 时，中间件向网关返回 502，进入现有失败退款流程，不返回可能被错误计费的成功结果。输出显式为零、输入非零可以正常返回。供应商可能已经对这些失败响应计费，网关退款不代表供应商退款。

中间件不自行重试。网关原有重试配置仍生效，可能重新调用供应商；需要避免重复决策时使用现有网关的重试设置。Chat 先写响应再结算的时序和结算失败语义也不改变，不保证客户端收到 200 时结算已经落库。

错误沿用 `{"error":{"type":...,"code":...,"message":...}}`：本地参数 400、缺少鉴权 401；网关的无权限 403、无渠道 503 等直接传回。上游非 200 保留 HTTP 状态并标记 `upstream_error`，包括 401、422、429、529；不回显可能含敏感数据的供应商原始错误正文。不跟随任何带凭据请求的重定向。

公共入口校验 Bearer 格式，实际账户验证和限流由 New API 完成。网关看到的是中间件源 IP；需要按真实客户端 IP 应用策略时，应单独配置可信代理链，不能无条件相信客户端自报头。原生请求/响应上限各 8 MiB，HTTP 单次调用超时 120 秒。协议校验不等于业务结果可信性验证。

模型广场、端点筛选和面板内调用示例继续显示 Chat，不增加 SystemOne 原生 UI。调用方使用本文的原生示例。中间件不创建渠道、不修改价格，也不自动部署。

## 局部验证

以下局部检查从仓库根目录用 Git Bash 执行；专用测试 MySQL 的启停按项目授权规则处理，详见[Docker 检查入口](../development/local-development.md#docker-检查入口)。

```bash
# Docker 入口自动注入专用测试 DSN，各用例创建随机隔离库。
scripts/docker-check.sh go-test ./internal/systemoneproxy ./cmd/systemone-proxy -run "'Test(SystemOne|Usage|Complete|Proxy|Gateway)'" -count=1
```

真实冒烟用例为 `TestLiveSystemOne`，仅在测试容器环境同时存在 `SYSTEMONE_LIVE_KEY` 与 `SYSTEMONE_LIVE_ENDPOINT` 时执行，对指定端点调用两次。真实密钥只在内存注入，测试数据库存占位密钥，不进入代码、配置、日志或命令行参数；普通 `go-test` 入口不转发该密钥。

协议参考：[TypeSafe API](https://docs.typesafe.ai/api)、[OpenRouter Decisions](https://openrouter.ai/docs/api/api-reference/alphadecisions/submit-a-decisions-questions-and-answers-request.md)。
