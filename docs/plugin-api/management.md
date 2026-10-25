# Task Plugin 管理与持久化

> 定位：现行本地合同；文内实现阶段的测试命令与结果为历史证据。当前执行检查须使用[Docker 入口](../development/local-development.md#docker-检查入口)，部署与真实供应商结论按各自记录的范围判断。

管理代码已接入，新环境默认总开关和生产绑定均关闭。eflow 于 2026-09-17 完成 v5 迁移与部署，当前开启总开关，并为两个现有 Happyhorse 渠道绑定固定内置版本；其他 provider 未自动绑定。全部 11 个内置 provider 均支持 Root 显式绑定后的受控提交与轮询，Task 身份、源码引用和本地 CAS/Billing 由 Go 协调层负责。类型映射与边界见 统一宿主。

## 存储与发布

`task_plugins` 保存不可变的 key/version/source/source_hash/api_version，以及可变的 active、enabled、remark、icon。源码最大 1 MiB，图标 data URI 最大 512 KiB；两列使用 MySQL MEDIUMTEXT，避免官方 TEXT 的 64 KiB 容量不足。版本最长 64 字节，VARBINARY 保留 SemVer 大小写区别。

`task_plugin_controls` 的单行 `id=1` 保存 revision、总开关、工厂层开关与禁用的工厂 key。默认 revision=1、总开关关闭、工厂层开启。`task_plugin_shadow_bindings` 独立保存影子绑定（旧 Go 观察源已随物理清理删除，当前不会产生在线样本）；阶段 4 增加 `task_plugin_source_snapshots` 和 `task_plugin_task_references`，用于持久任务身份与引用。阶段 5 增加 `task_plugin_production_bindings`，与影子绑定独立。现有迁移流程创建这六张插件表及控制初始行；web/worker 角色不自行修复缺表。升级须先完成 migrate，再启动应用。

所有管理写入先锁控制行，在同一事务内读取版本与影子绑定、校验引用、更新并递增 revision。编译和冲突校验使用独立 registry 和影子句柄；提交成功后整份发布。预检或提交失败保留旧版本；刷新失败保留最后成功运行时，并在 runtime/status 中报告。

各节点启动时刷新一次，此后每 30 秒读取一致快照。revision 未变时跳过重编译；跨节点最终一致，不提供即时撤销保证。状态中的 `observed_revision` 是本节点最近读到的数据库版本，`applied_revision` 是已成功发布的版本。`last_error` 记录最近刷新错误；启动从未成功时 applied_revision 为 0。停止同步器时等待在途刷新退出，再关闭 DB。

## 版本与开关

- 第一个数据库版本自动设为 active；后续上传只存为待激活版本。启用缺省为 true，但总开关初始关闭。
- 同 key/version 再上传相同源码可更新启用状态、备注和非空图标；不同源码返回 409。改源码必须增加版本。
- 激活指定版本同时将其 enabled 设为 true；选择旧版本即回滚。每个 key 最多一个 active。
- 禁用 active override 同时屏蔽同名工厂，避免回退后复活。没有 override 时逐插件开关修改工厂禁用集合。工厂层总开关只影响工厂，数据库 override 独立控制。
- 删除 active 版本会激活剩余版本中最新上传的一版，保留其 enabled 状态；没有剩余版本则回到工厂，是否生效取决于各层开关。工厂源码不可删除。
- 被影子绑定引用的数据库版本不可删除（包括禁用绑定），需先解除绑定。绑定/删除共用事务锁，防止并发产生悬空引用；工厂绑定单独保留源码快照。
- 被 Task 引用的数据库版本同样不可删除，返回 409；历史终态任务也保守保留引用，解除影子绑定不足以解除这项保护。内部任务保存与版本删除共用控制行锁；源码快照无公开清理接口，规则见 [任务身份与查询准备](task-identity.md)。
- 总开关开启时，冲突会使事务失败，不采用 `force` 隔离或自动选边。可先 `enabled:false` 上传，再解决冲突后激活。总开关关闭时允许保存合法但互相冲突的脚本，开启前统一检查。
- 关闭总开关无需编译已存脚本，即使升级后某个脚本损坏也可关闭；重新开启必须全部通过校验。

## Root API

所有路径前缀为 `/api/plugin/task`，全部使用现有 `RootAuth`，包括详情、图标和状态。会话或访问令牌仍须带匹配的 `New-Api-User`。普通用户和管理员不可访问。接口没有绕过鉴权的图片下载例外。

| 方法 / 相对路径 | 输入 / 行为 |
| --- | --- |
| GET 空路径 | `{settings, overrides, factory}`；不返回源码或图标正文 |
| POST / PUT 空路径 | `{source, enabled?, remark?, sourceSha256?, icon?}`；返回编译后的 meta；`force:true` 拒绝 |
| GET `/:key` | 可选 `?version=`；返回 `{plugin, source, icon, factory}`，默认 active 或内置版本 |
| GET `/:key/versions` | 数据库版本，按上传顺序倒序，不含源码 |
| GET `/:key/icon` | 可选 `?version=`；经校验的 SVG/PNG，缺失返回 404 |
| POST `/:key/activate` | `{version}`，激活或回滚 |
| POST `/:key/status` | `{enabled}`，逐插件开关 |
| DELETE `/:key/versions/:version` | 删除数据库版本 |
| POST `/:key/dryrun` | `{hook, member?, args?}`，可选 `?version=`；离线执行，不发送 HTTP |
| GET `/runtime/settings` | 数据库 revision 与三个层级的开关配置 |
| PUT `/runtime/settings` | `{enabled?, factory_enabled?}`，至少指定一项 |
| GET `/runtime/status` | 本节点应用/观察的 revision、启用状态、时间与刷新错误；`task_dispatch_connected:true`（表示宿主已接入，不代表开关已启用） |
| GET `/runtime/shadow/bindings` | 列出影子绑定，不返回源码 |
| PUT `/runtime/shadow/bindings/:channel_id` | `{key, version, factory, enabled, sample_every}`；首期 Alibaba 渠道与 alibaba 插件 |
| DELETE `/runtime/shadow/bindings/:channel_id` | 解除影子绑定 |
| GET `/runtime/shadow/reports` | 本节点最近 100 项以内脱敏报告与丢弃数、worker 状态 |

除图标外，成功返回 `{success:true,message:"",data:...}`；输入错误 400、未找到 404、版本/路由冲突 409、存储不可用 503。权限失败沿用现有认证中间件的状态码及 envelope。列表/详情响应是本地管理结构，不保证与官方管理前端直接兼容。

上传 HTTP body 上限 7 MiB（包含 JSON 转义开销），源码解码后仍限制为 1 MiB；小型状态操作 4 KiB，dryrun 输入 1 MiB / 最多 16 参数。dryrun 每节点最多 4 个并发调用，hook 超时 1 秒，并响应请求取消。脚本没有网络、文件系统或环境访问能力，仍不具备硬内存隔离，只允许 Root 管理。

生产提交在发送前再次检查 DB 开关和固定版本；插件 HTTP 失败不会自动重走 Go，避免供应商已受理时重复提交。查询解析出的 status/url/reason/progress 只投影到既有 Go 状态机，CAS 和退款继续由本地代码执行；已接原有 token 完成重算，通用完成 usage 差额结算尚未接入。Vertex OAuth 由宿主执行。当前不提供未知受理恢复、SSE、完整 Artifact、市场或完整 RBAC。

生产绑定接口：`GET /runtime/production/bindings`、`PUT /runtime/production/bindings/:channel_id`、`DELETE /runtime/production/bindings/:channel_id`。PUT 接受全部 11 provider 及 Sora/OpenAI、Doubao/VolcEngine 别名，并固定版本与源码哈希；绑定和版本删除共用引用保护。

绑定仅接受 `{key,version,factory,enabled}`；`go_fallback` 已删除，传入返回 400。新提交只走插件；无绑定或关闭时明确失败。历史任务使用 `POST /runtime/tasks/:task_id/adopt` 显式补齐身份，参数为数字数据库主键；事务内保留原状态/额度/ID，不发送供应商请求。绑定有任务引用时不可删除，可关闭以暂停。当前验证及限制见 统一宿主。

## 局部验证

已通过 `go test ./service/taskplugin ./controller/taskplugin -count=1 -v` 中 11 项无数据库测试。MySQL 测试使用仓库现有 `internal/testdb`，仅在 `TEST_MYSQL_DSN` 指定的测试服务器创建随机 `new_api_test_*` 数据库并清理：

```powershell
go test ./model -run '^TestTaskPluginStore' -count=1 -v
go test ./controller/taskplugin -run '^TestRootManagementMySQL$' -count=1 -v
```

2026-09-15 用户授权本地实库验证后，使用已有 `mysql:8.4.8` 镜像启动临时容器（1 CPU、1 GiB 内存限制，仅绑定 loopback 随机端口）。以上三项测试全部通过：

- 300,000 字节源码完整存取，大小写不同版本共存，无变更不递增 revision，预检和 SQL 唯一键失败均回滚。
- 8 路并发上传/激活后只有一个 active，revision 无丢失，拒绝多 active 状态。
- 真实 RootAuth 与 MySQL 用户记录验证：普通用户、管理员均被拒绝；Root 完成上传、查询、开关、dryrun、激活和删除；数据库降权后旧签名会话立即失权（本次 Redis 关闭）。

首次权限测试因测试账号的空 `aff_code` 触发唯一约束失败；手工为三个测试账号设置独立邀请码后，该测试重跑通过，未修改业务认证逻辑。预期的插件重复版本 SQL 错误属于回滚断言。测试后确认没有残留 `new_api_test_*` 数据库，并移除临时容器及其独立匿名卷。现有开发容器与业务数据未修改。

2026-09-15 用户继续授权处理集成阻塞后，手工修复旧迁移遗留问题：

- 删除 advancedcustom 中无人调用、依赖不存在的 `service.ConvertRequestByID` 的辅助函数，保留本地现有转换路径。
- 修复同包测试的错误嵌套、上游辅助函数名和转换器常量引用；恢复测试后发现 Responses 转 Chat 的流式请求漏设 `include_usage`，在现有分支补齐。非流式和上游不支持时不发送该参数，Claude/Gemini 路径保持原有行为。
- DeepSeek 余额代码补齐 `math` 导入，汇率改用本地 `GetEpayConfig().USDExchangeRate` 快照；测试 import 改为本地模块路径。

以下局部检查通过：`go test ./relay/channel/advancedcustom -count=1`（修正后通过包含全部 `TestAdaptor` 的定向命令复验）、`go test ./controller -run '^TestGetDeepSeekBalanceUSD$' -count=1`，以及 router 的 `TestStandardOAuthProvidersUseUnifiedCallbackRoute` / `TestTaskPluginManagementRoutesRequireAuthInAPIRouter`。新增路由集成测试确认管理入口挂入主 API 后仍拦截未登录访问。所有冲突均手工分析处理，没有自动选边或合并分支。

未运行全量 build、全量测试、应用服务启停、部署或业务库迁移；未验证 Redis 开启时的权限撤销延迟或真实多进程周期同步。路由包编译与注册检查不代表主程序启动或可部署性已验收。
