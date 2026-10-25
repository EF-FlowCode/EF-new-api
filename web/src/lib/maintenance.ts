import { isAxiosError } from 'axios'

let active = false
const listeners = new Set<() => void>()

export function isMaintenanceError(error: unknown): boolean {
  return (
    isAxiosError(error) &&
    error.response?.status === 503 &&
    error.response?.data?.error?.code === 'maintenance'
  )
}

export function setMaintenance(value: boolean) {
  if (active === value) return
  active = value
  listeners.forEach((listener) => listener())
}

export const getMaintenance = () => active
export function subscribeMaintenance(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export async function probeMaintenance(signal: AbortSignal) {
  const response = await fetch('/__maintenance/status', {
    cache: 'no-store',
    signal,
  })
  if (!response.ok) return
  const data: unknown = await response.json()
  if (
    typeof data === 'object' &&
    data !== null &&
    'maintenance' in data &&
    typeof data.maintenance === 'boolean'
  ) {
    setMaintenance(data.maintenance)
  }
}
