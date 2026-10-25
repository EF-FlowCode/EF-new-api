import { api, CALLER_HANDLED_REQUEST_CONFIG } from '@/lib/api'
import { securityProofHeaders } from '@/lib/secure-verification'
import type { ApiResponse, PasskeyOptionsPayload, PasskeyStatus } from './types'

export async function getPasskeyStatus(): Promise<ApiResponse<PasskeyStatus>> {
  const res = await api.get<ApiResponse<PasskeyStatus>>(
    '/api/user/passkey',
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}

/** Registration needs a passkey.register proof. */
export async function beginPasskeyRegistration(
  proofToken?: string
): Promise<ApiResponse<PasskeyOptionsPayload>> {
  const res = await api.post<ApiResponse<PasskeyOptionsPayload>>(
    '/api/user/passkey/register/begin',
    undefined,
    {
      ...CALLER_HANDLED_REQUEST_CONFIG,
      headers: securityProofHeaders(proofToken),
    }
  )
  return res.data
}

export async function finishPasskeyRegistration(payload: {
  flow_token: string
  credential: Record<string, unknown>
}): Promise<ApiResponse> {
  const res = await api.post<ApiResponse>(
    '/api/user/passkey/register/finish',
    payload,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}

/** Removing the Passkey needs a passkey.delete proof. */
export async function deletePasskey(proofToken?: string): Promise<ApiResponse> {
  const res = await api.delete<ApiResponse>('/api/user/passkey', {
    ...CALLER_HANDLED_REQUEST_CONFIG,
    headers: securityProofHeaders(proofToken),
  })
  return res.data
}

export async function beginPasskeyLogin(): Promise<
  ApiResponse<PasskeyOptionsPayload>
> {
  const res = await api.post<ApiResponse<PasskeyOptionsPayload>>(
    '/api/user/passkey/login/begin',
    undefined,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}

export async function finishPasskeyLogin(
  payload: Record<string, unknown>
): Promise<ApiResponse> {
  const res = await api.post<ApiResponse>(
    '/api/user/passkey/login/finish',
    payload,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}

/** Starts a step-up assertion for one scoped operation. */
export async function beginPasskeyVerification(operation: {
  scope: string
  context?: Record<string, unknown>
}): Promise<ApiResponse<PasskeyOptionsPayload>> {
  const res = await api.post<ApiResponse<PasskeyOptionsPayload>>(
    '/api/user/passkey/verify/begin',
    operation,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}

/** Finishes the assertion; the response data is the security proof. */
export async function finishPasskeyVerification(payload: {
  flow_token: string
  credential: Record<string, unknown>
}): Promise<ApiResponse> {
  const res = await api.post<ApiResponse>(
    '/api/user/passkey/verify/finish',
    payload,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}
