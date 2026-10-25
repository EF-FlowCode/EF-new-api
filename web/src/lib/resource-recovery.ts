declare global {
  interface Window {
    __newApiResourceRecovery?: {
      ready: () => void
      isResourceError: (error: unknown) => boolean
    }
  }
}

export function isResourceLoadError(error: unknown): boolean {
  return (
    typeof window !== 'undefined' &&
    (window.__newApiResourceRecovery?.isResourceError(error) ?? false)
  )
}
