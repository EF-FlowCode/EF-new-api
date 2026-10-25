import {
  api,
  CALLER_HANDLED_REQUEST_CONFIG,
  getRequestErrorMessage,
} from '@/lib/api'
import {
  buildAssertionResult,
  prepareCredentialRequestOptions,
  isPasskeySupported as detectPasskeySupport,
} from '@/lib/passkey'
import { beginPasskeyVerification, finishPasskeyVerification } from '../passkey'
import type {
  SecurityProof,
  VerificationMethod,
  VerificationMethodOption,
  VerificationMethods,
  VerificationOperation,
} from './types'

interface ApiEnvelope<T> {
  success: boolean
  message?: string
  code?: string
  data?: T
}

/**
 * Fetch the verification methods the server allows for one operation.
 */
export async function checkVerificationMethods(
  operation: VerificationOperation
): Promise<VerificationMethods> {
  const passkeySupported = await detectPasskeySupport().catch(() => false)
  try {
    const res = await api.get<
      ApiEnvelope<{ methods?: VerificationMethodOption[] }>
    >('/api/verify/methods', {
      ...CALLER_HANDLED_REQUEST_CONFIG,
      params: { scope: operation.scope },
    })
    if (!res.data?.success) {
      return {
        options: [],
        passkeySupported,
        error: res.data?.message || 'Verification is unavailable',
      }
    }
    return { options: res.data.data?.methods ?? [], passkeySupported }
  } catch (error) {
    return {
      options: [],
      passkeySupported,
      error: getRequestErrorMessage(error, 'Verification is unavailable'),
    }
  }
}

function unwrapProof(
  envelope: ApiEnvelope<SecurityProof> | undefined
): SecurityProof {
  if (!envelope?.success || !envelope.data?.proof_token) {
    throw new Error(envelope?.message || 'Verification failed')
  }
  return envelope.data
}

/**
 * Run one verification method for the operation and return its proof.
 */
export async function verify(
  method: VerificationMethod,
  operation: VerificationOperation,
  code?: string
): Promise<SecurityProof> {
  if (method === 'passkey') return verifyPasskey(operation)
  const body: Record<string, unknown> = {
    method,
    scope: operation.scope,
    context: operation.context,
  }
  if (method === '2fa') {
    const trimmed = code?.trim()
    if (!trimmed) {
      throw new Error('Please enter the verification code or backup code')
    }
    body.code = trimmed
  } else if (method === 'password') {
    if (!code) throw new Error('Please enter your password')
    body.password = code
  }
  try {
    const res = await api.post<ApiEnvelope<SecurityProof>>(
      '/api/verify',
      body,
      CALLER_HANDLED_REQUEST_CONFIG
    )
    return unwrapProof(res.data)
  } catch (error) {
    throw new Error(getRequestErrorMessage(error, 'Verification failed'), {
      cause: error,
    })
  }
}

async function verifyPasskey(
  operation: VerificationOperation
): Promise<SecurityProof> {
  if (typeof navigator === 'undefined' || !navigator.credentials) {
    throw new Error('Passkey verification is not supported in this environment')
  }

  try {
    const beginResponse = await beginPasskeyVerification(operation)
    if (!beginResponse.success || !beginResponse.data?.flow_token) {
      throw new Error(beginResponse.message || 'Failed to start verification')
    }

    const publicKey = prepareCredentialRequestOptions(
      beginResponse.data.options ?? beginResponse.data
    )
    const credential = (await navigator.credentials.get({
      publicKey,
    })) as PublicKeyCredential | null
    if (!credential) {
      throw new Error('Passkey verification was cancelled')
    }
    const assertion = buildAssertionResult(credential)
    if (!assertion) {
      throw new Error('Unable to build Passkey assertion')
    }

    const finishResponse = await finishPasskeyVerification({
      flow_token: beginResponse.data.flow_token,
      credential: assertion,
    })
    return unwrapProof(finishResponse as ApiEnvelope<SecurityProof>)
  } catch (error: unknown) {
    if (error instanceof DOMException && error.name === 'NotAllowedError') {
      throw new Error('Passkey verification was cancelled or timed out', {
        cause: error,
      })
    }
    if (error instanceof DOMException && error.name === 'InvalidStateError') {
      throw new Error(
        'Passkey verification is not available in the current state',
        { cause: error }
      )
    }
    throw error
  }
}
