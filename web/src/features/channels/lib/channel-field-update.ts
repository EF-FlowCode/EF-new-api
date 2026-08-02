export const CHANNEL_FIELD_UPDATE_DELAY_MS = 800

type ChannelFieldUpdateTimers = {
  setTimeout: (callback: () => void, delay: number) => number
  clearTimeout: (id: number) => void
}

const browserTimers: ChannelFieldUpdateTimers = {
  setTimeout: (callback, delay) => window.setTimeout(callback, delay),
  clearTimeout: (id) => window.clearTimeout(id),
}

export function createChannelFieldUpdateScheduler(
  onUpdate: (value: number) => void,
  timers: ChannelFieldUpdateTimers = browserTimers
) {
  let timeoutId: number | undefined
  let pendingValue: number | undefined

  const clearPendingTimer = () => {
    if (timeoutId === undefined) return
    timers.clearTimeout(timeoutId)
    timeoutId = undefined
  }

  const flush = () => {
    clearPendingTimer()
    if (pendingValue === undefined) return
    const value = pendingValue
    pendingValue = undefined
    onUpdate(value)
  }

  return {
    schedule(value: number) {
      clearPendingTimer()
      pendingValue = value
      timeoutId = timers.setTimeout(flush, CHANNEL_FIELD_UPDATE_DELAY_MS)
    },
    flush,
  }
}
