# 邮件格式与投递排查

> 定位：现行使用与配置指南。带日期的部署或验证段落保留为当时记录；分类入口见[文档导航](../README.md)。

注册验证码、密码重置和其他调用 `common.SendEmail` / `common.SendEmailWithAttachments` 的邮件共用 SMTP 配置与发送逻辑。

## 邮件格式

- 所有邮件声明 `MIME-Version: 1.0`。
- 普通邮件使用 `text/html; charset=UTF-8`；HTML 正文使用 Base64 传输编码，按最多 76 个字符折行并使用 CRLF。解码后保持调用方传入的正文和链接原始字节。
- 带附件邮件使用 `multipart/mixed`，第一段是同样经过 Base64 编码的 HTML，后续附件也使用 Base64。附件文件名和内容类型沿用既有行为，空类型回落到 `application/octet-stream`。
- 非 ASCII 主题使用标准 RFC 2047 encoded-word 编码，发件人显示名使用标准邮箱地址序列化。调用方仍可用分号分隔多个收件人，实际 `To` 邮件头使用逗号分隔。
- `SMTPArchiveBcc` 只加入 SMTP 收件人列表，不出现在 To/Cc/Bcc 邮件头；归档地址被 RCPT 拒绝仍不影响主收件人发送。

当前保留单段 HTML 正文，不自动生成 `multipart/alternative` 的纯文本版本。SMTP 认证、TLS 选项、验证码有效期和密码重置流程不因本次格式规范化改变。

## 可配置的账户邮件模板

Root 管理员在「系统设置 → 集成 → 邮件模板」选择「密码重置」或「额度提醒」，可修改主题和 HTML 正文、使用示例数据预览、保存或恢复默认。页面沿用现有模板列表，无需单独启用。首次加载支持此功能的程序后，后续文案调整只需保存配置，不再需要改代码或重新构建。

| 模板 | Key | 可用变量 | 正文必填变量 |
| --- | --- | --- | --- |
| 密码重置 | `password_reset` | `system_name`、`reset_url`、`valid_minutes` | `reset_url`、`valid_minutes` |
| 额度提醒 | `quota_warning` | `system_name`、`remaining_quota`、`topup_url` | `remaining_quota`、`topup_url` |

变量写为 `{{变量名}}`，两侧允许空白。新模板的变量均为纯文本：主题直接填入，正文自动 HTML 转义；不要预先给变量编码，也不要把变量放进脚本、样式或未加引号的属性中。链接可写为 `<a href="{{reset_url}}">{{reset_url}}</a>`。主题不要求包含系统名称，可以自行命名；额度数值包含当前设置的显示单位。

保存和预览会拒绝未知变量、未闭合/嵌套的双花括号、仅含空白的字段、主题换行及正文缺少必填变量。校验检查占位符，不检查自定义 HTML 的布局、链接是否可见或投递评分；编辑后需查看预览。工单和支付模板保留原有 HTML 片段变量语义。

主题与正文分别存入 `EmailTemplate.<key>.subject` / `EmailTemplate.<key>.body`。空字符串表示该字段使用默认值，这两个新模板的空字段预览也使用默认值。页面保存、恢复默认均通过现有 Option 批量事务更新两个字段，进程内成对发布。接受保存的实例随即生效，其他实例与独立 worker 沿用周期性 Option 同步，非跨实例即时更新。不存在新的数据库表或迁移。

### 默认密码重置正文

密码重置默认主题为 `{{system_name}}密码重置`，默认正文如下。完整链接仅显示一次，链接文字与点击目标一致，既可点击也可复制。

```html
<p>您好，我们收到了您的账户密码重置请求。</p>
<p>请在 {{valid_minutes}} 分钟内打开以下页面继续操作：</p>
<p><a href="{{reset_url}}">{{reset_url}}</a></p>
<p>如果您未发起此请求，请忽略本邮件。</p>
```

有效期复用 `VerificationValidMinutes`，链接生成、令牌存储/消费及账户查询保持既有流程。新文案通过过一次 SmarterMail 网页投递对照：同一收件人、主题和无效测试链接，原正文退信，新正文收到。这不表示已确定评分规则，也不保证每个收件人或程序发送都能成功；需要部署后的实际重置邮件继续验证。

### 默认额度提醒正文

默认主题为 `{{system_name}}账户余额提醒`，正文简洁展示当前余额及账户页面：

```html
<p>您好，您的账户当前剩余额度为 {{remaining_quota}}。</p>
<p>您可以在以下页面查看账户并补充余额：</p>
<p><a href="{{topup_url}}">{{topup_url}}</a></p>
```

