# 本地开发环境

> 定位：现行开发与行为约束。带日期的部署或验证段落保留为当时记录；分类入口见[文档导航](../README.md)。

本项目只保留一套名为 `new-api` 的 Docker Compose 开发环境。后端和基础设施在 Docker 中运行，前端使用 Rsbuild 开发服务器热更新。

Jev / TypeSafe 的可选独立协议中间件见 [SystemOne 独立中间件](../guides/systemone-proxy.md)。它通过现有 Chat 渠道复用网关，不需要数据库迁移或改动前端。

## 启动

在项目根目录运行菜单：

```powershell
.\menu.ps1
```

菜单包含本地开发、构建与部署、数据维护和平滑部署维护四组操作。启动完整开发环境会打开独立终端运行前端；在该终端按 Ctrl+C 停止前端并关闭终端后回到菜单，Docker 后端继续运行。其他操作在当前终端执行，完成或失败后按回车返回菜单。

统一发布菜单为：`6` 只读生成计划，`7` 完整发布，`8` 线上状态，`11` 仅准备不切换。镜像只构建一次，从冻结线上 baseline 新建独立克隆，完成同一候选镜像的真实升级验收后才上传与切换；开发实例不能代替门禁起点。详见 统一发布流程。单独开发构建可使用 `scripts/local.ps1 build`；前端检查统一经 `scripts/docker-check.sh web` 在 Docker/Linux 命名卷中执行。

生产发布需输入 `DEPLOY`，仅准备需输入 `PREPARE`，重置本地数据库需输入 `RESET`。`12–14` 为收尾/回切/恢复，须提供已验收发布 ID。兼容页面更新蓝绿切换，后台或数据库不兼容更新走维护流程。菜单里的刷新数据和重置仍沿用本地镜像；脚本集中在 `scripts/`：

```powershell
# 构建当前后端源码，启动 MySQL、Redis、后端和前端开发服务器
.\scripts\dev.ps1

# 沿用现有后端镜像
.\scripts\dev.ps1 -NoBuild

# 只启动 Docker 中的后端和基础设施
.\scripts\dev.ps1 -BackendOnly

# 删除唯一开发库并从最新安全快照重新初始化
.\scripts\dev.ps1 -ResetData

# 保留用户等本地数据，只刷新模型、供应商、能力、脱敏渠道和定价配置
.\scripts\dev.ps1 -RefreshSeed
```

普通启动不会删除或重建 MySQL 数据。只有显式使用 `-ResetData` 才会删除本地 Docker volume。

本地开发构建使用 `git describe --tags --always --dirty`，不可用时回退为 `v0.0.0`；统一发布使用应用内容指纹生成稳定版本，前后端一致，文档提交不触发重建。直接执行 Docker Compose 可通过进程变量 `BUILD_VERSION` 指定开发版本。

旧 `deploy.ps1` 和 `all` 发布入口已删除。开发启动使用 `dev.ps1`，停止/状态/手动构建使用 `local.ps1`，生产只使用 `release.ps1`。菜单确认只作用于菜单入口，直接执行 `release.ps1 run` 或 `dev.ps1 -ResetData` 会进入相应流程，需已有执行授权。

菜单支持 Windows PowerShell 5.1 和 PowerShell 7。菜单替身测试的历史用途是验证路由、确认取消和失败恢复；按当前规则，不在 Windows 宿主机直接执行检查脚本。

## Docker 检查入口

所有编译、测试、lint 和格式化均在 Docker 内执行；不把 Windows 源码目录 bind mount 给工具容器。在仓库根目录用 Git Bash 调用 `scripts/docker-check.sh`，它把跟踪文件及未忽略的未跟踪文件同步到 Linux 卷 `newapi-src`，复用固定工具链和缓存。

```bash
# 局部 Go 验证；会准备专用测试 MySQL，服务启停需已有授权
scripts/docker-check.sh go-test ./model -run TestUsageRefund -count=1

# 只同步工作区，不运行构建或测试
scripts/docker-check.sh sync

# 容器内格式化受影响的 Go 和 web 源文件并写回
scripts/docker-check.sh fmt
```

