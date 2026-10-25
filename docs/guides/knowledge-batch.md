# Embedding Batch File

> 定位：现行使用与配置指南。带日期的部署或验证段落保留为当时记录；分类入口见[文档导航](../README.md)。

Batch File 用于离线构建知识库：将**已经分段的文本请求**写入 JSONL，上传后异步生成向量。同步 `/v1/embeddings` 的 `input: ["文本1", "文本2"]` 仍是同步调用，不能因为输入为数组而使用 Batch 价格。此接口不解析 PDF／Word，不接收已有向量作为 Embedding 输入，也不负责向量数据库存储。

## 开启条件

- 模型为 `text-embedding-v1/v2/v3/v4`、`qwen3.7-text-embedding` 或 `qwen3.7-text-embedding-flash`。
- 渠道类型为 Ali 或 OpenAI 兼容渠道，Base URL 为百炼北京官方地址：`https://dashscope.aliyuncs.com` 或北京 `*.cn-beijing.maas.aliyuncs.com`。可附 `/compatible-mode` 或 `/compatible-mode/v1`；不接受自定义转发域名、新加坡、美国或任意端口。
- 文件内模型名必须是上游真实名称；当前 Batch 不支持模型重命名映射。同步请求仍使用已有模型映射。
- 在普通模型定价表中配置该模型的文本输入和 Batch 输入价格。`BatchInputRatio=0` 表示免费，留空表示未开通；文本输入价格同时用于同步请求。
- 使用现有出站 HTTP 安全策略。当前 Worker 协议无法承载 multipart 文件上传，开启 Worker 时不开放 Batch；也不支持带独立渠道代理的 Batch。不会通过绕开安全配置来上传。

当前 Rerank、Qwen VL Embedding、tongyi vision Embedding 不在百炼原生 Batch 支持清单中；这些模型仍可同步调用。没有通过循环同步请求模拟半价 Batch。

## 本实现限额

| 项目 | 限额 |
| --- | --- |
| 单个输入 JSONL | 8 MiB，UTF-8 |
| 文件请求行数 | 2,000 行 |
| 单行长度 | 小于 6 MiB |
| 每用户保留输入文件 | 100 个 |
| 每用户未结算批任务 | 10 个 |
| 完成窗口 | `24h` |
| 单行文本数 | v1/v2：25；v3/v4：10；qwen3.7 两款：20 |

以上为本网关限制，低于供应商 500 MB／50,000 行上限。单条文本 Token、维度取值等模型限制最终仍由供应商校验。新任务按批任务计入请求频率限制；另有活跃任务与文件限额。

## 使用示例

输入 `requests.jsonl`，每行 `custom_id` 唯一（1～128 字节），全文件使用同一模型：

```jsonl
{"custom_id":"chunk-001","method":"POST","url":"/v1/embeddings","body":{"model":"qwen3.7-text-embedding","input":["第一段知识库文本","第二段知识库文本"],"dimensions":1024}}
{"custom_id":"chunk-002","method":"POST","url":"/v1/embeddings","body":{"model":"qwen3.7-text-embedding","input":"第三段知识库文本","dimensions":1024}}
```

每行顶层只允许 `custom_id`、`method`、`url`、`body` 四个键；任意层级的重复键，以及 `body` 内与 `model`、`input`、`dimensions`、`encoding_format`、`instruct` 仅大小写不同的键（如 `Body`、`MODEL`），都会以 400 拒绝。通过校验的文件按规范键顺序重新序列化后保存和上传，文件对象的 `bytes` 为规范化后的大小；`body` 中其他字段原样透传给供应商。

下面使用 Bash / curl 说明 HTTP 请求。`BASE_URL` 为本站地址，`API_KEY` 为用户自己的 API Token。

