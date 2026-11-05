import { SettingsPage } from '../components/settings-page'
import type { RequestLimitsSettings } from '../types'
import {
  REQUEST_LIMITS_DEFAULT_SECTION,
  getRequestLimitsSectionContent,
} from './section-registry.tsx'

const defaultRequestLimitsSettings: RequestLimitsSettings = {
  ModelRequestRateLimitEnabled: false,
  ModelRequestRateLimitCount: 0,
  ModelRequestRateLimitSuccessCount: 1000,
  ModelRequestRateLimitDurationMinutes: 1,
  ModelRequestRateLimitGroup: '',
  ModelRPMConfig: '{"enabled":false,"models":{}}',
  ModelRatio: '',
  ModelPrice: '',
  TopupGroupRatio: '',
  CheckSensitiveEnabled: false,
  CheckSensitiveOnPromptEnabled: false,
  SensitiveWords: '',
  'fetch_setting.enable_ssrf_protection': true,
  'fetch_setting.allow_private_ip': false,
  'fetch_setting.domain_filter_mode': false,
  'fetch_setting.ip_filter_mode': false,
  'fetch_setting.domain_list': [],
  'fetch_setting.ip_list': [],
  'fetch_setting.allowed_ports': [],
  'fetch_setting.apply_ip_filter_for_domain': false,
  'runtime_limit_setting.streaming_timeout_seconds': 300,
  'runtime_limit_setting.error_log_enabled': false,
  'runtime_limit_setting.task_timeout_minutes': 1440,
  'runtime_limit_setting.max_request_body_mb': 128,
  'runtime_limit_setting.notify_limit_count': 2,
  'runtime_limit_setting.notify_limit_duration_minutes': 10,
  'runtime_limit_setting.response_header_timeout_seconds': 1800,
  'runtime_limit_setting.non_stream_response_header_timeout_seconds': 1800,
}

export function RequestLimitsSettings() {
  return (
    <SettingsPage
      routePath='/_authenticated/system-settings/request-limits/$section'
      defaultSettings={defaultRequestLimitsSettings}
      defaultSection={REQUEST_LIMITS_DEFAULT_SECTION}
      getSectionContent={getRequestLimitsSectionContent}
      area='request-limits'
    />
  )
}
