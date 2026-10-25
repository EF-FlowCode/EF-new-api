import type { AxiosError } from 'axios'

export interface VerificationRequiredInfo {
  code?: string
  message: string
  required: boolean
}

/** Header carrying a single-use, action-bound security proof. */
export const SECURITY_PROOF_HEADER = 'X-Security-Proof'

/** Request headers for a proof-protected call; empty without a proof. */
export function securityProofHeaders(
  proofToken?: string
): Record<string, string> | undefined {
  return proofToken ? { [SECURITY_PROOF_HEADER]: proofToken } : undefined
}

// Codes that mean "verify again and retry": a missing, expired, used or
// mismatched proof (backend middleware/secure_verification.go).
const VERIFICATION_CODES = new Set([
  'SECURITY_PROOF_REQUIRED',
  'SECURITY_PROOF_EXPIRED',
  'SECURITY_PROOF_CONSUMED',
  'SECURITY_PROOF_INVALID',
  'SECURITY_PROOF_SCOPE_MISMATCH',
  'SECURITY_PROOF_CONTEXT_MISMATCH',
  'SECURITY_PROOF_METHOD_MISMATCH',
])

/**
 * Determine whether an Axios error indicates secure verification is required.
 */
export function isVerificationRequiredError(
  error: unknown
): error is AxiosError {
  if (!error || typeof error !== 'object') return false
  const axiosError = error as AxiosError<{ code?: string }>
  if (axiosError.response?.status !== 403) return false
  const code = axiosError.response?.data?.code
  return Boolean(code && VERIFICATION_CODES.has(code))
}

/**
 * Extract verification requirement info from an Axios error.
 */
export function extractVerificationInfo(
  error: unknown
): VerificationRequiredInfo {
  const axiosError = error as AxiosError<{ code?: string; message?: string }>
  const code = axiosError.response?.data?.code
  const message =
    axiosError.response?.data?.message ?? 'Secure verification is required'

  return {
    code,
    message,
    required: true,
  }
}
