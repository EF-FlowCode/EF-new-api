export type SetupUsageMode = 'external' | 'self' | 'demo'

export interface SetupStatus {
  status: boolean
  root_init: boolean
  // Some backends also echo mode flags; they are optional here.
  SelfUseModeEnabled?: boolean
  DemoSiteEnabled?: boolean
}

export interface SetupFormValues {
  username: string
  password: string
  confirmPassword: string
  usageMode: SetupUsageMode
}

export interface SetupResponse {
  success: boolean
  message?: string
  data?: SetupStatus
}
