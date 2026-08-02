/**
 * Build the PWA web-app manifest at runtime from the deployment's configured
 * System Name + Logo — the same source that already drives `document.title`
 * and the favicon (see `main.tsx` / `applyFaviconToDom`). The installed app
 * (window title, taskbar/home-screen icon, splash) therefore shows whatever
 * branding the admin set in System Settings, instead of any value baked into
 * the static bundle.
 *
 * Generic by design: no deployment-specific name/logo is hardcoded here — it
 * reflects `/api/status`. Icons are rendered to square 192/512 (+ maskable)
 * PNGs on a canvas so a non-square logo still produces installable icons.
 *
 * Note: an already-installed PWA does not repaint its name/icon instantly —
 * Chromium refreshes installed-app metadata in the background (or on relaunch);
 * reinstall for an immediate change.
 */

type ManifestIcon = {
  src: string
  sizes: string
  type: string
  purpose: string
}

let lastAppliedKey = ''

export function applyPwaManifest(systemName?: string, logoUrl?: string): void {
  if (typeof document === 'undefined' || typeof window === 'undefined') return
  const name = (systemName || '').trim()
  const logo = (logoUrl || '').trim()
  if (!name && !logo) return

  const key = `${name}|${logo}`
  if (key === lastAppliedKey) return
  lastAppliedKey = key

  // Fire-and-forget: icon rendering waits on an <img> load.
  void (async () => {
    try {
      const icons = logo ? await buildIconsFromLogo(logo) : []
      const manifest = {
        name: name || undefined,
        short_name: name || undefined,
        start_url: '/',
        scope: '/',
        display: 'standalone',
        background_color: '#ffffff',
        theme_color: '#efe7d8',
        icons,
      }
      const blob = new Blob([JSON.stringify(manifest)], {
        type: 'application/manifest+json',
      })
      const url = URL.createObjectURL(blob)

      let link = document.querySelector<HTMLLinkElement>('link[rel="manifest"]')
      if (!link) {
        link = document.createElement('link')
        link.rel = 'manifest'
        document.head.appendChild(link)
      }
      const prev = link.href
      link.href = url
      if (prev.startsWith('blob:')) URL.revokeObjectURL(prev)

      // iOS home-screen icon (Safari ignores the manifest) + app title.
      if (icons.length) applyAppleTouchIcon(icons[0].src)
      if (name) applyMeta('apple-mobile-web-app-title', name)
    } catch {
      // Logo unreachable / canvas tainted / etc. — keep the current manifest.
      lastAppliedKey = ''
    }
  })()
}

async function buildIconsFromLogo(logo: string): Promise<ManifestIcon[]> {
  const img = await loadImage(logo)
  return [
    icon(render(img, 192, 0.86), '192x192', 'any'),
    icon(render(img, 512, 0.86), '512x512', 'any'),
    // maskable: keep the logo inside the ~80% safe zone
    icon(render(img, 512, 0.72), '512x512', 'maskable'),
  ]
}

function icon(src: string, sizes: string, purpose: string): ManifestIcon {
  return { src, sizes, type: 'image/png', purpose }
}

function loadImage(url: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image()
    img.crossOrigin = 'anonymous' // needed so canvas.toDataURL() isn't tainted
    img.onload = () => resolve(img)
    img.onerror = reject
    img.src = url
  })
}

// Draw the logo "contain"-fitted and centered on a square `size` canvas with a
// white background (the logo art is white-bg line art), return a PNG data URL.
function render(img: HTMLImageElement, size: number, scale: number): string {
  const canvas = document.createElement('canvas')
  canvas.width = size
  canvas.height = size
  const ctx = canvas.getContext('2d')
  if (!ctx) throw new Error('no 2d context')
  ctx.fillStyle = '#ffffff'
  ctx.fillRect(0, 0, size, size)

  const target = size * scale
  const ratio = Math.min(target / img.width, target / img.height)
  const w = img.width * ratio
  const h = img.height * ratio
  ctx.drawImage(img, (size - w) / 2, (size - h) / 2, w, h)
  return canvas.toDataURL('image/png')
}

function applyAppleTouchIcon(href: string): void {
  let link = document.querySelector<HTMLLinkElement>(
    'link[rel="apple-touch-icon"]',
  )
  if (!link) {
    link = document.createElement('link')
    link.rel = 'apple-touch-icon'
    document.head.appendChild(link)
  }
  link.href = href
}

function applyMeta(name: string, content: string): void {
  let meta = document.querySelector<HTMLMetaElement>(`meta[name="${name}"]`)
  if (!meta) {
    meta = document.createElement('meta')
    meta.name = name
    document.head.appendChild(meta)
  }
  meta.content = content
}