`go-test` 会将参数交给容器内 shell；正则含 `|` 等 shell 字符时保留内层引号，例如 `-run "'TestUsageRefund|TestBilling'"`。包路径和过滤条件应按本次改动限定。

`web` 会运行整个前端的类型、lint 和单元检查，`go` 会运行后端 vet、测试与编译，不带子命令会执行全部检查。构建、全量及其他重型验证须先说明成本并获授权。独立 RelayKit 模块的检查须先 `sync`，再在同一固定 Go 镜像和 Linux 卷内进入 `relaykit/` 执行；不能以根模块检查代替。

没有专门子命令的局部前端检查，先同步，再在固定 Bun 镜像中执行指定文件：

```bash
scripts/docker-check.sh sync
export MSYS_NO_PATHCONV=1
docker run --rm -v newapi-src:/src -v newapi-web-modules:/src/web/node_modules \
  -v newapi-bun-cache:/root/.bun/install/cache -w /src/web \
  oven/bun:1.4.0@sha256:5ff609364c049b54eb0ff560ec96319729a972078ef2c755d758f0c6ef89c2d6 \
  sh -c 'bun install --frozen-lockfile && bun test src/features/usage-refunds/lib/refunds.test.ts'
```

第一次安装依赖会产生下载成本；已有依赖使用持久缓存。本文只说明入口，不表示所有列出的检查已对当前工作区执行。

### 任务结构化定价的定向回归

`scripts/docker-check.sh pricing-editor-test` 在 Linux 卷运行 32 项定价单元测试、类型检查、局部 lint 及 31 个真实组件浏览器场景。新增场景覆盖单位价／Token 价原样保存、显式改为总金额，以及迁移面板确认、阻塞、过期预览；`web/tests/pricing-editor.tsx` 模拟 HTTP，不连接业务库或供应商。

后端按改动选择 `./setting/billing_setting` 的 `TestTaskPrice*`、`./service/taskplugin` 的 `TestTaskPriceMigration*`、`./model` 的 `TestModelPricingTaskRuleFormatUpgradeAndProvenance`，以及 `./relay` 的 `TestPluginMappedModelKeepsPricingAndIdentityMySQL`／`TestPluginTokenPricingSnapshotSubmissionMySQL`。通过 `go-test` 入口执行，MySQL 用例需已有专用容器启停授权，使用随机隔离库；本地固定源码解析覆盖全部 11 provider，但不等于供应商验收。

迁移测试涵盖停用绑定、冲突／孤立阻塞、固定历史源码、摘要过期、写入回滚和确认幂等。任务提交测试检查选中结构化规则、客户分组快照与改价后单次发送。发布脚本只对精确字符串 → amount 格式升级放行，仍拒绝优先级切换；定向通过不替代全线上定价升级门禁。

## 任务完整 HTTP 本地验收

`scripts/task-http-acceptance.sh <已构建的候选镜像>` 验证实际应用及 worker，通过登录会话、API Token、正式 `/v1/videos`、`/v1/responses`、`/v1/tasks/doubao` 和 Root 管理接口进入业务代码。构建和服务启停必须事先授权；镜像需要包含真实前端产物，不能使用 `docker-check.sh go-build` 的占位页面。测试驱动、浏览器和模拟供应商均在已有 `new-api-release-ui:pw1.58.2` 工具镜像运行，不在宿主机执行 Node，也不 bind mount Windows 源码。

入口经 `docker-check.sh sync` 将脚本同步到 Linux 卷，每次创建独立的 `newapi-task-http-*` MySQL、Redis、应用、worker 和模拟供应商。网络为 Docker internal，无宿主端口映射；只使用合成用户、价格和凭据。模拟服务固定返回任务 ID／用量／结果，应用 API、资金事务及轮询不使用替身。

场景包括匿名／普通用户权限拒绝，WAN／Token／固定总金额迁移前后真实扣费对照，过期迁移预览，提交后改价与幂等重放，失败退款，重启后在途结算，以及浏览器真实会话读取、修改、保存并再次请求核对新价。断言检查 MySQL 中的任务、私有价格快照、用户和 Token 剩余额度，以及模拟供应商的提交次数。

