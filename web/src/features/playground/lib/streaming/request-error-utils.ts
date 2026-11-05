import { ERROR_MESSAGES } from '../../constants'

type RequestErrorLike = {
  message?: string
  response?: {
    data?: {
      error?: {
        code?: string
      }
      message?: string
    }
  }
}

export type RequestErrorDetails = {
  errorCode?: string
  errorMessage: string
}

export function parseRequestErrorDetails(error: unknown): RequestErrorDetails {
  const requestError = error as RequestErrorLike

  return {
    errorCode: requestError?.response?.data?.error?.code || undefined,
    errorMessage:
      requestError?.response?.data?.message ||
      requestError?.message ||
      ERROR_MESSAGES.API_REQUEST_ERROR,
  }
}
