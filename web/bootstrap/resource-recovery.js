// Inlined into HTML: this recovery UI must work even when JS/CSS bundles cannot load.
// Keep this file dependency-free. Messages are supplied from the six app locales.
;(function (messages) {
  var locale = navigator.language || 'en'
  try {
    locale = localStorage.getItem('i18nextLng') || locale
  } catch (_) {
    // Storage can be disabled in private browsing.
  }
  var t = messages[locale.split('-')[0]] || messages.en
  var pending = new Map()
  var started = false
  var startupFailed = false
  var startupSlow = false
  var panel
  var startupTimer

  function isResourceError(error) {
    return (
      !!error &&
      (error.name === 'ChunkLoadError' ||
        error.code === 'CSS_CHUNK_LOAD_FAILED' ||
        /Loading (?:CSS )?chunk [\s\S]* failed/i.test(error.message || ''))
    )
  }

  function render() {
    if (!document.body) return
    var records = Array.from(pending.values())
    var failed = records.filter(function (item) {
      return item.status === 'failed'
    })
    var offline = records.some(function (item) {
      return item.status === 'offline'
    })
    var visible = startupFailed || startupSlow || failed.length || offline
    if (!visible) {
      if (panel) panel.hidden = true
      return
    }
    if (!panel) {
      panel = document.createElement('section')
      panel.id = 'resource-recovery'
      panel.setAttribute('role', 'status')
      panel.setAttribute('aria-live', 'polite')
      panel.style.cssText =
        'position:fixed;bottom:16px;left:16px;right:16px;max-width:560px;margin:auto;padding:20px;border:1px solid #cbd5e1;border-radius:12px;background:#fff;color:#172033;box-shadow:0 4px 24px #0002;font:15px/1.6 system-ui,sans-serif;z-index:2147483647'
      document.body.appendChild(panel)
    }
    panel.hidden = false
    panel.replaceChildren()
    var heading = document.createElement('strong')
    heading.textContent =
      t[
        offline || navigator.onLine === false
          ? 'Resource connection offline'
          : startupSlow && !startupFailed && !failed.length
            ? 'Page loading is taking longer than expected'
            : 'Page resources could not be loaded'
      ]
    panel.appendChild(heading)
    var description = document.createElement('p')
    description.textContent =
      t[
        offline || navigator.onLine === false
          ? 'Loading will resume when your connection returns.'
          : 'Check your connection and try again. Refreshing may discard unsaved changes.'
      ]
    panel.appendChild(description)
    function button(label, action) {
      var element = document.createElement('button')
      element.type = 'button'
      element.textContent = t[label]
      element.style.cssText =
        'font:inherit;padding:6px 14px;margin-right:10px;border:1px solid #64748b;border-radius:6px;background:#f8fafc;color:#172033;cursor:pointer'
      element.addEventListener('click', action)
      panel.appendChild(element)
    }
    if (failed.length && navigator.onLine !== false) {
      button('Retry loading', function () {
        failed.forEach(function (item) {
          if (item.resume) item.resume()
        })
      })
    }
    button('Refresh page', function () {
      window.location.reload()
    })
  }

  function waitForResume(item, status) {
    item.status = status
    return new Promise(function (resolve) {
      item.resume = function () {
        item.resume = null
        item.status = 'loading'
        render()
        resolve()
      }
      render()
    })
  }

  function run(key, load) {
    var existing = pending.get(key)
    if (existing) return existing.promise
    var item = { status: 'loading', resume: null, promise: null }
    pending.set(key, item)
    item.promise = (async function () {
      var attempts = 0
      try {
        for (;;) {
          if (navigator.onLine === false) await waitForResume(item, 'offline')
          // An online event is only a hint; check again before issuing a request.
          if (navigator.onLine === false) continue
          try {
            return await load()
          } catch (error) {
            if (!isResourceError(error)) throw error
            if (navigator.onLine === false) continue
            if (attempts < 3) {
              await new Promise(function (resolve) {
                setTimeout(resolve, 1000 * Math.pow(2, attempts++))
              })
            } else {
              // Keep the import pending: TanStack/React lazy cache rejected imports.
              // Resuming the same promise lets the route recover without a reload.
              await waitForResume(item, 'failed')
              attempts = 0
            }
          }
        }
      } finally {
        pending.delete(key)
        render()
      }
    })()
    return item.promise
  }

  window.addEventListener('online', function () {
    pending.forEach(function (item) {
      if (item.status === 'offline' && item.resume) item.resume()
    })
    render()
  })
  window.addEventListener('offline', render)
  window.addEventListener(
    'error',
    function (event) {
      var target = event.target
      if (
        !started &&
        target &&
        ((target.tagName === 'SCRIPT' &&
          (target.defer || target.type === 'module')) ||
          (target.tagName === 'LINK' && target.rel === 'stylesheet'))
      ) {
        startupFailed = true
        render()
      }
    },
    true
  )
  document.addEventListener('DOMContentLoaded', render)
  // Deferred scripts can themselves delay DOMContentLoaded indefinitely.
  startupTimer = setTimeout(function () {
    startupSlow = true
    render()
  }, 20000)
  window.__newApiResourceRecovery = {
    run: run,
    isResourceError: isResourceError,
    ready: function () {
      started = true
      startupSlow = false
      clearTimeout(startupTimer)
      render()
    },
  }
})(__RESOURCE_RECOVERY_MESSAGES__)