成功或失败均保留 `.releases/validation/newapi-task-http-*/` 的报告、日志、镜像身份和截图，并停止本次环境。证据卷及数据库卷保留用于排查；私有状态文件含合成测试 Token，不应作为公开附件。该验收覆盖合成场景，不替代冻结线上 baseline 的全部定价升级验收，不自动推送、发布或推进上游游标。

### 2026-10-04 新增整站场景（已通过同候选演练）

脚本增加 Responses sync/background/stream/stream+background、成功 GET、同键重放、流式断连后查询、失败只退一次、原名/别名 Token 权限，以及通用任务入口代表。请求经过真实 TokenAuth/Distribute/RelayTask；测试只读取数据库断言，通过模拟供应商 `/control/finish` 释放结果，由真实 worker 完成结算，不直接调用资金函数或改写任务终态。重启前同时保存 Video 与 Responses 在途任务，重启后核对快照、一次发送和钱包/Token 额度。

入口先把传入镜像解析为不可变 `sha256` ID，migrate/web/worker 均用同一 ID；报告绑定该 ID 与 `scripts.sha256` 来源摘要，每阶段校验实际脚本内容。缺少新增协议、权限或重启检查时，不允许仅运行 browser-only 就把报告标 completed。输出仍是独立合成样本，不能作为生产 baseline 全价格验收。

定向阶段的 5 项脚本断言通过后，已在候选 `sha256:918794cecf909436f4f2bb7af0366ef159d61ab0bd44119fccc9ddc31942cb9d` 执行完整脚本：26 项检查通过、模拟供应商收到 20 次提交，报告 `completed=true`。记录位于 `.releases/validation/newapi-task-http-20261004-181753/`；同一镜像的独立 baseline 克隆 11 项升级门禁亦通过，联合绑定见 `release-20261004-182422-b2e96106/combined-rehearsal.json`。浏览器截图已检查，测试服务均已停止。

首次尝试在脚本清单校验处失败：MSYS 的 `sha256sum` 默认二进制标记与 Linux 清单空格格式不同，实际文件摘要一致。入口现在固定 `--binary` 并仅规范化清单标记，仍逐文件核对内容摘要；原失败证据保留。部署脚本指纹变化后已重新执行正式门禁，应用镜像复用、未重建。详见升级演练结果。

### 2026-10-04 早期 15 项验收结果（历史候选）

基于提交 `0f6b9f3eb` 构建完整应用，最终一轮 15 项检查通过，模拟供应商实际收到 10 次任务提交。测试账号使用分组倍率 1、500000 quota／USD；任务请求、查询、管理操作均通过真实 HTTP，浏览器通过真实登录表单建立会话，未模拟应用 API。

| 场景 | 预扣 quota | 完成 quota | 结果 |
| --- | ---: | ---: | --- |
| WAN 单价 USD 0.000003，提交 5 秒、完成 7 秒，720P | 10 | 14 | 迁移前后相同 |
| Token 系数 0.5，完成 1000 Token | 125000 | 500 | 迁移前后相同 |
| 固定总金额 USD 0.00001 | 5 | 5 | 迁移前后相同 |
| 页面将 WAN 单价改为 USD 0.000004 后的新请求 | 20 | 28 | 页面保存、刷新回读和后端实际扣费一致 |

每笔同时核对任务额度、用户钱包余额和 API Token 剩余额度。额外通过匿名／普通用户的管理权限拒绝、过期预览不改价、提交后改价不影响已有快照、重复幂等键不再次发送、失败只退款一次、停止应用／worker 后重新运行 migrate 并恢复在途结算。失败任务重放应返回 `409 plugin_submission_failed`，不会用同一键创建新任务。

初次编排的 migrate 节点名、合成密码长度、失败重放状态码和浏览器登录准备均已修正；单独恢复浏览器环境时，模拟供应商重用旧任务 ID 触发宿主身份冲突保护，改为唯一 ID 后在全新隔离环境完整复验通过。应用业务代码未为这些 fixture 问题修改。

