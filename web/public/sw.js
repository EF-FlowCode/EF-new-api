/**
 * Minimal "installable-only" service worker for New API.
 *
 * Intentionally does NOT cache anything: an admin/API-gateway dashboard is
 * fully network-driven, and a caching SW would risk serving stale JS bundles
 * after a deploy. We only register a no-op `fetch` listener because Chromium
 * requires a fetch handler to exist before the app is considered installable
 * (Add to Home Screen / install to desktop, standalone window).
 *
 * eflow private change — keep behaviour conservative; do not add precaching
 * here without versioning + skipWaiting cache cleanup.
 */

self.addEventListener('install', () => {
  // Activate this SW immediately instead of waiting for old tabs to close.
  self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  // Take control of open clients and proactively drop any caches a previous
  // (or future-misconfigured) SW version might have left behind.
  event.waitUntil(
    (async () => {
      try {
        const keys = await caches.keys()
        await Promise.all(keys.map((k) => caches.delete(k)))
      } catch {
        /* ignore */
      }
      await self.clients.claim()
    })(),
  )
})

// No-op fetch handler: let the browser handle every request normally.
// Its mere presence is what makes the app installable.
self.addEventListener('fetch', () => {})
