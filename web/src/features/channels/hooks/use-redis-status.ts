import { useEffect, useState } from 'react'

let cachedStatus: { redisEnabled: boolean } | null = null
let inflight: Promise<boolean> | null = null

async function fetchRedisStatus(): Promise<boolean> {
  if (cachedStatus !== null) return cachedStatus.redisEnabled
  if (inflight) return inflight
  inflight = (async () => {
    try {
      const res = await fetch('/api/status')
      if (!res.ok) throw new Error('Failed to fetch status')
      const json = await res.json()
      const enabled = json?.data?.redis_enabled !== false
      cachedStatus = { redisEnabled: enabled }
      return enabled
    } catch {
      cachedStatus = { redisEnabled: true }
      return true
    } finally {
      inflight = null
    }
  })()
  return inflight
}

/**
 * Reads `redis_enabled` from /api/status. Used by editors that warn the user
 * when Redis is missing (e.g. channel rate limiting needs Redis to enforce
 * limits across multiple replicas).
 */
export function useRedisStatus(): { redisEnabled: boolean; loading: boolean } {
  const [redisEnabled, setRedisEnabled] = useState<boolean>(
    cachedStatus?.redisEnabled ?? true
  )
  const [loading, setLoading] = useState<boolean>(cachedStatus === null)

  useEffect(() => {
    let active = true
    if (cachedStatus !== null) return
    fetchRedisStatus().then((enabled) => {
      if (!active) return
      setRedisEnabled(enabled)
      setLoading(false)
    })
    return () => {
      active = false
    }
  }, [])

  return { redisEnabled, loading }
}