本次只覆盖 Alibaba／Doubao 两个内置插件和三类合成价格，未运行真实供应商、线上全部价格 baseline 升级、并发压力或全部 provider 的完整 HTTP 验收。全部测试容器已停止，最终原始证据在本机忽略目录 `.releases/validation/newapi-task-http-20261004-105815/`；报告含 15 项结果、镜像身份、真实页面截图和容器最终状态。

## 任务价格核对表单局部验收

从仓库根目录用 Git Bash 执行 `scripts/docker-check.sh task-review-test`。沿用 Linux 源码卷、固定 Bun 和已有 Playwright 镜像，运行类型检查、核对表单局部 lint，再转译隔离组件页并验证五个浏览器场景：Token 快照只读／已就绪／非法快照、旧固定价格及缺失历史价格。HTTP 由 fixture 模拟，不连接应用、数据库或供应商，不构建整站，也不会自动构建缺失的浏览器镜像。

## 计费编辑器局部验收

从仓库根目录用 Git Bash 执行 `scripts/docker-check.sh pricing-editor-test`。入口先将工作区同步到 Linux 命名卷，再在固定 Bun 1.4.0 镜像中执行时间规则/公开摘要/编辑器状态定向测试、前端类型检查和相关文件 ESLint；随后转译独立组件测试页，用已有 `new-api-release-ui:pw1.58.2` 镜像运行真实 React/Radix 和 `ModelRatioDialog` 保存回调验证。

测试不构建整站，不启动业务服务，不连接数据库或外部 API；临时 HTTP 服务只监听测试容器自身的 loopback，浏览器容器使用 `--network none`，不开放宿主端口。没有新增前端依赖。若缺少浏览器工具镜像，入口会报错，需另行授权 `ui-image` 构建；不会自动构建镜像。

测试源为 `web/tests/pricing-editor.tsx` 和 `scripts/pricing-editor.test.cjs`，临时浏览器产物只写源码卷。该局部检查不替代发布 baseline 克隆升级门禁。

## 页面资源加载与弱网恢复

前端保留内容哈希及生产路由拆包。HTML 内联 `web/bootstrap/resource-recovery.js`，其提示不依赖应用 JS/CSS 或外部字体；Rspack 通过 `web/resource-recovery-plugin.ts` 将异步 JS/CSS 下载接入同一恢复流程。

- 下载失败后按 1、2、4 秒间隔最多自动重试三次；同一 chunk 的并发加载共享恢复过程。
- 浏览器报告离线时暂停下载，提示等待联网；收到联网事件后恢复下载。联网提示不代表目标站点已可达，实际下载仍受重试上限约束。
- 重试用尽后显示“重试加载”和“刷新页面”。保留未完成的 import Promise，点击重试可在原页面继续加载，避免懒加载组件缓存失败。不会自动刷新、清空草稿或重放业务 API。
- 首次入口脚本/样式加载失败或启动超过 20 秒时，显示独立基础提示；入口失败需用户主动刷新。刷新可能丢失未保存内容，提示中会说明。
- 程序异常及普通 API 请求错误不进入资源重试。缺失的 `/static/`、`/assets/` 文件返回 404 和 `Cache-Control: no-store`，不再返回 SPA HTML；普通页面路径仍回退到需校验缓存的 HTML。
- 字体使用已有本地 Public Sans 资源及系统中文字体，不请求 Google Fonts。

永久缺失的旧哈希文件仍需要刷新取得新版本；保留历史资源和改善公网线路属于发布/基础设施的后续工作。本机制不提供完整离线应用或离线业务数据缓存。

局部验证须先使用 `scripts/docker-check.sh sync` 同步到 Linux 卷，在固定工具链容器内运行 `bun test src/lib/resource-recovery.test.ts`、相关 ESLint/TypeScript 检查和 `go test ./router -run '^TestWebMissingAssetsDoNotReturnHTML$'`。已有 `new-api-release-ui:pw1.58.2` 镜像可运行 `node web/scripts/resource-recovery.browser.cjs`，用拦截的测试资源验证重试、断网恢复、草稿保留及入口兜底，无需启动应用服务或读取业务数据。生产拆包集成验收需另行授权运行前端构建；开发模式未启用路由自动拆包，不能代替该项验收。

## 地址与账号

