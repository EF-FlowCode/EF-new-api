# Task Plugin 开发与管理

当前内置 10 个官方插件及本地 Happyhorse，共 11 个。全部 11 个 provider 已接统一 Task 宿主，支持 JSON/multipart，取消 Go fallback。新环境默认总开关关闭、无生产绑定。eflow 已于 2026-09-17 部署 v5，并为现有 Happyhorse 渠道开启固定版本绑定；真实供应商验收仅覆盖记录中的一条文生视频链路，其他 provider 不因接入宿主而自动获得生产绑定或验收结论。插件配置页面、市场及任意新根的动态 HTTP 分发仍未接入。2026-10-04 工作区另接 JSON `/v1/tasks`、ali/doubao/kling/jimeng 固定声明入口与 Suno submit；范围和限制见通用与受控原生入口。2026-10-04 工作区接入固定绑定的公共 Responses 三模式和 Video 解码／渲染，支持范围、鉴权内容链接及未发布状态见统一宿主。任务日志已提供 Root 人工核对入口。

插件负责协议与 usage facts，Go 负责安全 HTTP、本地计费、Task/源码/引用事务和原子终态 CAS。WAN s2v/Wan3 支持完成用量、提交价格快照与持久待结算；Happyhorse 也通过插件运行；11 个旧 Go 任务目录及接口已物理删除，源码历史可从 Git 查阅；全部已迁平台的历史任务须经 Root 显式接管。未知受理可持久恢复，预扣与提交意图同事务；客户端幂等及 Root 预览/确认见 [恢复与人工核对](recovery.md)。其他接口见 [管理与持久化](management.md)，当前能力、破坏性变化与验证见 统一宿主。

## 入口

在仓库根目录用 Git Bash 执行。先同步到 Linux 命名卷，再在与项目一致的固定 Go 镜像中运行离线 CLI；不在 Windows 宿主机执行 `go run`。

```bash
scripts/docker-check.sh sync
export MSYS_NO_PATHCONV=1
docker run --rm -v newapi-src:/build -v newapi-gomod:/go/pkg/mod -v newapi-gocache:/root/.cache/go-build \
  -w /build golang:1.26.9-alpine@sha256:3082400e369fa24d5fc60bca20edab3f6d604e0c5a690ec66b295eff4dd87ade \
  sh -c 'go run ./cmd/plugin lint ./plugins/tasks/alibaba/plugin.js && go run ./cmd/plugin test ./plugins/tasks/alibaba/plugin.js --fixture ./plugins/testdata/alibaba-wan.json'
```

该 CLI 只运行脚本转换函数，不执行返回的 HTTP 请求描述，不读取渠道密钥或连接业务数据库。fixture 使用假凭据；首次编译可能下载 Go 依赖。

局部验证统一使用下列入口。`go-test` 会准备专用测试 MySQL；服务启停按项目授权规则执行。只做离线 CLI 回放时无需启动数据库。

```bash
scripts/docker-check.sh go-test ./pkg/jsplugin ./plugins ./cmd/plugin -count=1
```

`pkg/jsplugin` 提供 `CompilePlugin`、`Registry`、`ReplayFixture` 和 `RunCLI`。`plugins` 包嵌入 10 个官方 provider 与本地 Happyhorse，导入该包时向进程内 `DefaultRegistry` 注册；它不注册 Gin 路由。独立 CLI 按指定路径读取一个脚本，不加载整套内置插件。

## 编写与调试

每个插件是一个同步 ECMAScript module，声明 `meta` 并实现请求构造、响应解析、任务查询和用量提取等 hook。协议签名见 [v1.d.ts](v1.d.ts)，声明结构见 [v1.schema.json](v1.schema.json)，完整上游参考见 [v1.md](v1.md)。三份文件保留固定上游协议参考；其中完整公共协议、动态 native 路由、Artifact 和管理页面等声明不等于本地已实现。实际能力以统一宿主为准。

fixture 使用 `{unixNow?, cases: [{name?, hook, member?, path?, args, expected?, expectedError?}]}`。`path` 可指定 `protocols` 下多层 hook；`member` 与 `path` 不能同时使用。每例必须有期望结果或错误；运行时比较 JSON 结构，遇到不一致返回非零退出码。

先添加固定输入和期望输出，再修改 provider 脚本；保留模型别名、错误响应、空/零用量和协议转换案例。脚本更新后需要重新验证，修改源码必须增加版本，使用 Root API 上传和激活。

## 运行边界

- Sobek 运行时默认单次调用超时 5 秒、每个引擎并发 8；关闭模块导入、source map 加载及 async/await，不提供 `fetch`、文件系统、环境变量或 `require`。
- 脚本只生成数据和请求描述。通用宿主提供请求/响应及 usage 校验；全部 11 provider 提交和轮询使用复用本地 SSRF 策略的 HTTP 执行器，全部 provider 的代表路径已有本地测试，真实供应商范围以统一宿主中的记录为准。旧 Go 在线影子观察源已删除，保留管理和离线工具。
- 运行时是进程内隔离，不提供硬内存上限。只回放经过审阅的脚本；`console.log` 不会自动脱敏，fixture 不放真实凭据。
- JSON 使用 eflow 现有 `encoding/json`，没有增加全局可替换 codec。保留非法 UTF-8、深度、大小和失败批次不可发布测试。
- `jsplugin.DedicatedChannelType=61`（与上游 `ChannelTypeTaskPlugin` 一致）仅用于离线协议校验；2026-10 起本地阿里媒体渠道已挪到 1001–1004，不再占用 61。生产渠道表格和路由尚未注册此编号。
- `jsplugin.MaxTaskDurationSeconds=3600` 来自官方 v1 用量样例上限，约束插件元数据及宿主 usage facts 校验，不改变本地 Go 任务时长规则。

## 来源与状态

基准是官方 `QuantumNous/new-api` 的固定提交 `69a50029819a26c53e6babd276d49cfe2f8880ad`，本地迁移起点是 `8486ab926`。

直接导入 `pkg/jsplugin`、`plugins/embed.go`、10 个 `plugins/tasks/*/plugin.js` 和协议参考。Go import 改为 `eflow.local/new-api`。JS 文件按固定快照保留，包含 Wan3 后续图片/视频能力；脚本能通过离线检查不表示对应模型在线可用或已经与本地计费等价。

宿主差异与后续阶段见 迁移记录。旧的独立修复计划已暂停。
