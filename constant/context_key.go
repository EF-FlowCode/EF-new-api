package constant

type ContextKey string

const (
	ContextKeyTokenCountMeta  ContextKey = "token_count_meta"
	ContextKeyPromptTokens    ContextKey = "prompt_tokens"
	ContextKeyEstimatedTokens ContextKey = "estimated_tokens"

	ContextKeyOriginalModel     ContextKey = "original_model"
	ContextKeyRequestStartTime  ContextKey = "request_start_time"
	ContextKeyAuthSettings      ContextKey = "auth_settings"
	ContextKeyRelayPolicy       ContextKey = "relay_runtime_policy"
	ContextKeyRelaySucceeded    ContextKey = "relay_succeeded"
	ContextKeyRelayStreamStatus ContextKey = "relay_stream_status"

	/* token related keys */
	ContextKeyTokenUnlimited         ContextKey = "token_unlimited_quota"
	ContextKeyTokenKey               ContextKey = "token_key"
	ContextKeyTokenId                ContextKey = "token_id"
	ContextKeyTokenGroup             ContextKey = "token_group"
	ContextKeyTokenSpecificChannelId ContextKey = "specific_channel_id"
	ContextKeyTokenModelLimitEnabled ContextKey = "token_model_limit_enabled"
	ContextKeyTokenModelLimit        ContextKey = "token_model_limit"
	ContextKeyTokenCrossGroupRetry   ContextKey = "token_cross_group_retry"

	/* channel related keys */
	ContextKeyChannelId                ContextKey = "channel_id"
	ContextKeyChannelName              ContextKey = "channel_name"
	ContextKeyChannelCreateTime        ContextKey = "channel_create_time"
	ContextKeyChannelBaseUrl           ContextKey = "base_url"
	ContextKeyChannelType              ContextKey = "channel_type"
	ContextKeyChannelSetting           ContextKey = "channel_setting"
	ContextKeyChannelOtherSetting      ContextKey = "channel_other_setting"
	ContextKeyChannelParamOverride     ContextKey = "param_override"
	ContextKeyChannelHeaderOverride    ContextKey = "header_override"
	ContextKeyChannelOrganization      ContextKey = "channel_organization"
	ContextKeyChannelAutoBan           ContextKey = "auto_ban"
	ContextKeyChannelModelMapping      ContextKey = "model_mapping"
	ContextKeyChannelStatusCodeMapping ContextKey = "status_code_mapping"
	ContextKeyChannelIsMultiKey        ContextKey = "channel_is_multi_key"
	ContextKeyChannelMultiKeyIndex     ContextKey = "channel_multi_key_index"
	ContextKeyChannelKey               ContextKey = "channel_key"
	// ContextKeySelectedChannel keeps the complete selected channel for the
	// controller's first relay attempt. Channel metadata alone is insufficient
	// for rate-limit and multi-key scheduler lifecycle decisions.
	ContextKeySelectedChannel ContextKey = "selected_channel"

	ContextKeyAutoGroup           ContextKey = "auto_group"
	ContextKeyAutoGroupIndex      ContextKey = "auto_group_index"
	ContextKeyAutoGroupRetryIndex ContextKey = "auto_group_retry_index"

	/* user related keys */
	ContextKeyUserId                      ContextKey = "id"
	ContextKeyUserSetting                 ContextKey = "user_setting"
	ContextKeyUserQuota                   ContextKey = "user_quota"
	ContextKeyUserStatus                  ContextKey = "user_status"
	ContextKeyUserEmail                   ContextKey = "user_email"
	ContextKeyUserGroup                   ContextKey = "user_group"
	ContextKeyUserAdditionalCustomerTiers ContextKey = "user_additional_customer_tiers"
	ContextKeyUsingGroup                  ContextKey = "group"
	ContextKeyUserName                    ContextKey = "username"

	ContextKeyLocalCountTokens ContextKey = "local_count_tokens"
	// eflow: ContextKeyBillingAnomaly 由渠道处理器显式标注哪部分用量是本地估算的
	// （值为 model.LogBillingAnomaly*）；未标注时结算层按通用规则推断。
	ContextKeyBillingAnomaly ContextKey = "billing_anomaly"

	ContextKeySystemPromptOverride ContextKey = "system_prompt_override"

	// ContextKeyFileSourcesToCleanup stores file sources that need cleanup when request ends
	ContextKeyFileSourcesToCleanup ContextKey = "file_sources_to_cleanup"

	// ContextKeyAdminRejectReason stores an admin-only reject/block reason extracted from upstream responses.
	// It is not returned to end users, but can be persisted into consume/error logs for debugging.
	ContextKeyAdminRejectReason ContextKey = "admin_reject_reason"

	// ContextKeyLanguage stores the user's language preference for i18n
	ContextKeyLanguage ContextKey = "language"
	ContextKeyIsStream ContextKey = "is_stream"

	// ContextKeyRateLimitSkipped marks that the current request was diverted from
	// its primary (often affinity-bound) channel solely because of channel-level
	// rate limiting. Used by the affinity layer to suppress SwitchOnSuccess so a
	// transient rate-limit detour does not displace cache affinity on the
	// original channel.
	ContextKeyRateLimitSkipped ContextKey = "rate_limit_skipped"

	// ContextKeyAuditLogged marks that the current request has already recorded
	// its own audit event, so the auth-chain fallback (finishAdminAudit) skips it.
	ContextKeyAuditLogged ContextKey = "audit_logged"

	// ContextKeyTokenAuditParams contains only the API token operation's safe metadata.
	ContextKeyTokenAuditParams ContextKey = "token_audit_params"
)