| 服务 | 地址或账号 |
| --- | --- |
| 前端开发服务器 | `http://127.0.0.1:3001` |
| 后端 API | `http://127.0.0.1:3000` |
| 管理员 | `root` / `123456789` |
| MySQL | `127.0.0.1:3307`，数据库 `new_api`，`root` / `123456` |

首次初始化前可通过 `NEW_API_LOCAL_ADMIN_PASSWORD` 修改本地管理员密码。已有数据库不会被普通启动改密。

后端启动时会对 `SQL_DSN` 指向的服务器执行版本与产品身份校验；单独配置 `LOG_SQL_DSN` 时也执行同样校验。两者仅接受 Oracle MySQL 8.x，明确拒绝其他 MySQL 主版本以及 MariaDB、Percona、TiDB、Vitess、OceanBase、PolarDB、AliSQL 等兼容实现；未配置 `LOG_SQL_DSN` 时日志复用主数据库连接。

## Go 数据库测试

Task Plugin 离线开发使用[总览中的固定 Go 容器命令](../plugin-api/README.md#入口)执行 lint 和 fixture 回放，无需数据库或启动业务服务。11 provider 已走统一插件提交/轮询与本地结算，旧 Go 任务适配器已物理删除；新环境默认总开关关闭，渠道需要显式绑定；已有部署范围见插件生产合同。旧 Alibaba/Wan Go 影子观察源已删除，保留管理与离线对照工具。使用范围和示例见 [Task Plugin 开发与管理](../plugin-api/README.md)，接口见 [管理与持久化](../plugin-api/management.md)，身份与轮询见 [任务身份与查询准备](../plugin-api/task-identity.md)。

内部 Task 插件身份、源码快照/引用事务、生产绑定与查询准备的局部及 MySQL 验证见 [任务身份与查询准备](../plugin-api/task-identity.md)。本地生产路径验证应使用 httptest；这些测试使用随机独立数据库，不应在开发业务库手工构造插件 Task 或把候选查询结果直接写入额度。

Alibaba 生产提交、落库补偿与轮询回归命令见 生产生命周期。全量 Go 测试未设置 `TEST_MYSQL_DSN` 时会跳过数据库用例；不能用该结果代替隔离 MySQL 专项测试。测试 DSN 必须显式指向本地测试服务器，不使用线上或开发业务库配置。

数据库集成测试只支持 Oracle MySQL 8.x。`scripts/docker-check.sh go-test` 自动准备专用 `newapi-check-mysql`（MySQL 8.4.8），并将测试 DSN 注入容器；不要使用上文开发账号、开发库或生产连接串。

```bash
scripts/docker-check.sh go-test ./model -run TestUsageRefund -count=1
```

测试 helper 为每个用例创建 `new_api_test_` 前缀的随机隔离库，cleanup 只删除该用例创建的库。测试账号需要创建和删除数据库的权限。脱离统一入口且没有 `TEST_MYSQL_DSN` 时，本地数据库用例可能跳过，CI 缺少该变量会失败；跳过结果不能作为实库验收。

## 安全快照

首次初始化、`-ResetData` 和 `-RefreshSeed` 从 `.ai-governance/workspace/snapshots/` 读取最新的三类本地快照：

- `new-api-dev-sanitized-*.sql`：模型、供应商、能力映射，以及 `channels`、`options` 表结构。
- `remote-pricing-options-*.sql`：定价和分组相关的 `options` 白名单。
- `remote-sanitized-channels-*.sql`：脱敏渠道结构。

脱敏渠道仅根据能力映射保留渠道 ID、模型/分组覆盖和调度权重，协议类型统一重置为通用类型；渠道被强制禁用，Key、Base URL、组织信息、Headers、代理、参数覆盖、余额及扩展设置均替换为空值或本机黑洞地址。安全快照不包含生产用户、Token、日志、支付、OAuth、SMTP 或任何真实上游凭据。

## 容器

Docker Desktop 中应只有以下一组：

```text
new-api
|- new-api-dev
|- new-api-dev-mysql
`- new-api-dev-redis
```

停止环境但保留数据：

```powershell
docker compose -f docker-compose.dev.yml down
```
