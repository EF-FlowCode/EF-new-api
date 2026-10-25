# Task Plugin 任务身份与查询准备

> 定位：现行本地合同；文内实现阶段的测试命令与结果为历史证据。当前执行检查须使用[Docker 入口](../development/local-development.md#docker-检查入口)，部署与真实供应商结论按各自记录的范围判断。

阶段 4 已实现内部任务持久化、固定版本恢复与查询准备；统一宿主已将其接入全部 11 provider 提交与轮询。生产总开关和绑定默认关闭。JS 只生成请求描述、执行一次受控 HTTP 并解析候选；Task 终态、CAS 和额度仍由 Go 状态机负责。

## 持久身份与存储

`Task.PrivateData.Plugin` 是可选的内部身份，保存 `identity_version=1`、`api_version=1`、key、version、source_hash 和 factory 来源标记。现有上游任务 ID、凭据与 BillingContext 独立保留。没有插件身份的旧数据解释为历史任务；全部已迁 provider 的旧 Go 运行时已退役，须显式接管后恢复轮询；私有字段不进入公开 Task JSON。

`TaskPrivateData.Scan` 在读取下一行时清空旧字段，防止复用扫描对象造成旧 Go 任务继承上一行插件身份。普通 `Task.Insert()` 拒绝带插件身份的对象；内部插件任务应通过 `Manager.CreatePluginTask` 保存。

| 表 | 职责 |
| --- | --- |
| `tasks.private_data` | 任务持有明确身份和既有私有上下文 |
| `task_plugin_source_snapshots` | 按 SHA-256 去重的不可变源码，MEDIUMTEXT、最大 1 MiB；保存 key/version/api_version 供校验 |
| `task_plugin_task_references` | Task 数据库主键到插件身份的独立关联；key/version/factory 有复合索引，用于删除检查 |

两张新表加入现有迁移清单，不创建新后台同步服务。工厂与数据库 override 都有独立于当前运行时的持久源码快照；不把 1 MiB 源码重复写入每个 Task JSON。引用与快照没有公开编辑、删除或自动清理接口，历史任务引用保守保留；直接删除 Task 不会自动回收这些记录。

保存任务先编译、校验明确版本，再在同一事务内锁 `task_plugin_controls` 控制行、复核开关与版本、保存源码快照、Task 和引用。失败整体回滚，不改调用者对象。任务保存不推进管理 revision，因为任务引用不改变 active registry；它与管理版本删除共用控制锁，防止保存/删除竞态产生悬空引用。

Root 删除数据库插件版本时，只要存在非工厂 Task 引用就返回 409，包括已经完成的历史任务。解除影子绑定不能解除 Task 引用保护。工厂任务用持久源码恢复，不依赖同名 override；源码快照本身没有删除入口。本阶段不开放绕过引用保护的强制删除。

## 内部使用边界

`service/taskplugin.Manager` 增加：

| 方法 / 结果 | 行为 |
| --- | --- |
| `AdoptLegacyTask(ctx, id)` | 按当前固定绑定原地接管全部已迁平台历史任务（Jimeng 要求已有准确 req_key）；同绑定幂等，不重发、不改额度与状态 |
| `CreatePluginTask(ctx, task, TaskVersion)` | 保存内部提供的新任务和指定版本；要求上游 ID、公共 ID 和有效身份，返回保存后的新对象 |
| `PrepareTaskQueries(ctx, ids, environments)` | 从一个 MySQL 一致快照读取任务/引用/源码/开关，返回旧 Go 任务 ID 列表与插件查询计划 |
| `TaskQuery.TaskIDs()` | 返回该查询覆盖的 Task 数据库主键副本 |
| `TaskQuery.Descriptor()` | 返回请求描述副本，不执行 HTTP |
| `TaskQuery.Execute` / `ParseProduction` | 生产执行/解析前复核绑定开关，历史任务使用原固定源码 |
| `TaskQuery.Parse(ctx, response)` | 再次读取并检查持久身份、查询上下文和开关，返回带 Task 主键与固定身份的候选结果 |

这些方法仅供可信宿主代码使用。它们不代替 API 的租户鉴权、渠道分配、凭据准备或预扣；不得直接接受用户提供的身份或查询环境。`TaskQueryEnvironment` 只允许宿主准备 base URL、authHeader 和 auth；模型、action、上游 ID 和 API key 来自持久任务。环境中的 auth map 复制后保存，限制 64 KiB，不保留可变请求上下文。

`CreatePluginTask` 是内部存储边界。生产提交在插件明确返回 taskId 后调用它保存固定身份、源码快照和引用；网络超时等受理未知不会自动重试或回退 Go，仍需后续恢复专题处理。

## 查询分组与解析

一次准备接受 1–100 个不重复的 Task 数据库主键，缺失任务或任一校验失败时不返回部分计划。逐任务查询各自恢复原版本；批查询只合并身份、渠道、用户、凭据、action 和模型都相同的任务。不同版本、不同用户或不同凭据不会合批。查询结果按上游 ID 对应 Task，而非按响应数组位置；批响应中的未知 ID 或重复 ID 被拒绝。缺少的批结果不伪造终态。

准备和解析都核对私有身份、引用与源码哈希；解析还复核用户、渠道、凭据及查询上下文未被替换。不依据当前 active 或当前渠道类型偷换旧版本，不自动退回 Go 或最新脚本。Google/Vertex 继续使用插件编码的 operation identity；Vertex 脚本仅接收准备好的鉴权输入，OAuth 交换已由 Go 宿主实现。

候选结果由宿主 JSON 边界复制，包含 `TaskID`、`Identity`、`Result`、`CompletionUsage` 和 `CompletionUsageInvalid`。轮询消费方将 `taskId/status/url/reason/progress` 投影为 `TaskInfo`，再复用现有状态/CAS 与 `FinalizeTaskWithBilling`；候选不是结算指令。提交 usage 经 schema 校验后转换为本地时长/分辨率计费维度；价格、预扣和退款仍由 Go 负责，完成 usage 经同一 schema 校验后持久化；s2v/Wan3 通过提交价格快照、持久待结算及原子差额结算，其他模型保留原资金合同，见 统一宿主。

## 开关与错误

| 条件 | 查询准备/解析行为 |
| --- | --- |
| 激活新版或回滚 | 旧任务继续按自身身份恢复 |
| 总开关关闭 | 拒绝新增 JS 查询准备与解析 |
| active override 禁用 | 同 key 的历史 override 与工厂任务均暂停 |
| 所引用的非 active override 禁用 | 该版本任务暂停，其他版本按各自开关处理 |
| 工厂层或该工厂 key 禁用 | 工厂任务暂停，不影响独立启用的 override |
| 开关恢复 | 原身份继续有效，不重新提交任务 |
| 身份非法/不匹配 | `ErrTaskPluginIdentity` |
| 源码或所引用版本缺失 | `ErrTaskPluginMissing` |
| 开关禁用 | `ErrTaskPluginDisabled` |
| Task 不存在 | `ErrNotFound` |
| 存储不可用 | `ErrUnavailable`，不回退旧脚本或更改 Task |
| JS 构造/解析非法结果 | `ErrTaskQueryHook`，不泄露脚本错误原文 |
| 调用取消/超时 | 保留 context 取消/超时错误 |

新接口按数据库读取时的一致快照检查开关，不依赖管理运行时的 30 秒刷新。不过已经返回的 descriptor 只是数据，不是可持续使用的执行授权；正式 HTTP 调度已在发送前重新门控，也不能承诺检查后的跨节点即时撤销。

## 验证记录

2026-09-15 用户批准阶段 4 和临时 MySQL 定向测试后，新增 7 项顶层测试通过，其中 5 项实际使用 MySQL：

- JSON 往返、公开字段脱敏、扫描器复用/空值、普通 Insert 防绕过。
- 源码去重、混合 Go/插件读取、Task/引用/源码整体回滚、缺失与篡改拒绝。
- 6 轮并发保存任务/删除版本，没有悬空引用；任务创建不无故推进 revision。
- 10 插件保存后，用不带任何 factory 源码的新 Manager 恢复代表查询。
- 激活/回滚、工厂升级、总开关/工厂开关/active 与非 active 版本禁用；恢复后仍使用固定版本。
- Alibaba 成功候选不修改已存 Task 状态或 Quota；Suno 跨版本、用户、凭据批查询隔离，乱序对应、未知/重复 ID 拒绝。
- 存储不可用不返回部分查询计划，不改变传入任务。

主验证命令（`TEST_MYSQL_DSN` 指向独立测试服务器）：

```powershell
go test ./model ./service/taskplugin ./controller/taskplugin -run '^Test(TaskPluginIdentityJSONLegacy|TaskPluginTask|TaskPluginQueries|TaskPluginStore|TaskPluginShadowBindingReferenceTransaction|RootManagementMySQL)' -count=1 -v
go test ./service/taskplugin ./controller/taskplugin ./model -run '^Test(TaskPluginQueriesVersionSwitchesAndCandidatesMySQL|RootManagementMySQL|Snapshot_Roundtrip|UpdateWithStatus_)' -count=1 -v
go test ./service/taskplugin -run '^TestTaskPluginQueryStorageUnavailable$' -count=1
```

第二条复验新增的非 active 版本关停/取消、真实 Root 删除历史任务引用返回 409，并覆盖已有 Task CAS 胜出/失败/并发用例。此前管理存储及影子引用事务测试也通过。首次创建源码时的 `record not found`、故障注入回调和旧重复键 SQL 日志是预期测试路径。

临时 `new-api-taskplugin-identity-20260915` 使用 MySQL 8.4.8，1 CPU/1 GiB、仅 loopback；测试后确认随机数据库无残留，容器与独立匿名卷已删除。没有业务库迁移、应用启动、全量构建/测试、真实供应商请求或部署。重建 Manager 的验证不等同真实多进程切换验证；10 个代表查询不证明所有模型和计费等价。

整体迁移仍为 `partial`。统一宿主已接全部 11 provider 提交与轮询、既有本地补偿与终态 CAS，并增加实际提交 source hash 校验；最新本地测试证据和未闭合项见 生产生命周期。渠道 62、SSE、完整公共协议、Artifact 与市场仍未接入。
