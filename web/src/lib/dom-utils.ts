export function applyFaviconToDom(url: string) {
  if (typeof document === 'undefined' || !url) return
  try {
    const next = new URL(url, window.location.href).href
    const existing =
      document.querySelectorAll<HTMLLinkElement>('link[rel~="icon"]')
    if (existing.length === 1 && existing[0].href === next) return
    const link = document.createElement('link')
    link.rel = 'icon'
    link.href = url
    existing.forEach((l) => l.remove())
    document.head.appendChild(link)
  } catch {
    // Ignore malformed URLs
  }
}

/** 快捷键提示用的修饰键：Apple 平台显示 ⌘，其余显示 Ctrl */
export function getModifierKeyLabel(): string {
  if (typeof navigator === 'undefined') return 'Ctrl'
  return /Mac|iPhone|iPad|iPod/i.test(navigator.platform || navigator.userAgent)
    ? '⌘'
    : 'Ctrl'
}
