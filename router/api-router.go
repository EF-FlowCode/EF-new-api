package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	// Import oauth package to register providers via init()
	_ "github.com/QuantumNous/new-api/oauth"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

func SetApiRouter(router *gin.Engine) {
	apiRouter := router.Group("/api")
	apiRouter.Use(middleware.RouteTag("api"))
	apiRouter.Use(gzip.Gzip(gzip.DefaultCompression))
	apiRouter.Use(middleware.BodyStorageCleanup()) // 清理请求体存储
	apiRouter.Use(middleware.GlobalAPIRateLimit())
	{
		apiRouter.GET("/setup", controller.GetSetup)
		apiRouter.POST("/setup", controller.PostSetup)
		apiRouter.GET("/status", controller.GetStatus)
		apiRouter.GET("/uptime/status", controller.GetUptimeKumaStatus)
		apiRouter.GET("/models", middleware.UserAuth(), controller.DashboardListModels)
		apiRouter.GET("/status/test", middleware.AdminAuth(), controller.TestStatus)
		apiRouter.GET("/notice", controller.GetNotice)
		apiRouter.GET("/user-agreement", controller.GetUserAgreement)
		apiRouter.GET("/privacy-policy", controller.GetPrivacyPolicy)
		apiRouter.GET("/about", controller.GetAbout)
		//apiRouter.GET("/midjourney", controller.GetMidjourney)
		apiRouter.GET("/home_page_content", controller.GetHomePageContent)
		apiRouter.GET("/pricing", middleware.TryUserAuth(), controller.GetPricing)
		apiRouter.GET("/model_ranking", controller.GetModelRanking)
		perfMetricsRoute := apiRouter.Group("/perf-metrics")
		perfMetricsRoute.Use(middleware.HeaderNavModulePublicOrUserAuth("pricing"))
		{
			perfMetricsRoute.GET("/summary", controller.GetPerfMetricsSummary)
			perfMetricsRoute.GET("", controller.GetPerfMetrics)
		}
		apiRouter.GET("/rankings", middleware.HeaderNavModuleAuth("rankings"), controller.GetRankings)
		// 公共邮箱验证码供注册使用，继续保留 Turnstile 防滥发。
		apiRouter.GET("/verification", middleware.EmailVerificationRateLimit(), middleware.TurnstileCheck(), controller.SendEmailVerification)
		apiRouter.GET("/reset_password", middleware.CriticalRateLimit(), controller.SendPasswordResetEmail)
		apiRouter.POST("/user/reset", middleware.CriticalRateLimit(), controller.ResetPassword)
		// OAuth routes - specific routes must come before :provider wildcard
		apiRouter.GET("/oauth/state", middleware.CriticalRateLimit(), controller.GenerateOAuthCode)
		apiRouter.POST("/oauth/email/bind", middleware.UserAuth(), middleware.CriticalRateLimit(), controller.EmailBind)
		// Non-standard OAuth (WeChat, Telegram) - keep original routes
		apiRouter.GET("/oauth/wechat", middleware.CriticalRateLimit(), controller.WeChatAuth)
		apiRouter.POST("/oauth/wechat/bind", middleware.CriticalRateLimit(), controller.WeChatBind)
		apiRouter.GET("/oauth/telegram/login", middleware.CriticalRateLimit(), controller.TelegramLogin)
		apiRouter.GET("/oauth/telegram/bind", middleware.CriticalRateLimit(), controller.TelegramBind)
		// Standard OAuth providers (GitHub, Discord, OIDC, LinuxDO) - unified route
		apiRouter.GET("/oauth/:provider", middleware.CriticalRateLimit(), controller.HandleOAuth)
		apiRouter.GET("/ratio_config", middleware.CriticalRateLimit(), controller.GetRatioConfig)

		apiRouter.POST("/stripe/webhook", controller.StripeWebhook)
		apiRouter.POST("/creem/webhook", controller.CreemWebhook)
		apiRouter.POST("/waffo/webhook", controller.WaffoWebhook)
		// :env separates test vs prod URLs so the operator can register each
		// in Pancake's matching webhook slot; handler enforces env match.
		apiRouter.POST("/waffo-pancake/webhook/:env", controller.WaffoPancakeWebhook)

		// Universal secure verification routes
		apiRouter.POST("/verify", middleware.UserAuth(), middleware.CriticalRateLimit(), controller.UniversalVerify)

		userRoute := apiRouter.Group("/user")
		{
			userRoute.POST("/register", middleware.CriticalRateLimit(), middleware.TurnstileCheck(), controller.Register)
			userRoute.POST("/login", middleware.CriticalRateLimit(), controller.Login)
			userRoute.POST("/login/2fa", middleware.CriticalRateLimit(), controller.Verify2FALogin)
			userRoute.POST("/passkey/login/begin", middleware.CriticalRateLimit(), controller.PasskeyLoginBegin)
			userRoute.POST("/passkey/login/finish", middleware.CriticalRateLimit(), controller.PasskeyLoginFinish)
			//userRoute.POST("/tokenlog", middleware.CriticalRateLimit(), controller.TokenLog)
			userRoute.GET("/logout", controller.Logout)
			userRoute.POST("/epay/notify", controller.EpayNotify)
			userRoute.GET("/epay/notify", controller.EpayNotify)
			userRoute.GET("/groups", controller.GetUserGroups)

			selfRoute := userRoute.Group("/")
			selfRoute.Use(middleware.UserAuth())
			{
				selfRoute.POST("/risk_warning/ack", controller.AcknowledgeRiskWarning)
				selfRoute.GET("/self/groups", controller.GetUserGroups)
				selfRoute.GET("/self", controller.GetSelf)
				selfRoute.GET("/models", controller.GetUserModels)
				selfRoute.PUT("/self", middleware.CriticalRateLimit(), controller.UpdateSelf)
				selfRoute.DELETE("/self", controller.DeleteSelf)
				selfRoute.GET("/token", controller.GenerateAccessToken)
				selfRoute.GET("/passkey", controller.PasskeyStatus)
				selfRoute.POST("/passkey/register/begin", controller.PasskeyRegisterBegin)
				selfRoute.POST("/passkey/register/finish", controller.PasskeyRegisterFinish)
				selfRoute.POST("/passkey/verify/begin", controller.PasskeyVerifyBegin)
				selfRoute.POST("/passkey/verify/finish", controller.PasskeyVerifyFinish)
				selfRoute.DELETE("/passkey", controller.PasskeyDelete)
				selfRoute.GET("/aff", controller.GetAffCode)
				selfRoute.GET("/invitation_codes", controller.GetMyInvitationCodes)
				selfRoute.GET("/invitation_codes/quota", controller.GetMyInvitationCodeQuota)
				selfRoute.POST("/invitation_codes", controller.GenerateMyInvitationCode)
				selfRoute.GET("/topup/info", controller.GetTopUpInfo)
				selfRoute.GET("/topup/self", controller.GetUserTopUps)
				selfRoute.POST("/topup", middleware.CriticalRateLimit(), controller.TopUp)
				selfRoute.POST("/pay", middleware.CriticalRateLimit(), controller.RequestEpay)
				selfRoute.POST("/amount", controller.RequestAmount)
				selfRoute.POST("/stripe/pay", middleware.CriticalRateLimit(), controller.RequestStripePay)
				selfRoute.POST("/stripe/amount", controller.RequestStripeAmount)
				selfRoute.POST("/creem/pay", middleware.CriticalRateLimit(), controller.RequestCreemPay)
				selfRoute.POST("/waffo/amount", controller.RequestWaffoAmount)
				selfRoute.POST("/waffo/pay", middleware.CriticalRateLimit(), controller.RequestWaffoPay)
				selfRoute.POST("/waffo-pancake/amount", controller.RequestWaffoPancakeAmount)
				selfRoute.POST("/waffo-pancake/pay", middleware.CriticalRateLimit(), controller.RequestWaffoPancakePay)
				selfRoute.POST("/aff_transfer", controller.TransferAffQuota)
				selfRoute.PUT("/setting", controller.UpdateUserSetting)
				// 已登录用户复用发送逻辑和邮件限流，不再重复做人机验证。
				selfRoute.GET("/verification", middleware.EmailVerificationRateLimit(), controller.SendEmailVerification)

				// 2FA routes
				selfRoute.GET("/2fa/status", controller.Get2FAStatus)
				selfRoute.POST("/2fa/setup", controller.Setup2FA)
				selfRoute.POST("/2fa/enable", controller.Enable2FA)
				selfRoute.POST("/2fa/disable", controller.Disable2FA)
				selfRoute.POST("/2fa/backup_codes", controller.RegenerateBackupCodes)

				// Check-in routes
				selfRoute.GET("/checkin", controller.GetCheckinStatus)
				selfRoute.POST("/checkin", controller.DoCheckin)

				// Custom OAuth bindings
				selfRoute.GET("/oauth/bindings", controller.GetUserOAuthBindings)
				selfRoute.DELETE("/oauth/bindings/:provider_id", controller.UnbindCustomOAuth)
			}

			// eflow: 客服只读 — 让 role>=5 的客服能看到用户列表 / 搜索 / 单用户 / 用户画像 +
			// 充值订单只读，但所有 mutating 接口（POST/PUT/DELETE + topup/complete /
			// bindings / 2fa / manage）保持 admin only。因为 RoleAdminUser >=
			// RoleCustomerServiceUser，admin 自动满足 TicketStaffAuth，不会重复注册。
			staffReadRoute := userRoute.Group("/")
			staffReadRoute.Use(middleware.TicketStaffAuth())
			{
				staffReadRoute.GET("/", controller.GetAllUsers)
				staffReadRoute.GET("/search", controller.SearchUsers)
				staffReadRoute.GET("/:id", controller.GetUser)
				// eflow: 通用用户画像接口，跟工单 admin 画像同样数据结构，但走 user_id 不依赖
				// ticket。前端在使用日志、用户列表等地方点用户名都能调这条返回 sheet 数据。
				staffReadRoute.GET("/:id/profile-summary", controller.GetUserProfileSummary)
				// eflow: 充值订单列表 — 客服处理退款工单时常要查"用户最近哪笔单子付了多少"，
				// 给只读查询权。补单 (POST /topup/complete) 仍保留在 admin 组。
				staffReadRoute.GET("/topup", controller.GetAllTopUps)
				// eflow: users.group 列实际出现过的分组名，给用户列表筛选下拉用。
				// 区别于 /api/group/（倍率配置里的"应该有"），这条直读列实际取值。
				staffReadRoute.GET("/distinct_groups", controller.ListUserDistinctGroups)
			}

			adminRoute := userRoute.Group("/")
			adminRoute.Use(middleware.AdminAuth())
			{
				adminRoute.GET("/group_options", controller.ListUserGroupOptions)
				adminRoute.POST("/topup/complete", controller.AdminCompleteTopUp)
				adminRoute.GET("/:id/oauth/bindings", controller.GetUserOAuthBindingsByAdmin)
				adminRoute.DELETE("/:id/oauth/bindings/:provider_id", controller.UnbindCustomOAuthByAdmin)
				adminRoute.DELETE("/:id/bindings/:binding_type", controller.AdminClearUserBinding)
				adminRoute.POST("/", controller.CreateUser)
				adminRoute.POST("/manage", controller.ManageUser)
				adminRoute.PUT("/", controller.UpdateUser)
				adminRoute.DELETE("/:id", controller.DeleteUser)
				adminRoute.DELETE("/:id/reset_passkey", controller.AdminResetPasskey)

				// Admin 2FA routes
				adminRoute.GET("/2fa/stats", controller.Admin2FAStats)
				adminRoute.DELETE("/:id/2fa", controller.AdminDisable2FA)
			}
		}

		ticketRoute := apiRouter.Group("/ticket")
		ticketRoute.Use(middleware.UserAuth())
		{
			ticketRoute.POST("/", controller.CreateTicket)
			ticketRoute.GET("/self", controller.GetUserTickets)
			ticketRoute.GET("/self/:id", controller.GetUserTicket)
			ticketRoute.POST("/self/:id/message", controller.CreateUserTicketMessage)
			ticketRoute.PUT("/self/:id/close", controller.CloseUserTicket)
			ticketRoute.GET("/invoice/eligible_orders", controller.GetEligibleInvoiceOrders)
			ticketRoute.POST("/invoice/", controller.CreateInvoiceTicket)
			ticketRoute.POST("/refund/", controller.CreateRefundTicket)
			ticketRoute.POST("/attachment", middleware.CriticalRateLimit(), controller.UploadTicketAttachment)
			ticketRoute.DELETE("/attachment/:id", controller.DeleteTicketAttachment)
		}

		// 附件预览/下载独立路由：浏览器直接发起的 <img src> / <a href> 无法注入自定义
		// 请求头，因此使用 SessionAuth（cookie-only）而非 UserAuth。
		// 仅幂等的 GET 资源放在这里。
		ticketAttachmentDownloadRoute := apiRouter.Group("/ticket/attachment")
		ticketAttachmentDownloadRoute.Use(middleware.SessionAuth())
		{
			ticketAttachmentDownloadRoute.GET("/:id", controller.DownloadTicketAttachment)
		}

		// 工单后台路由：客服（role=5）可以访问，客服仅能处理分配给自己或所在组未分配的工单，
		// 这层可见性在 controller 里通过 ensureTicketAccessible 二次校验。
		ticketAdminRoute := apiRouter.Group("/ticket/admin")
		ticketAdminRoute.Use(middleware.TicketStaffAuth())
		{
			ticketAdminRoute.GET("/", controller.GetAllTickets)
			ticketAdminRoute.GET("/staff", controller.ListTicketStaff)
			ticketAdminRoute.GET("/:id", controller.GetTicket)
			ticketAdminRoute.GET("/:id/user-profile", controller.GetTicketUserProfile)
			ticketAdminRoute.POST("/:id/message", controller.CreateAdminTicketMessage)
			ticketAdminRoute.PUT("/:id/status", controller.UpdateTicketStatus)
			ticketAdminRoute.PUT("/batch-status", controller.BatchUpdateTicketStatus)
			ticketAdminRoute.PUT("/:id/assign", controller.AssignTicket)
			ticketAdminRoute.GET("/:id/invoice", controller.GetTicketInvoice)
			ticketAdminRoute.PUT("/:id/invoice/status", controller.UpdateInvoiceStatus)
			ticketAdminRoute.GET("/:id/refund", controller.GetTicketRefund)
			ticketAdminRoute.PUT("/:id/refund/status", controller.UpdateRefundStatus)
		}

		optionRoute := apiRouter.Group("/option")
		optionRoute.Use(middleware.RootAuth())
		{
			optionRoute.GET("/", controller.GetOptions)
			optionRoute.PUT("/", controller.UpdateOption)
			optionRoute.PUT("/bulk", controller.UpdateOptionsBulk)
			optionRoute.POST("/payment_compliance", controller.ConfirmPaymentCompliance)
			optionRoute.GET("/channel_affinity_cache", controller.GetChannelAffinityCacheStats)
			optionRoute.DELETE("/channel_affinity_cache", controller.ClearChannelAffinityCache)
			optionRoute.POST("/rest_model_ratio", controller.ResetModelRatio)
			optionRoute.POST("/migrate_console_setting", controller.MigrateConsoleSetting) // 用于迁移检测的旧键，下个版本会删除
			optionRoute.GET("/email_templates", controller.ListEmailTemplates)
			optionRoute.POST("/email_templates/preview", controller.PreviewEmailTemplate)
			optionRoute.POST("/email_templates/reset", controller.ResetEmailTemplate)
			optionRoute.POST("/test_email", middleware.CriticalRateLimit(), controller.TestSMTPEmail)
			optionRoute.POST("/waffo-pancake/catalog", controller.ListWaffoPancakeCatalog)
			optionRoute.POST("/waffo-pancake/pair", controller.CreateWaffoPancakePair)
			optionRoute.POST("/waffo-pancake/save", controller.SaveWaffoPancake)
		}

		// Custom OAuth provider management (root only)
		customOAuthRoute := apiRouter.Group("/custom-oauth-provider")
		customOAuthRoute.Use(middleware.RootAuth())
		{
			customOAuthRoute.POST("/discovery", controller.FetchCustomOAuthDiscovery)
			customOAuthRoute.GET("/", controller.GetCustomOAuthProviders)
			customOAuthRoute.GET("/:id", controller.GetCustomOAuthProvider)
			customOAuthRoute.POST("/", controller.CreateCustomOAuthProvider)
			customOAuthRoute.PUT("/:id", controller.UpdateCustomOAuthProvider)
			customOAuthRoute.DELETE("/:id", controller.DeleteCustomOAuthProvider)
		}
		performanceRoute := apiRouter.Group("/performance")
		performanceRoute.Use(middleware.RootAuth())
		{
			performanceRoute.GET("/stats", controller.GetPerformanceStats)
			performanceRoute.DELETE("/disk_cache", controller.ClearDiskCache)
			performanceRoute.POST("/reset_stats", controller.ResetPerformanceStats)
			performanceRoute.POST("/gc", controller.ForceGC)
			performanceRoute.GET("/logs", controller.GetLogFiles)
			performanceRoute.DELETE("/logs", controller.CleanupLogFiles)
		}
		ratioSyncRoute := apiRouter.Group("/ratio_sync")
		ratioSyncRoute.Use(middleware.RootAuth())
		{
			ratioSyncRoute.GET("/channels", controller.GetSyncableChannels)
			ratioSyncRoute.POST("/fetch", controller.FetchUpstreamRatios)
		}
		riskRoute := apiRouter.Group("/risk")
		riskRoute.Use(middleware.AdminAuth())
		{
			riskRoute.GET("/overview", controller.GetRiskCenterOverview)
			riskRoute.GET("/config", controller.GetRiskCenterConfig)
			riskRoute.GET("/detect-ip", controller.DetectRiskIP)
			riskRoute.PUT("/config", controller.UpdateRiskCenterConfig)
			riskRoute.GET("/groups", controller.GetRiskGroups)
			riskRoute.GET("/rules", controller.GetRiskRules)
			riskRoute.POST("/rules", controller.CreateRiskRule)
			riskRoute.PUT("/rules/:id", controller.UpdateRiskRule)
			riskRoute.DELETE("/rules/:id", controller.DeleteRiskRule)
			riskRoute.GET("/subjects", controller.GetRiskSubjects)
			riskRoute.POST("/subjects/:scope/:id/unblock", controller.UnblockRiskSubject)
			riskRoute.GET("/incidents", controller.GetRiskIncidents)

			// omni-moderation tab
			riskRoute.GET("/moderation/config", controller.GetModerationConfig)
			riskRoute.PUT("/moderation/config", controller.UpdateModerationConfig)
			riskRoute.GET("/moderation/overview", controller.GetModerationOverview)
			riskRoute.GET("/moderation/incidents", controller.GetModerationIncidents)
			riskRoute.GET("/moderation/incidents/:id", controller.GetModerationIncidentDetail)
			riskRoute.POST("/moderation/debug", controller.SubmitModerationDebug)
			riskRoute.GET("/moderation/debug/:id", controller.GetModerationDebugResult)
			riskRoute.GET("/moderation/categories", controller.GetModerationCategories)
			riskRoute.GET("/moderation/rules", controller.GetModerationRules)
			riskRoute.POST("/moderation/rules", controller.CreateModerationRule)
			riskRoute.PUT("/moderation/rules/:id", controller.UpdateModerationRule)
			riskRoute.DELETE("/moderation/rules/:id", controller.DeleteModerationRule)
			riskRoute.GET("/moderation/queue_stats", controller.GetModerationQueueStats)

			// enforcement layer (unified post-hit handling)
			riskRoute.GET("/enforcement/config", controller.GetEnforcementConfig)
			riskRoute.PUT("/enforcement/config", controller.UpdateEnforcementConfig)
			riskRoute.GET("/enforcement/overview", controller.GetEnforcementOverview)
			riskRoute.GET("/enforcement/incidents", controller.GetEnforcementIncidents)
			riskRoute.GET("/enforcement/counters", controller.GetEnforcementCounters)
			riskRoute.POST("/enforcement/users/:id/reset_counter", controller.ResetEnforcementCounter)
			riskRoute.POST("/enforcement/users/:id/unban", controller.UnbanEnforcementUser)
			riskRoute.POST("/enforcement/test_email", controller.SendEnforcementTestEmail)
		}
		monitoringAdminRoute := apiRouter.Group("/monitoring/admin")
		monitoringAdminRoute.Use(middleware.AdminAuth())
		{
			monitoringAdminRoute.GET("/groups", controller.GetAdminMonitoringGroups)
			monitoringAdminRoute.GET("/groups/:group", controller.GetAdminMonitoringGroupDetail)
			monitoringAdminRoute.GET("/groups/:group/history", controller.GetAdminMonitoringGroupHistory)
			monitoringAdminRoute.POST("/refresh", middleware.CriticalRateLimit(), controller.RefreshMonitoringData)
			monitoringAdminRoute.DELETE("/groups/:group/records", controller.DeleteMonitoringGroupRecords)
		}
		monitoringPublicRoute := apiRouter.Group("/monitoring/public")
		monitoringPublicRoute.Use(middleware.TryUserAuth())
		{
			monitoringPublicRoute.GET("/groups", controller.GetPublicMonitoringGroups)
			monitoringPublicRoute.GET("/groups/:group/history", controller.GetPublicMonitoringGroupHistory)
		}
		channelRoute := apiRouter.Group("/channel")
		channelRoute.Use(middleware.AdminAuth())
		{
			channelRoute.GET("/", controller.GetAllChannels)
			channelRoute.GET("/search", controller.SearchChannels)
			channelRoute.GET("/models", controller.ChannelListModels)
			channelRoute.GET("/models_enabled", controller.EnabledListModels)
			channelRoute.GET("/:id", controller.GetChannel)
			channelRoute.POST("/:id/key", middleware.RootAuth(), middleware.CriticalRateLimit(), middleware.DisableCache(), middleware.SecureVerificationRequired(), controller.GetChannelKey)
			channelRoute.GET("/test", controller.TestAllChannels)
			channelRoute.GET("/test/:id", controller.TestChannel)
			channelRoute.GET("/update_balance", controller.UpdateAllChannelsBalance)
			channelRoute.GET("/update_balance/:id", controller.UpdateChannelBalance)
			channelRoute.POST("/", controller.AddChannel)
			channelRoute.PUT("/", controller.UpdateChannel)
			channelRoute.DELETE("/disabled", controller.DeleteDisabledChannel)
			channelRoute.POST("/tag/disabled", controller.DisableTagChannels)
			channelRoute.POST("/tag/enabled", controller.EnableTagChannels)
			channelRoute.PUT("/tag", controller.EditTagChannels)
			channelRoute.DELETE("/:id", controller.DeleteChannel)
			channelRoute.POST("/batch", controller.DeleteChannelBatch)
			channelRoute.POST("/fix", controller.FixChannelsAbilities)
			channelRoute.GET("/fetch_models/:id", controller.FetchUpstreamModels)
			channelRoute.POST("/fetch_models", middleware.RootAuth(), controller.FetchModels)
			channelRoute.POST("/codex/oauth/start", controller.StartCodexOAuth)
			channelRoute.POST("/codex/oauth/complete", controller.CompleteCodexOAuth)
			channelRoute.POST("/:id/codex/oauth/start", controller.StartCodexOAuthForChannel)
			channelRoute.POST("/:id/codex/oauth/complete", controller.CompleteCodexOAuthForChannel)
			channelRoute.POST("/:id/codex/refresh", controller.RefreshCodexChannelCredential)
			channelRoute.GET("/:id/codex/usage", controller.GetCodexChannelUsage)
			channelRoute.POST("/ollama/pull", controller.OllamaPullModel)
			channelRoute.POST("/ollama/pull/stream", controller.OllamaPullModelStream)
			channelRoute.DELETE("/ollama/delete", controller.OllamaDeleteModel)
			channelRoute.GET("/ollama/version/:id", controller.OllamaVersion)
			channelRoute.POST("/batch/tag", controller.BatchSetChannelTag)
			channelRoute.GET("/tag/models", controller.GetTagModels)
			channelRoute.POST("/copy/:id", controller.CopyChannel)
			channelRoute.POST("/multi_key/manage", controller.ManageMultiKeys)
			channelRoute.POST("/upstream_updates/apply", controller.ApplyChannelUpstreamModelUpdates)
			channelRoute.POST("/upstream_updates/apply_all", controller.ApplyAllChannelUpstreamModelUpdates)
			channelRoute.POST("/upstream_updates/detect", controller.DetectChannelUpstreamModelUpdates)
			channelRoute.POST("/upstream_updates/detect_all", controller.DetectAllChannelUpstreamModelUpdates)
			// eflow: multi-key scheduler 状态快照（admin-api 状态面板用）。
			// per-channel 返回 (key_fp, model) 上的实时 RPM/TPM/in_flight/cool_until。
			channelRoute.GET("/:id/scheduler/state", controller.GetSchedulerState)
		}
		tokenRoute := apiRouter.Group("/token")
		tokenRoute.Use(middleware.UserAuth())
		{
			tokenRoute.GET("/", controller.GetAllTokens)
			tokenRoute.GET("/search", middleware.SearchRateLimit(), controller.SearchTokens)
			tokenRoute.GET("/:id", controller.GetToken)
			tokenRoute.POST("/:id/key", middleware.CriticalRateLimit(), middleware.DisableCache(), controller.GetTokenKey)
			tokenRoute.POST("/", controller.AddToken)
			tokenRoute.PUT("/", controller.UpdateToken)
			tokenRoute.DELETE("/:id", controller.DeleteToken)
			tokenRoute.POST("/batch", controller.DeleteTokenBatch)
			tokenRoute.POST("/batch/keys", middleware.CriticalRateLimit(), middleware.DisableCache(), controller.GetTokenKeysBatch)
		}

		usageRoute := apiRouter.Group("/usage")
		usageRoute.Use(middleware.CORS(), middleware.CriticalRateLimit())
		{
			tokenUsageRoute := usageRoute.Group("/token")
			tokenUsageRoute.Use(middleware.TokenAuthReadOnly())
			{
				tokenUsageRoute.GET("/", controller.GetTokenUsage)
			}
		}

		redemptionRoute := apiRouter.Group("/redemption")
		redemptionRoute.Use(middleware.AdminAuth())
		{
			redemptionRoute.GET("/", controller.GetAllRedemptions)
			redemptionRoute.GET("/search", controller.SearchRedemptions)
			redemptionRoute.GET("/:id", controller.GetRedemption)
			redemptionRoute.POST("/", controller.AddRedemption)
			redemptionRoute.PUT("/", controller.UpdateRedemption)
			redemptionRoute.DELETE("/invalid", controller.DeleteInvalidRedemption)
			redemptionRoute.DELETE("/:id", controller.DeleteRedemption)
		}
		invitationCodeRoute := apiRouter.Group("/invitation_code")
		invitationCodeRoute.Use(middleware.AdminAuth())
		{
			invitationCodeRoute.GET("/", controller.GetAllInvitationCodes)
			invitationCodeRoute.GET("/search", controller.SearchInvitationCodes)
			invitationCodeRoute.GET("/:id", controller.GetInvitationCode)
			invitationCodeRoute.GET("/:id/usages", controller.GetInvitationCodeUsages)
			invitationCodeRoute.POST("/", controller.AddInvitationCode)
			invitationCodeRoute.PUT("/", controller.UpdateInvitationCode)
			invitationCodeRoute.DELETE("/invalid", controller.DeleteInvalidInvitationCodes)
			invitationCodeRoute.DELETE("/:id", controller.DeleteInvitationCode)
		}
		logRoute := apiRouter.Group("/log")
		// eflow: 全站消费日志（GetAllLogs / stat / search）仅 admin 可见。
		// 客服处理工单要看某用户的消耗，走 /api/user/:id/profile-summary（用户画像，
		// staffReadRoute）那种「单用户汇总」形式，而不是放开全量日志流。
		logRoute.GET("/", middleware.AdminAuth(), controller.GetAllLogs)
		logRoute.DELETE("/", middleware.AdminAuth(), controller.DeleteHistoryLogs)
		logRoute.GET("/stat", middleware.AdminAuth(), controller.GetLogsStat)
		// eflow: /self、/self/stat、/self/export 三条用户可触发的重查询路径都套
		// SearchRateLimit（per-user，N/60s）+ UserConcurrencyLimit（per-user 同时 in-flight 上限），
		// 防止单用户狂刷把 logs 表打爆。SearchRateLimit 是 QPS 维度，concurrency 是堆积维度，两者必须叠加。
		// 每条路径用各自的限流 mark（SRS/SR/SRX），互不挤占——否则列表 + 统计 + 导出共享一个
		// 10/60s 桶，正常翻页几下就 429。
		logRoute.GET("/self/stat", middleware.UserAuth(), middleware.SearchRateLimitWithMark("SRS"), middleware.UserConcurrencyLimit("log-self-stat", 2), controller.GetLogsSelfStat)
		logRoute.GET("/channel_affinity_usage_cache", middleware.AdminAuth(), controller.GetChannelAffinityUsageCacheStats)
		logRoute.GET("/search", middleware.AdminAuth(), controller.SearchAllLogs)
		logRoute.GET("/self", middleware.UserAuth(), middleware.SearchRateLimitWithMark("SR"), middleware.UserConcurrencyLimit("log-self", 3), controller.GetUserLogs)
		// eflow: 普通用户自助导出使用日志（报账明细，ZIP 双 CSV）。
		// 导出是一次取最多 10w 行，并发上限收紧到 1（同一时刻每用户只准开一个导出）。
		logRoute.GET("/self/export", middleware.UserAuth(), middleware.SearchRateLimitWithMark("SRX"), middleware.UserConcurrencyLimit("log-self-export", 1), controller.ExportUserLogs)
		// eflow: 离线导出——后台异步跑（最多 30w 行）+ 把 ZIP 发到邮箱，避免大区间同步导出卡死。
		// 入队很轻，真正的并发控制在任务级锁（见 controller/log_export_async.go），这里只用 SRX 防刷。
		logRoute.GET("/self/export-async", middleware.UserAuth(), middleware.SearchRateLimitWithMark("SRX"), controller.ExportUserLogsAsync)
		logRoute.GET("/self/search", middleware.UserAuth(), middleware.SearchRateLimit(), controller.SearchUserLogs)

		dataRoute := apiRouter.Group("/data")
		dataRoute.GET("/", middleware.AdminAuth(), controller.GetAllQuotaDates)
		dataRoute.GET("/users", middleware.AdminAuth(), controller.GetQuotaDatesByUser)
		dataRoute.GET("/self", middleware.UserAuth(), controller.GetUserQuotaDates)

		logRoute.Use(middleware.CORS(), middleware.CriticalRateLimit())
		{
			logRoute.GET("/token", middleware.TokenAuthReadOnly(), controller.GetLogByKey)
		}
		groupRoute := apiRouter.Group("/group")
		groupRoute.Use(middleware.AdminAuth())
		{
			groupRoute.GET("/", controller.GetGroups)
		}

		prefillGroupRoute := apiRouter.Group("/prefill_group")
		prefillGroupRoute.Use(middleware.AdminAuth())
		{
			prefillGroupRoute.GET("/", controller.GetPrefillGroups)
			prefillGroupRoute.POST("/", controller.CreatePrefillGroup)
			prefillGroupRoute.PUT("/", controller.UpdatePrefillGroup)
			prefillGroupRoute.DELETE("/:id", controller.DeletePrefillGroup)
		}

		mjRoute := apiRouter.Group("/mj")
		mjRoute.GET("/self", middleware.UserAuth(), controller.GetUserMidjourney)
		mjRoute.GET("/", middleware.AdminAuth(), controller.GetAllMidjourney)

		taskRoute := apiRouter.Group("/task")
		{
			taskRoute.GET("/self", middleware.UserAuth(), controller.GetUserTask)
			taskRoute.GET("/", middleware.AdminAuth(), controller.GetAllTask)
		}

		vendorRoute := apiRouter.Group("/vendors")
		vendorRoute.Use(middleware.AdminAuth())
		{
			vendorRoute.GET("/", controller.GetAllVendors)
			vendorRoute.GET("/search", controller.SearchVendors)
			vendorRoute.GET("/:id", controller.GetVendorMeta)
			vendorRoute.POST("/", controller.CreateVendorMeta)
			vendorRoute.PUT("/", controller.UpdateVendorMeta)
			vendorRoute.DELETE("/:id", controller.DeleteVendorMeta)
		}

		modelsRoute := apiRouter.Group("/models")
		modelsRoute.Use(middleware.AdminAuth())
		{
			modelsRoute.GET("/sync_upstream/preview", controller.SyncUpstreamPreview)
			modelsRoute.POST("/sync_upstream", controller.SyncUpstreamModels)
			modelsRoute.GET("/missing", controller.GetMissingModels)
			modelsRoute.GET("/", controller.GetAllModelsMeta)
			modelsRoute.GET("/search", controller.SearchModelsMeta)
			modelsRoute.GET("/:id", controller.GetModelMeta)
			modelsRoute.POST("/", controller.CreateModelMeta)
			modelsRoute.PUT("/", controller.UpdateModelMeta)
			modelsRoute.DELETE("/:id", controller.DeleteModelMeta)
		}

		// Deployments (model deployment management)
		deploymentsRoute := apiRouter.Group("/deployments")
		deploymentsRoute.Use(middleware.AdminAuth())
		{
			deploymentsRoute.GET("/settings", controller.GetModelDeploymentSettings)
			deploymentsRoute.POST("/settings/test-connection", controller.TestIoNetConnection)
			deploymentsRoute.GET("/", controller.GetAllDeployments)
			deploymentsRoute.GET("/search", controller.SearchDeployments)
			deploymentsRoute.POST("/test-connection", controller.TestIoNetConnection)
			deploymentsRoute.GET("/hardware-types", controller.GetHardwareTypes)
			deploymentsRoute.GET("/locations", controller.GetLocations)
			deploymentsRoute.GET("/available-replicas", controller.GetAvailableReplicas)
			deploymentsRoute.POST("/price-estimation", controller.GetPriceEstimation)
			deploymentsRoute.GET("/check-name", controller.CheckClusterNameAvailability)
			deploymentsRoute.POST("/", controller.CreateDeployment)

			deploymentsRoute.GET("/:id", controller.GetDeployment)
			deploymentsRoute.GET("/:id/logs", controller.GetDeploymentLogs)
			deploymentsRoute.GET("/:id/containers", controller.ListDeploymentContainers)
			deploymentsRoute.GET("/:id/containers/:container_id", controller.GetContainerDetails)
			deploymentsRoute.PUT("/:id", controller.UpdateDeployment)
			deploymentsRoute.PUT("/:id/name", controller.UpdateDeploymentName)
			deploymentsRoute.POST("/:id/extend", controller.ExtendDeployment)
			deploymentsRoute.DELETE("/:id", controller.DeleteDeployment)
		}
	}
}
