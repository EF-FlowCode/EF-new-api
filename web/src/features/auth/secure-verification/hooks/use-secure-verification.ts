import { useCallback, useMemo, useState } from 'react'
import i18next from 'i18next'
import { toast } from 'sonner'
import {
  extractVerificationInfo,
  isVerificationRequiredError,
} from '@/lib/secure-verification'
import { checkVerificationMethods, verify } from '../api'
import type {
  ProtectedCall,
  SecureVerificationState,
  StartVerificationOptions,
  UseSecureVerificationOptions,
  VerificationMethod,
  VerificationMethods,
  VerificationOperation,
} from '../types'

interface InternalState extends SecureVerificationState {
  apiCall: ProtectedCall | null
  operation: VerificationOperation | null
}

const defaultMethods: VerificationMethods = {
  options: [],
  passkeySupported: false,
}

const initialState: InternalState = {
  method: null,
  loading: false,
  code: '',
  title: undefined,
  description: undefined,
  apiCall: null,
  operation: null,
}

/** Methods the dialog can offer right now (allowed, available, supported). */
export function usableMethods(methods: VerificationMethods) {
  return methods.options
    .filter((option) => option.available)
    .filter((option) => option.method !== 'passkey' || methods.passkeySupported)
    .map((option) => option.method)
}

/**
 * Drives the step-up dialog: fetches the methods allowed for one operation,
 * obtains a single-use proof and then runs the protected call with it.
 */
export function useSecureVerification(
  options: UseSecureVerificationOptions = {}
) {
  const {
    onSuccess,
    onError,
    onCancel,
    successMessage,
    autoReset = true,
  } = options

  const [methods, setMethods] = useState<VerificationMethods>(defaultMethods)
  const [state, setState] = useState<InternalState>(initialState)
  const [open, setOpen] = useState(false)

  const fetchVerificationMethods = useCallback(
    async (operation: VerificationOperation) => {
      const result = await checkVerificationMethods(operation)
      setMethods(result)
      return result
    },
    []
  )

  const reset = useCallback(() => {
    setState(initialState)
    setOpen(false)
  }, [])

  const startVerification = useCallback(
    async (apiCall: ProtectedCall, config: StartVerificationOptions) => {
      const { operation, preferredMethod, title, description } = config
      const availableMethods = await fetchVerificationMethods(operation)
      const usable = usableMethods(availableMethods)

      if (availableMethods.error) {
        toast.error(i18next.t(availableMethods.error))
        onError?.(new Error(availableMethods.error))
        return false
      }

      const defaultMethod: VerificationMethod | null =
        preferredMethod && usable.includes(preferredMethod)
          ? preferredMethod
          : (usable[0] ?? null)

      setState({
        ...initialState,
        apiCall,
        operation,
        method: defaultMethod,
        title,
        description,
      })
      setOpen(true)
      return true
    },
    [fetchVerificationMethods, onError]
  )

  const executeVerification = useCallback(
    async (method?: VerificationMethod, code?: string) => {
      if (!state.apiCall || !state.operation) {
        toast.error(i18next.t('Verification is not configured properly'))
        return
      }

      const actualMethod = method ?? state.method
      if (!actualMethod) {
        toast.error(i18next.t('Select a verification method first'))
        return
      }

      setState((prev) => ({ ...prev, loading: true }))

      try {
        const proof = await verify(
          actualMethod,
          state.operation,
          code ?? state.code
        )
        const result = await state.apiCall(proof)

        if (successMessage) {
          toast.success(successMessage)
        }

        onSuccess?.(result, actualMethod)

        if (autoReset) {
          reset()
        }

        return result
      } catch (error) {
        const message =
          error instanceof Error
            ? i18next.t(error.message)
            : i18next.t('Verification failed')
        toast.error(message)
        onError?.(error)
        throw error
      } finally {
        setState((prev) => ({ ...prev, loading: false }))
      }
    },
    [state, successMessage, onSuccess, onError, autoReset, reset]
  )

  const setCode = useCallback((code: string) => {
    setState((prev) => ({ ...prev, code }))
  }, [])

  const switchMethod = useCallback((method: VerificationMethod) => {
    setState((prev) => ({ ...prev, method, code: '' }))
  }, [])

  const cancel = useCallback(() => {
    reset()
    onCancel?.()
  }, [reset, onCancel])

  /**
   * Run the call without a proof first and open the dialog only when the
   * server asks for verification (for actions that need it conditionally).
   */
  const withVerification = useCallback(
    async (apiCall: ProtectedCall, config: StartVerificationOptions) => {
      try {
        return await apiCall()
      } catch (error) {
        if (isVerificationRequiredError(error)) {
          const info = extractVerificationInfo(error)
          toast.info(i18next.t(info.message))
          await startVerification(apiCall, config)
          return null
        }
        throw error
      }
    },
    [startVerification]
  )

  // Ready-made props for <SecureVerificationDialog />. Verification errors are
  // already surfaced via toast inside executeVerification.
  const dialogProps = useMemo(
    () => ({
      open,
      onOpenChange: (next: boolean) => {
        if (next) setOpen(true)
        else cancel()
      },
      methods,
      state,
      onVerify: async (method: VerificationMethod, code?: string) => {
        try {
          await executeVerification(method, code)
        } catch {
          // surfaced via toast
        }
      },
      onCancel: cancel,
      onCodeChange: setCode,
      onMethodChange: switchMethod,
    }),
    [open, methods, state, executeVerification, cancel, setCode, switchMethod]
  )

  return {
    dialogProps,
    open,
    setOpen,
    methods,
    state,
    startVerification,
    executeVerification,
    cancel,
    reset,
    setCode,
    switchMethod,
    withVerification,
    fetchVerificationMethods,
    isLoading: state.loading,
    currentMethod: state.method,
    code: state.code,
  }
}
