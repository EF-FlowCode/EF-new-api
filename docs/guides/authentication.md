# 控制台认证与安全（eflow）

> 2026-10-16 认证与安全专题（路线 B：保留 Redis 会话，按意图移植上游安全能力，数据结构与接口契约对齐上游）。处置台账见 上游逐项处置。尚未构建或部署。

## 登录态

- 浏览器：gin 会话存 Redis（`session:<sid>`，cookie 只带签名 SID），30 天有效；请求头必须带 `New-Api-User: <id>`。未启用 Redis 时回退整值签名 cookie。
- 子站：`e-flowcode-auth` HS256 JWT（`uid`/`iat`/`exp`，v2 增加 `sid`=主会话公开 ID、`ver`=2；子站只校验 `uid`/`exp`）。主站 `GetSelf` 在 cookie 缺失、过期或属于其他会话时重签；主会话失效的任何控制台请求会清掉它。
- 登录设备：会话内记录 `login_method`、`login_ip`、`login_ua`、`login_at`；`session:active:<sid>` 记最近活跃（每分钟最多一次）。接口 `GET /api/user/sessions`、`DELETE /api/user/sessions/:sid`、`POST /api/user/sessions/revoke-others`（只允许浏览器会话；对外只给 SID 的 HMAC）。

## 访问令牌（PAT）

- 格式 `nap_` + 43 位字母数字，`Authorization: Bearer nap_...`；库里只存 SHA-256（`user_access_tokens.token_hash`，也是审计 `token_ref`）。每人最多 20 个，默认 30 天，可选永不过期。
- 带范围令牌的 `New-Api-User` 可省略，带了必须与令牌所有者一致。成功响应带 `X-Access-Token-Expires-At`（秒，0=永不过期，eflow 扩展）。
- 路由规则：`middleware/access_token_routes.go`（与上游同名路由）+ `access_token_routes_eflow.go`（eflow 独有）。`scope` 需令牌持有该权限；`any` 任何有效令牌；`session` 拒绝一切令牌。未声明的路由一律拒绝。`router/access_token_scope_test.go` 遍历全部控制台路由，新增路由必须同时声明规则。
- 权限目录：`service/access_token_scope.go`，按动作写死最低角色（无 Casbin）。客服(5)/财务(7)可授予 `user:read`、`billing:read`、`ticket_admin:*` 与个人权限；管理员(10)可授予除 `channel:secret_view` 与 system 组外的全部；Root 可授予全部。日志导出（`/api/log/export*`）路由本身只允许 Root，令牌按 `log:read` 判断。用户降级后超出新角色的权限立即失效。
- 错误码：401 `access_token_invalid` / `ACCESS_TOKEN_EXPIRED` / `ACCESS_TOKEN_LEGACY_RETIRED`；403 `ACCESS_TOKEN_SCOPE_DENIED`（消息带所需 scope）/ `ACCESS_TOKEN_ROUTE_UNDECLARED` / `AUTH_SESSION_REQUIRED`。
- 管理接口（全部 session-only）：`GET/POST /api/user/access_tokens`、`GET /catalog`、`GET /scopes`、`PATCH /:id`、`DELETE /:id`、`DELETE /legacy`。创建、改权限、撤销要一次性验证凭证，改名不要。
- 旧版 `users.access_token`：本版本首次迁移后 30 天停用（`options.LegacyAccessTokenRetireAt`，迁移节点写入一次，设置接口只读；读不到按已停用处理）。过渡期内保持原有权限（仍需 `New-Api-User`，session-only 路由除外），不能再生成，只能撤销。
- 令牌请求记入 `access_token` 审计：写操作已有管理兜底审计时不重复，读操作每令牌每路由 10 分钟一条。

## 二次验证凭证

