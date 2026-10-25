export type VerificationMethod = '2fa' | 'passkey' | 'password' | 'session'

/**
 * The action a proof authorizes. Scope strings and context shapes match the
 * backend (service/security_verification.go); a proof only works for the
 * exact scope and context it was issued for.
 */
export interface VerificationOperation {
  scope: string
  context?: Record<string, unknown>
}

/** A single-use, one-minute proof sent as the X-Security-Proof header. */
export interface SecurityProof {
  proof_token: string
  expires_at: number
  method: VerificationMethod
  scope: string
}

export interface VerificationMethodOption {
  method: VerificationMethod
  available: boolean
  reason?: string
}

export interface VerificationMethods {
  /** Methods the server allows for the current operation, in server order. */
  options: VerificationMethodOption[]
  passkeySupported: boolean
  /** Set when the server refused the operation itself (e.g. not permitted). */
  error?: string
}

export interface SecureVerificationState {
  method: VerificationMethod | null
  loading: boolean
  code: string
  title?: string
  description?: string
}

export interface UseSecureVerificationOptions {
  onSuccess?: (result: unknown, method: VerificationMethod) => void
  onError?: (error: unknown) => void
  successMessage?: string
  autoReset?: boolean
  /** Called when the user dismisses the dialog without verifying. */
  onCancel?: () => void
}

export interface StartVerificationOptions {
  operation: VerificationOperation
  preferredMethod?: VerificationMethod
  title?: string
  description?: string
}

/** A protected call. It receives the proof once verification succeeded. */
export type ProtectedCall<T = unknown> = (proof?: SecurityProof) => Promise<T>
