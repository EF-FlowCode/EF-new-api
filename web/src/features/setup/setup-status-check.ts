interface SetupStatusCheckResponse {
  success?: boolean
  data?: {
    status?: boolean
  }
}

export type SetupStatusCheckOutcome = 'continue' | 'redirect' | 'retry'

export function createSetupStatusCheck() {
  let checked = false

  return {
    shouldCheck(pathname: string): boolean {
      return !checked && !pathname.startsWith('/setup')
    },

    consume(
      response: SetupStatusCheckResponse | null
    ): SetupStatusCheckOutcome {
      if (
        !response?.success ||
        !response.data ||
        typeof response.data.status !== 'boolean'
      ) {
        return 'retry'
      }
      if (!response.data.status) {
        return 'redirect'
      }

      checked = true
      return 'continue'
    },
  }
}