```bash
curl "$BASE_URL/v1/files" -H "Authorization: Bearer $API_KEY" \
  -F purpose=batch -F file=@requests.jsonl

curl "$BASE_URL/v1/batches" -H "Authorization: Bearer $API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"input_file_id":"file_返回的ID","endpoint":"/v1/embeddings","completion_window":"24h"}'

curl "$BASE_URL/v1/batches/batch_返回的ID" -H "Authorization: Bearer $API_KEY"

curl "$BASE_URL/v1/files/返回的output_file_id/content" \
  -H "Authorization: Bearer $API_KEY" -o results.jsonl

curl -X POST "$BASE_URL/v1/batches/batch_返回的ID/cancel" \
  -H "Authorization: Bearer $API_KEY"
```

创建接口返回本地 Batch ID 和 `validating`，并由后台上传与提交。创建／查询／列表／取消返回 OpenAI 风格对象，额外提供 `billing_status`。本地文件和任务 ID 与供应商 ID 隔离。

| 操作 | 路径 |
| --- | --- |
| 上传输入文件 | `POST /v1/files`（multipart，`purpose=batch`） |
| 文件列表 | `GET /v1/files?limit=100&after=...&purpose=batch` |
| 文件元数据 | `GET /v1/files/:id` |
| 下载输入／成功／失败结果 | `GET /v1/files/:id/content` |
| 删除本地文件记录 | `DELETE /v1/files/:id` |
| 创建批任务 | `POST /v1/batches` |
| 批任务列表 | `GET /v1/batches?limit=20&after=...` |
| 查询批任务 | `GET /v1/batches/:id` |
| 取消批任务 | `POST /v1/batches/:id/cancel` |

列表按新到旧排列，`limit` 为 1～100，下一页使用 `last_id` 作为 `after`。文件和任务均按用户归属隔离。预扣耗尽 API Token 后仍允许访问已有任务；禁用、过期、用户封禁及 IP 限制仍生效。

取消操作若遇到正在更新的任务返回 `409`，客户端稍后重试。取消请求受理不等于立即退款，也不会撤销已成功的向量计算。输入文件被活跃任务引用时不能删除；删除只清理本站记录，不删除供应商副本。结果文件内容由供应商保存并流式下载，本地不长期缓存完整向量文件，需在供应商保留期内下载。输出文件的 `bytes` 当前未预取，返回 `0`，不应据此判断结果是否为空。

## 预扣、结算与恢复

1. 创建时先持久化任务、锁定输入文件并原子预扣用户钱包及 Token 额度，成功后才可能发送上游请求。预估按文本及重复指令字节数加每行 256 的余量计算，并向上取整；它不是最终 Token 数。
2. 创建端从渠道中选择一把可用 Key，并将模型、该凭据、Batch 单价、额度单位和分组倍率随任务快照保存。空白或全部禁用的凭据在预扣前拒绝；多 Key 渠道不会保存整个 Key 列表。创建结束即释放请求调度占用，不占用整个异步处理窗口。后续修改价格不会改变该任务结算。
3. 终态扫描成功文件，按成功行实际输入 Token 计费。失败行不收费，部分失败／过期／取消任务也会为已成功部分计费；余额差额通过已有 Task CAS／原子资金事务补扣或退款。
4. 任务终态和结果文件引用只在结算事务提交后公开。重复查询、后台重启或重复轮询不会重复提交已知任务或重复扣费。额度不足、结果未完整下载、缺失权威用量或数量不一致时继续等待并重试结算，不提前宣称成功。
5. 独立轮询每 15 秒触发，最多并行处理 4 个 Batch；租约阻止多实例同时处理同一任务。普通视频任务的 30 分钟超时回收不适用于 Batch。

`billing_status` 的常见取值：

- `reserved`：预扣已完成，任务处理中或等待最终结算。
- `settled`：最终结算已原子提交，可下载成功／失败结果。
- `submission_unresolved`：提交发生超时或进程中断，无法确定上游是否受理。后台会按持久化 metadata 身份查询上游任务列表并接管找到的任务，绝不盲目重复提交。

