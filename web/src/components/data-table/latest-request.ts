import { useCallback, useEffect, useRef } from 'react'

export type LatestRequestScope = {
  signal: AbortSignal
  isCurrent: () => boolean
  commit: (effect: () => void) => boolean
}

export type LatestRequestState = {
  begin: () => LatestRequestScope
  dispose: () => void
}

export function createLatestRequestState(): LatestRequestState {
  let sequence = 0
  let activeController: AbortController | null = null
  let disposed = false

  return {
    begin() {
      if (disposed) {
        throw new Error('Cannot begin a request after disposal')
      }
      sequence += 1
      const requestSequence = sequence
      const previousController = activeController
      const controller = new AbortController()
      activeController = controller
      previousController?.abort()

      const isCurrent = () =>
        !disposed &&
        requestSequence === sequence &&
        activeController === controller &&
        !controller.signal.aborted

      return {
        signal: controller.signal,
        isCurrent,
        commit(effect) {
          if (!isCurrent()) return false
          effect()
          return true
        },
      }
    },
    dispose() {
      if (disposed) return
      disposed = true
      sequence += 1
      const controller = activeController
      activeController = null
      controller?.abort()
    },
  }
}

export type LatestRequestHandlers<T> = {
  onSuccess?: (value: T) => void
  onError?: (error: unknown) => void
  onSettled?: () => void
}

export type RunLatestRequest = <T>(
  request: (signal: AbortSignal) => Promise<T>,
  handlers?: LatestRequestHandlers<T>
) => Promise<void>

export function useLatestRequest(): RunLatestRequest {
  const requestStateRef = useRef<LatestRequestState | null>(null)
  const mountedRef = useRef(false)

  useEffect(() => {
    const requestState = createLatestRequestState()
    requestStateRef.current = requestState
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      requestState.dispose()
      if (requestStateRef.current === requestState) {
        requestStateRef.current = null
      }
    }
  }, [])

  return useCallback(
    async <T>(
      request: (signal: AbortSignal) => Promise<T>,
      handlers: LatestRequestHandlers<T> = {}
    ) => {
      if (!mountedRef.current) return

      const requestState = requestStateRef.current
      if (!requestState) return
      const scope = requestState.begin()
      try {
        const value = await request(scope.signal)
        scope.commit(() => handlers.onSuccess?.(value))
      } catch (error) {
        scope.commit(() => handlers.onError?.(error))
      } finally {
        scope.commit(() => handlers.onSettled?.())
      }
    },
    []
  )
}