- `GET /api/verify/methods?scope=` 返回该操作允许的方式；`POST /api/verify {method, scope, context, code|password}`（2FA/备用码、密码、`session`）或 Passkey `POST /api/user/passkey/verify/begin {scope, context}` → `finish {flow_token, credential}` 得到 `{proof_token, expires_at, method, scope}`。
- 受保护请求带 `X-Security-Proof`。凭证 HS256（键由 `SESSION_SECRET` 按上游方式派生），60 秒有效，只能用一次（Redis `security_proof:<jti>`，Lua 原子取删），绑定用户、会话（浏览器 SID 或 `pat:<id>`）、scope 与上下文哈希。错误码 `SECURITY_PROOF_REQUIRED/EXPIRED/CONSUMED/INVALID/SCOPE_MISMATCH/CONTEXT_MISMATCH/METHOD_MISMATCH`、`SECURITY_ACTION_FORBIDDEN`。
- 方式策略：有 2FA/Passkey 时只能用它们；否则用密码；连密码都没有时用浏览器会话里 10 分钟内的登录（`session`，访问令牌不可用）。密码永远不能用于 `channel.key.read`。
- 访问令牌可以完成二次验证，但只能拿到其权限覆盖的 scope（`admin.user.*` 需 `user:write`，本人安全设置需 `account_security:write`，渠道密钥需 `channel:secret_view`），永远拿不到令牌管理类 scope。
- 接入的操作：渠道密钥；Passkey 注册（两步流程）/删除；2FA 开启（setup→enable 同一流程）/关闭/备用码；改密；注销；换绑邮箱、绑定微信与第三方 OAuth、解绑自定义 OAuth；令牌创建/改权限/撤销；管理员删除用户、启用/禁用、提升/降级、`set_role`（绑定目标角色）、创建管理员、重置密码或改邮箱、重置 Passkey、关 2FA、清除绑定。额度调整、只改分组/备注/显示名不需要。
- 站长决定：用访问令牌调用的启用/禁用免凭证（admin-api 自动化）。

## 登录二次验证

- 绑定了 TOTP 或 Passkey 的账号，密码、OAuth、微信、Telegram 登录都先进入待验证状态，返回 `{require_verification, flow_token, expires_at, methods}`（启用 TOTP 时 `require_2fa=true` 兼容旧前端）。
- 完成：`POST /api/user/login/verify {flow_token, code}` 或 `POST /api/user/login/passkey/begin {flow_token}` → `finish {flow_token, passkey_flow_token, credential}`；旧 `/api/user/login/2fa` 仍可用（只校验本会话待验证登录）。无密码 Passkey 登录强制 user verification。

## 审计

- 使用日志 `logs`、`audit_logs` 与日志目录下的应用日志文件都不能删除：`DELETE /api/log/`（按时间删除历史使用日志）与 `DELETE /api/performance/logs`（按数量/天数删除 `oneapi-*.log`）已下线，系统设置里对应的清理入口同步删除；`GET /api/performance/logs` 只读查看保留。
- `audit_logs`（LOG_DB，结构同上游）：登录、安全操作、管理写操作兜底（客服及以上角色，只记路由参数，不记请求/响应体）、访问令牌调用。`GET /api/audit`（管理员，`audit:read`）、`GET /api/audit/self`。非 root 看不到 root 与未知角色的事件。**永久保留，不得清理**（源码扫描测试守护）。

## 其他

- Passkey 多域名：设置的 RP ID 可填多个逗号分隔域名，请求取与访问域名匹配的那个；`passkey_credentials.rp_id` 记录注册域名，凭证只能在原域名使用；移除仍有凭证的域名会被拒绝。
- Telegram：设置 Client ID/Secret 后走 OpenID Connect + PKCE（`/api/oauth/state?provider=telegram` 返回授权地址，回调 `/oauth/telegram`），旧登录小部件接口返回 `TELEGRAM_LEGACY_RETIRED`；未配置时旧小部件照常。
- 换绑邮箱：已有邮箱且没有 2FA/Passkey 时还要填当前邮箱确认码（`POST /api/oauth/email/bind/old_code`）。不允许解绑账号唯一的登录方式。

## 内部脚本

`cmd/usage-refund` 接受 `nap_` 令牌（需 `usage_refund:read/write`，可省略 `NEW_API_ADMIN_ID`）。release-ui / task-pricing-preflight / billing-integration 在有 `user_access_tokens` 表时插入 2 小时有效的临时 Root PAT（权限取 `scripts/release-pat-scopes.json`，由 Go 测试与 Root 目录保持一致），结束删除。