明确的上游拒绝会持久化失败意图并退款。尚未开始提交的任务超过一天会过期退款。**未知受理且始终找不到对应任务的情况，不会自动认定免费或退款**；需运营人员与供应商核对受理记录后处置。当前没有自动关闭此类争议任务的管理入口。单次核对最多扫描 100 页，超过后继续保留预扣并记录错误。

结算账务与 Task 状态在同一数据库事务中；消费日志／统计沿用现有 Task 机制，仍在事务后记录，崩溃窗口内不保证日志完整。结果下载或上游凭据失效会影响历史文件获取。

## 验证与上线边界

2026-09-14 已在本地 MySQL 8.4.8 隔离测试库验证预扣回滚、并发限额、跨用户文件隔离、终态幂等、取消部分成功结算、明确拒绝退款、未知受理恢复和结算不足回滚／补款后重试。另用实际创建 Controller 验证单 Key、多 Key 跳过禁用项、空白和全部禁用凭据；缺陷回归先复现失败，修复后通过。上述局部测试使用模拟上游响应；用量归一化、协议形状、价格展示及渠道能力判断也通过局部测试。重跑数据库测试需要配置 `TEST_MYSQL_DSN`；测试会创建并清理隔离测试库。

新增 `knowledge_files` 表和 `tasks.batch_lease_until` 列，部署需要执行正常数据库迁移。新旧后台实例混跑不受支持：旧实例不认识 Batch 平台，可能执行错误超时回收；应统一升级后台后再开放该能力。后台与请求实例分开发布时，必须同步发布支持此契约的后台版本。

2026-09-14 经授权完成 Docker 构建、生产数据库迁移及 `eflow-rolling-v3` 维护升级。升级前保留数据库、配置、应用数据和旧镜像备份；隔离库迁移与正式迁移的 schema 一致，仅新增上述表和列。请求端与后台已运行同一镜像，公网入口、静态资源和未认证访问限制通过检查；三个文本模型的 Batch 价格与分组渠道能力已在公开定价接口核验。

随后使用授权临时 Token 进行真实付费联调：知识库分组 10 个模型共 14 次同步调用全部成功，覆盖文本／图片 Embedding、文本／图文 Rerank、向量维度及有限值、相关文档排序和图片 Token 明细。逐笔结算与配置价格及现有 0.5 分组倍率一致，同步费用合计 ¥0.000224。此为小样本功能验证，未执行压力测试或全量回归。

真实 Batch 首轮发现创建端遗漏渠道 Key 选择，导致三个任务在上游上传阶段返回 401；预扣全部退回。补齐凭据绑定并通过回归后，再次备份数据库、配置、应用数据和旧镜像，以 v3 兼容方式平滑发布 Web 修复，保留既有 v3 后台。

修复后，三个文本模型的真实 Batch 均完成上传、上游受理、轮询、结果下载和最终结算。每个任务包含 2 行请求，返回 3 条 1024 维有限向量，`custom_id` 与输入对应：

| 模型 | 成功输入 Token | 最终费用（含现有 0.5 分组倍率） |
| --- | ---: | ---: |
| qwen3.7-text-embedding | 62 | ¥0.000008 |
| qwen3.7-text-embedding-flash | 62 | ¥0.000002 |
| text-embedding-v4 | 21 | ¥0.000002 |

独立取消测试到达 `cancelled/settled`，未成功计算部分的预扣全额退回。活跃任务的输入删除返回 `409`，完成后输入和输出文件可以删除；已清理本次测试的本站文件记录。同步与 Batch 测试最终总费用为 ¥0.000236，无剩余预扣。费用按本站整数额度精度取整，极小请求的金额可能与未取整理论值有差异。Batch 为异步服务，实测不同模型的完成时间差异明显，不保证即时完成。

当前官方基础价和分组价见 [knowledge-base-pricing.md](knowledge-base-pricing.md)。跨契约回滚前必须检查新 Batch 任务并完成结算或受理核对，不能直接恢复旧后台而遗漏待结算任务，也不能用迁移前备份覆盖上线后的新业务数据。
