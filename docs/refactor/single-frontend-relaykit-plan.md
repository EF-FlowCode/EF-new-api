# 单前端与 RelayKit 重构计划

## 1. 目标

本次重构分为两个互不混杂的阶段：

1. 将现有 `web/default` 现代前端提升为唯一的 `web/` 前端，删除 `default/classic` 双前端选择、构建分支和运行时配置。
2. 引入官方 RelayKit，将 OpenAI Chat、OpenAI Responses、Claude、Gemini 的协议 DTO 与转换逻辑收敛到独立模块。

前端内部的浅色/深色状态和六套配色属于页面外观能力，不是双前端机制，本次保留。

## 2. 当前基线

- 工作分支：`eflow`
- 宿主基线：`93b6a8575`
- 官方 RelayKit 基线：`upstream/main@66ee6b8f9`
- 官方功能跟踪基线：`upstream/main@9724ef1b2`
- RelayKit 模块路径：`github.com/QuantumNous/new-api/relaykit`
- CodeGraph 已初始化，索引目录 `.codegraph/` 已加入 `.gitignore`

现状确认：

- `web/default` 是唯一完整前端，`web/classic` 只剩构建配置和两个图片文件。
- 后端 `GetTheme()` 已固定返回 `default`，主题选择链只剩兼容空壳。
- 现有协议转换散落在 `service/convert.go`、`service/openaicompat/` 和各渠道 adapter 中。
- HTTP、SSE、Gin、渠道调度、鉴权、计费和数据库逻辑不进入 RelayKit。

## 2.1 执行进度

- [x] 单前端扁平化（提交 `06042bb55`）。
- [x] 引入官方 RelayKit、建立本地 module 接线并合并 eflow DTO 行为。
- [x] 迁移 OpenAI Chat 与 OpenAI Responses 请求和非流式响应。
- [x] 迁移 Claude 与 Gemini 请求和非流式响应。
- [x] 迁移流式转换、删除旧协议转换层。

RelayKit 引入阶段已通过模块边界测试与 `GOWORK=off go test ./...`；宿主调用尚未切换，因此当前提交可独立回滚。

OpenAI 阶段已删除 `service/openaicompat` 中重复的请求和非流式响应转换实现；迁移期间通过单一 JSON 边界桥接宿主 DTO，最终统一 DTO import 时删除该桥接。

Claude/Gemini 阶段已将请求与非流式响应切换到 RelayKit，并删除宿主中约 2200 行重复转换代码。宿主边界测试覆盖显式零值、PDF/文本文件、Claude 默认 `max_tokens`、OpenRouter 方言、Gemini safety/imagine/thinking 与 usage/cache/`VideoTokens`。

流式阶段已将 OpenAI、Claude、Gemini 与 AWS Claude 的转换统一到 RelayKit 状态机，并删除宿主旧 `service/convert.go` 与四组重复流式转换函数。SSE 读写、计费统计和渠道错误处理继续留在宿主；流式边界测试覆盖单次 `message_start`、终止事件幂等及 OpenAI 独立 usage 块语义。

### 2.2 官方功能同步进度（2026-07-31）

- [x] 支持 zstd 请求体解压，并继续限制解压后的请求体大小。
- [x] 创建同名模型时保留已有定价，避免空表单删除现有价格。
- [x] 渠道优先级和权重编辑合并连续变更，并在失焦时立即提交。
- [x] 补齐渠道代理 URL 校验、HTTP/HTTPS/SOCKS5/SOCKS5H 支持、HTTP/1.1 模式和 HTTP/2 连接分片配置。
- [x] 补齐 Tencent TokenHub Key 提示和 OIDC 自定义显示名链路。
- [x] 无限额度 API Key 可查看已用额度。
- [x] 充值金额允许清空，并按支付方式与全局限制中的较高值校验最低充值额。
- [x] 新增“未设置价格的模型”页签，只列出已启用且没有固定价、倍率或阶梯表达式的模型。
- [x] 以原生 `Textarea` 实现轻量 JSON 编辑器，并接入现有 JSON 配置入口；未新增编辑器依赖。
- [x] 支持 DeepSeek Responses API，并处理 DeepSeek V4 `-none` / `-max` 推理后缀。
- [x] 普通日志所有者可查看异常流式状态，同时继续隐藏管理员调试字段。
- [x] OAuth 回调只在 provider/state 标记匹配且 opener 存活时识别为绑定流程，避免外部页面打开的登录标签页误判。
- [x] 核对 New API 渠道 multipart 图片编辑修复；本地渠道直接复用 OpenAI adapter，已具备等价行为，无需恢复旧 adapter。

首批已通过前端 `bun run typecheck`、4 个局部测试、zstd 中间件局部测试和 `git diff --check`。第二批已通过 DeepSeek、日志字段过滤和 OAuth 回调局部测试、前端 typecheck 与定向 ESLint。全量前端构建与根模块全量测试仍按最终阶段单独确认后执行。

## 3. 阶段一：单前端扁平化

### 3.1 文件结构