此模板只用于用户选择邮件（或未指定通知方式时的默认邮件）渠道。通知阈值、显示额度来源、收件邮箱选择和限频保持原有逻辑；Bark、Gotify、Webhook 保留原内容。充值地址沿用 `PaymentReturnURL("/console/topup")`。新默认额度正文尚未验证真实收件结果。

## “已发送”与“已收到”

应用提交 SMTP 成功、邮件系统显示已发送，以及归档副本可见，都不代表最终收件服务器已接受，更不代表进入收件箱。邮件系统可能在提交成功后异步重试或退信。

密码重置接口为避免泄露账户是否存在，对未找到唯一账户或发送失败也返回统一成功响应；实际发送错误记录在应用日志中。Redis 等验证码存储失败仍按现有行为返回 503。不要把页面成功提示作为投递凭证。

排查时按时间、实际收件人和 Message-ID 关联以下证据，分享前遮蔽完整邮箱、重置令牌和 SMTP 凭据：

1. **应用日志**：查 `failed to send password reset email` 或 `skip password reset email`，确认是否进入发送阶段。
2. **发件系统**：查看 Delivery 日志、队列和退信，确认实际拒收节点及完整 SMTP 返回信息。SmarterMail 普通邮箱账号通常只能看到退信，服务器日志需系统管理员或服务商提供。
3. **返回结果**：`4xx` 通常是临时失败，需查后续重试；`5xx` 是永久拒绝。例如 `550 Message rejected as spam (score: ...)` 表示反垃圾拒收，需要评分规则明细，不能仅凭收件地址认定拦截来自目标邮箱服务。
4. **收件端**：确认最终收件服务器接受后，再查收件箱、垃圾箱、隔离区和收件规则。

有单独归档邮箱时，从归档核对当次原始主题、正文和邮件头，从发件邮箱核对退信；按原始发送时间、收件人和 Message-ID 配对。归档收到只证明副本投递，不代表原收件人收到。历史额度提醒使用“您的额度即将用尽”及 `/wallet` 链接时，应按当时版本分析，不能直接作为当前程序或默认模板的投递结果。

注册邮件能收到而重置邮件被拒，可能是正文、链接、邮件格式和域名信誉共同影响评分。网页发送与程序发送使用不同的邮件生成方式；网页测试成功不等于程序原邮件已经修复。规范 MIME 是格式兼容性修正，不保证降低垃圾评分或成功投递。

## 局部验证

以下局部检查从仓库根目录用 Git Bash 执行；专用测试 MySQL 的启停按项目授权规则处理，详见[Docker 检查入口](../development/local-development.md#docker-检查入口)。

```bash
scripts/docker-check.sh go-test ./common -run "'TestSendEmailMIME|TestSendEmailDoesNotMutateSettingsForFallbackFrom|Test.*SMTP|Test.*NTLM'" -count=1 -timeout=60s
scripts/docker-check.sh go-test ./constant ./model ./service ./controller -run "'TestAccountEmailTemplate|TestRenderPasswordResetEmail|TestLegacyEmailTemplateHTMLVariables|TestEmailTemplateOptionsRejectInvalidBeforePersistence|TestGetEmailTemplateObservesAtomicSubjectBodyUpdates|TestIsBulkOptionAllowed'" -count=1 -timeout=60s
```

MIME 回归仅启动本机临时 SMTP 接收器，捕获公共发送函数实际发出的 envelope 和 DATA，并用标准库解析邮件头、正文和附件。覆盖中文长主题、含逗号的中文显示名、多收件人、盲送不泄露、长 HTML/链接/换行完整还原、二进制与空附件及 Base64 行宽；不连接业务数据库或真实邮件服务器。

模板回归覆盖必填/未知/非法变量、自定义主题与正文、清空回落默认、预览一致性、旧 HTML 变量兼容、主题与正文成对读取、模板保存白名单及无效单项/批量配置在持久化前拒绝。HTML 解析器检查默认重置邮件唯一链接、可见文字与目标一致、特殊字符转义及动态有效期；不发送邮件，不连接数据库。真实数据库保存/恢复、后台页面交互与部署后的实际收件仍需环境验收。

用户已反馈将新版文案填入自定义模板后测试成功；此为用户收件反馈，不代表所有收件人及两类邮件均已完成投递验收。修正位于公共发送函数，后台通知进程也必须加载新版程序才能采用新编码；使用独立 worker 的部署需遵循平滑部署文档的后台升级要求。
