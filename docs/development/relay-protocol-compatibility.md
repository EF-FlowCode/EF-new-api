# 协议兼容定向合并（2026-10-03）

> 定位：现行开发与行为约束。带日期的部署或验证段落保留为当时记录；分类入口见[文档导航](../README.md)。

本轮基于本地 `9ff5e7e32b13f3ee328400b5a338dceadc71ef5f`，定向采用官方
`1a4166d8e8ba9802d2ca56fe8ecf0ed5404e80d5` 中的协议兼容修复。
本批以一次本地提交保存实现与局部验证结果，尚未构建或部署；不代表全部上游更新已迁入。

## 已合并行为

| 范围 | 行为与本地整合方式 |
| --- | --- |
| Claude 原生请求 | `messages[].output_config` 与顶层 `safeguards` 使用 `json.RawMessage` 保留；不把原生字段强行映射到其他协议 |
| OpenAI 模型能力 | `GetOpenAIChatCapabilities` 统一采样参数、developer role 与 `max_completion_tokens`；正式请求与显式/自动渠道测试共用规则。使用映射后的模型名，保留已有推理后缀保留名单 |
| 工具结果媒体 | Responses 图片、文件、音频、视频与 Claude 图片结果转 Chat 时，文本留在 tool 消息，媒体移到后续 user 消息；同批 tool 结果保持连续。未知块保留原 JSON 回退 |
| Responses custom tools | 一次性将工具定义、指定工具选择、调用历史和结果转换为目标协议支持的 function 工具，参数为必填字符串 `input`；Chat、Claude、Gemini 的响应及流式事件按请求级映射还原为 `custom_tool_call` |
| Responses 流式 | 缓存晚到名称，最终无名调用不输出；自定义工具参数收齐后解包原始字符串，再发 input delta/done。中途 finish 后的新文字/推理产生独立 item，最终响应保留全部分段 |
| 宿主收敛 | Advanced Custom 与 Ali 的 Responses→Chat 响应共用 RelayKit 状态机；删除宿主重复状态机与无调用方 usage 映射。Ali 的字段过滤仅用于 Ali，Advanced Custom 保留通用字段 |

已知标准 GPT-5.1/5.2/5.4 与 GPT-6 Sol/Luna（含合法日期快照）仅在无推理或
`reasoning_effort=none` 时保留采样参数；GPT-6 Astra 不保留这些参数。
未知模型/变体不套用已知模型的限制。GPT-5 命名变体不自动继承标准型号的采样例外。

## 工具转换合同

请求转换使用克隆，不修改重试时的原始请求。`convmeta.Values.ResponsesTools` 仅保存本次成功编码的自定义工具名；
请求转换失败和渠道重试会清理宿主映射。显式多跳共享这一映射，响应转换必须传入同一请求上下文。
自定义 `convmeta.Meta` 实现可额外实现 `ResponsesToolStateCarrier`；基础接口没有增加必需方法。
传入 nil 上下文的旧便捷入口只能完成请求转换，无法为后续响应保存工具身份。

同名 function/custom、重复 custom、空 custom 名和未声明的 custom `tool_choice` 返回转换错误，
不静默删除用户指定的工具。grammar 内容作为工具描述中的提示传递，不承诺上游执行原生 grammar 约束。
原始字符串即使看起来像 JSON 也仍作为字符串传递；返回非字符串 `input` 时保留 JSON 表示及整数精度。

Chat→Responses 非流输出的推理项使用 `summary` 并排在文字前；不适用于该 item 的空 role/content/quality/size 被省略。
流式序号通过 `ResponseStreamOptions.EmitSequenceNumber` 开启；宿主 Responses→Chat 桥开启此选项。
Responses→Gemini 的现有 Advanced Custom 路由沿用原有 SSE 外壳。

Claude/Gemini/Chat 三种协议的 custom tool 流/非流转换已在库或宿主边界验证。
本轮没有新增 Responses→Claude 的渠道路由；Claude 流中的 input/output usage 分帧仍由宿主聚合，
调用方在 `FinalizeStreamResponse` 前通过 `SetUsage` 提供完整用量。

## 保留的本地修复

- `884532754` 的 Claude tool_result 多模态估算继续递归计文本和媒体，避免把 base64 当正文计数。
- `e827f34aa` 的 Gemini 推理预算、等级、显式 false、模型映射优先与多跳状态继续保留。
- canonical usage 保留缓存、图片、音频、视频等明细。Chat 桥只在 usage 缺失/null 时估算，显式零值不改成估算。
- 上游错误、非法流和读取异常不生成成功终态；原生 Responses 的终态结算与禁止重放合同保持独立，见 [流结算说明](responses-stream-accounting.md)。

## 后续请求链修复（2026-10-03，已提交）

在协议兼容提交 `f6a352495` 和限流提交 `d64edcb0b` 之后，定向补入两项上游兼容修复：

- Kimi K3（`6e10f9bc9`）：原生 Chat 的 `messages[].tools` 使用 `json.RawMessage` 保留，只有携带 tools 且 content 为 nil 的消息省略 content；普通 `content:null` 和显式空字符串保持。消息级工具参与输入 token 估算，系统提示注入跳过工具声明消息，Moonshot 名单加入 `kimi-k3`。不承诺将消息级动态声明映射到其他协议。
- 智谱（`3e8c358da`）：Chat 请求转换保留显式 `reasoning_effort`，未提供时继续省略；保留 THINKING、Stop、最大 token 选择及 base64 图片归一。

另修复本地与固定上游均存在的 Ollama 独立流失败标注缺口，详见[成功次数限流](../guides/model-request-rate-limit.md)。该项不是直接迁入已有上游补丁。