- 将 `web/default` 中的源码、配置、脚本和静态资源移动到 `web/`。
- 用现代前端的 `package.json` 替换 workspace 清单，`web/bun.lock` 成为唯一锁文件。
- 删除 `web/classic` 及 classic/default 同步专用工具。
- README 中受项目规则保护的品牌图片路径不修改；仅保留 `web/default/public/logo.png` 兼容资产，不保留代码、依赖或构建逻辑。

### 3.2 构建与运行时

- Go embed 从 `web/default/dist` 改为 `web/dist`。
- `ThemeAssets` 收敛为单一 Web 资产结构。
- Dockerfile、开发镜像、Makefile、部署脚本和 CI 的前端目录统一为 `web/`。
- 删除 `theme.frontend` 的注册、同步、接口校验和系统设置表单。
- 将 `ThemeAwarePath` 改为无条件的旧控制台路径兼容函数，保留 `/console/*` 到现代路由的映射。

### 3.3 文档与项目工具

- 更新构建、依赖清单、i18n 和 shadcn 项目路径说明。
- 删除已经失去用途的 classic-to-default 同步 skill。
- 不修改或删除受保护的项目名、组织名、版权、许可证和品牌归属信息。

### 3.4 验收

- 除 README 品牌图片兼容资产外，不再存在 `web/default` 或 `web/classic` 前端实现。
- 不再存在 `theme.frontend` 或后端前端主题切换状态。
- `bun run typecheck` 通过。
- Go embed 与路由相关包完成局部编译检查。
- `git diff --check` 通过。

阶段一作为独立提交，确认可回滚后再进入 RelayKit。

## 4. 阶段二：RelayKit 迁移

### 4.1 模块引入

- 从官方基线引入完整 `relaykit/` 独立 module。
- 根 `go.mod` 使用 `require v0.0.0` 与本地 `replace => ./relaykit`。
- 先验证 RelayKit 自身不反向依赖宿主模块。

### 4.2 eflow DTO 差异合并

必须保留以下本地语义：

- optional scalar 字段的显式 `0`、`0.0`、`false` 指针语义。
- Qwen/QwQ `thinking_budget` 透传。
- Claude billing header 清理。
- `VideoTokens`、Responses 图片生成计费辅助数据和现有 usage/cache 字段。
- OpenRouter 特殊方言、Gemini safety/imagine 设置和推理适配选项。

Gin context、日志、网络媒体下载和计费结算继续由宿主 adapter 提供，不能进入独立 RelayKit。

### 4.3 调用路径迁移顺序

1. OpenAI Chat 与 OpenAI Responses 的请求和非流式响应。
2. Claude Messages 的请求和非流式响应。
3. Gemini `generateContent` 的请求和非流式响应。
4. 各协议流式响应，逐流创建状态，并保证调用 `FinalizeStreamResponse`。
5. 删除已被替代的旧转换函数、重复 DTO 和临时 alias。

临时 type alias 只用于保持迁移期间可编译，每完成一个协议就删除对应旧入口，避免长期形成双转换层。

### 4.4 RelayKit seam

RelayKit 对宿主只暴露以下小接口：

- `ConvertRequest`
- `ConvertResponse`
- `NewResponseStreamState`
- `ConvertStreamResponseChunk`
- `FinalizeStreamResponse`

模型名、渠道元数据、推理设置、安全设置和 token 估算通过 `convmeta.Meta` / `Options` 注入。SSE 读取和写入仍由现有 relay helper 完成。

### 4.5 验收

- RelayKit 可在 `GOWORK=off` 下独立构建和执行局部测试。
- 四种协议的请求、非流式响应和流式响应保持现有 eflow 行为。
- 显式零值、工具调用、多模态、推理内容、usage 与结束事件有针对性测试。
- 不存在宿主包反向导入 RelayKit 内部实现的循环依赖。
- 每个迁移阶段通过 `git diff --check` 和受影响包编译检查。

## 5. 风险与回滚点

| 风险 | 控制方式 | 回滚点 |
| --- | --- | --- |
| 大量前端文件移动导致路径遗漏 | 全仓路径扫描、Bun typecheck、Go embed 检查 | 单前端独立提交 |
| 锁文件因 workspace 变化漂移 | 仅保留一份 Bun 锁文件并由 Bun 重建 | 单前端提交前 |
| DTO 字段丢失导致上游语义变化 | 先对比本地与官方 DTO，再切换调用方 | RelayKit 模块引入提交 |
| 流式终止事件或 usage 丢失 | 每条流强制 finalize，并增加针对性测试 | 各协议流式迁移提交 |
| 计费行为被协议层误吸收 | 计费继续留在宿主，RelayKit 只返回统一 usage | 每个协议提交 |

## 6. 提交边界

1. `重构前端为单一应用结构`
2. `引入 RelayKit 协议转换模块`
3. `迁移 OpenAI 协议转换到 RelayKit`
4. `迁移 Claude 和 Gemini 协议转换到 RelayKit`
5. `迁移流式转换并清理旧协议层`

默认先执行局部验证。`bun run build`、根模块 `go test ./...` 等重型验证在最终阶段单独确认后执行。