Docker 固定 Go 1.26.1 镜像、Linux 命名卷的局部回归通过：宿主六相关包及 RelayKit 五相关包，覆盖 Kimi 原生 JSON/大整数、重试克隆隔离、动态工具估算、提示注入隔离、普通消息合同、既有 custom tools 转换、智谱归一和 Ollama 失败/成功限流联动。智谱测试初次使用了非真实请求的内容类型，改为 JSON 输入/输出后复测通过；生产代码只新增字段透传。

本批已提交 `c251ff5b3`，未构建、部署，未跑全仓或真实供应商验收；基点及审计游标不变。

## 上游缺口移植（2026-10-16，分支 `gap/relay-20261016`）

按意图移植、各自原子提交，均有 Docker 内单测：

| 上游 | 本地行为 |
| --- | --- |
| `036ba376d`（部分） | `claude-opus-4-6/4-7/4-8-<effort>` 后缀写入的 `output_config.effort` 经 `reasoning.ClaudeEffort` 落到模型接受的档位（minimal→low；4.6 的 xhigh→max；4.5 的 xhigh/max→high），未知模型原样。Gemini 2.5 的 xhigh/max 用模型最大预算（2.5 Pro 32768，Flash/Flash-Lite 24576），high 仍为 24576。本地无 Claude 能力渲染器，关闭思考时保留 effort、Haiku 5.5 能力表等未移植 |
| `c10780e05` | Chat/Responses→Gemini 的工具参数与结构化输出：类型化 Schema 装得下的照旧清洗；装不下的（const、oneOf、`$ref`/`$defs`、type 数组、非字符串 enum、tuple items 等）原样放进 `parametersJsonSchema` / `responseJsonSchema`。Vertex 渠道恒支持，其余渠道在 Gemini 版本设置为 `v1` 的模型上仍清洗。`additionalProperties`、`$schema` 单独出现不触发切换 |
| `78bd5b1cb`（部分） | Responses `function_call_output` 的 content-part 数组：文本留在 `response`，图片/文件转 `inlineData`；Gemini 3 放 `functionResponse.parts`，1.x/2.x 放在同轮所有 functionResponse 之后。无法承载或加载失败的媒体被省略（无 convdiag，只记日志）。inlineData MIME 白名单改按 Gemini Blob 列表，音视频按前缀放行 |
| `ca2cae676` | `type=reasoning` 的输出项在 summary 为 nil 时也序列化为 `"summary":[]` |

请求体读取失败的报错与 413（`d10eafb57`、`ef60ef27f`）属于中间件层，见对应提交说明。
未移植：`0a03088e0`（转换后流式失败计费改口径且依赖上游 toolconv/渲染器）、`19d435e7f`（错误遮挡收窄是安全取舍）、`c6bac617d` 的后缀开关（会改变 Gemini 适配器默认关闭时 effort 后缀的现有行为），待站长决定。

## 来源与剩余边界

| 上游提交 | 本轮结果 |
| --- | --- |
| `c2b7a9a9e` | 采用 Claude per-message output_config |
| `789c97019` | 采用 Claude safeguards |
| `49ec46966` | 采用模型能力表，按本地后缀与宿主入口整合 |
| `d0cb7347c` | 采用 GPT-6 max_completion_tokens，并补齐显式渠道测试入口 |
| `6e9de44a7` | 采用 Responses tool output 媒体提升与批次连续性 |
| `4eb3b9160` | 采用 Claude tool_result 图片提升 |
| `335ebcff7` | 部分采用：custom tool Chat 往返、状态传播及响应事件；用共享预处理接入本地转换器 |
| `1a4166d8e` | 部分采用：custom tool Claude/Gemini 往返和无名调用处理；移除原 Gemini 丢弃 custom 历史的预处理 |

未整体迁入上游 toolconv/hosted tools/annotations 体系与诊断丢弃策略，也未迁入全站 `@` 修改器、
其他 provider 原生推理 renderer、插件、认证或计费配置。新增 hosted tool 类型、grammar 强约束或上游工具策略变化时专项复审。
Git 共同基点 `2d8e50bf3` 与正式审计游标 `918427d8a` 保持不变；游标之后本次固定来源共 218 个提交，未做全量逐条审计。

## 验证

通过 `scripts/docker-check.sh fmt` 同步至 Linux `newapi-src` 命名卷并格式化。
所有 Go 测试使用 Dockerfile 固定 Go 1.26.1 镜像（digest `2389ebfa5b7f43eeafbd6be0c3700cc46690ef842ad962f6c5bd6be49ed82039`）。

- RelayKit 定向包：`dto`、`relayconvert`、`internal/oai_chat`、`internal/oai_responses`、`internal/claude_messages`、`reasoning`，全部通过。
- 宿主 9 个包的相关用例：`service/openaicompat`、`service`、`relay/common`、OpenAI/Claude/Gemini/Advanced Custom 适配器、`relay`、`controller`，全部通过。
- 覆盖强制工具选择、原始字符串、冲突、重试清状态、晚到名称、工具流、分段、媒体批次、零用量/缓存/视频、Gemini 推理和原生 Responses 禁止重试。
- 36 个协议请求/响应/流转换快照用例通过；只更新 6 个输出 Responses 的快照，差异为 summary 顺序与不适用空字段省略，usage 未变。

未执行整仓全量测试、build、前端检查、真实供应商请求、数据库/发布升级验收。
未启停业务服务或修改线上配置；短期测试容器已自动删除，命名缓存卷保留。
现有线上候选镜像的验收记录不能用于放行本轮源码。
