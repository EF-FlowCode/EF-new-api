import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { runInNewContext } from 'node:vm'

class Element extends EventTarget {
  hidden = false
  id = ''
  textContent = ''
  style = { cssText: '' }
  children: Element[] = []
  attributes = new Map<string, string>()
  setAttribute(key: string, value: string) {
    this.attributes.set(key, value)
  }
  appendChild(child: Element) {
    this.children.push(child)
  }
  replaceChildren() {
    this.children = []
  }
}

type Recovery = {
  run: <T>(key: string, load: () => Promise<T>) => Promise<T>
  ready: () => void
  isResourceError: (error: unknown) => boolean
}

function setup(online = true, appReady = true) {
  const timers: { fn: () => void; ms: number }[] = []
  const window = Object.assign(new EventTarget(), {
    __newApiResourceRecovery: undefined as Recovery | undefined,
    location: {
      reload: () => {
        throw new Error('Unexpected automatic reload')
      },
    },
  })
  const document = Object.assign(new EventTarget(), {
    body: new Element(),
    createElement: () => new Element(),
  })
  const navigator = { onLine: online, language: 'en' }
  const source = readFileSync(
    new URL('../../bootstrap/resource-recovery.js', import.meta.url),
    'utf8'
  )
  runInNewContext(
    source.replace('__RESOURCE_RECOVERY_MESSAGES__', '{"en":{}}'),
    {
      window,
      document,
      navigator,
      localStorage: {
        getItem: () => {
          throw new Error('Storage blocked')
        },
      },
      setTimeout: (fn: () => void, ms: number) => {
        timers.push({ fn, ms })
        return fn
      },
      clearTimeout: (fn: () => void) => {
        const index = timers.findIndex((timer) => timer.fn === fn)
        if (index >= 0) timers.splice(index, 1)
      },
    }
  )
  assert.ok(window.__newApiResourceRecovery)
  if (appReady) window.__newApiResourceRecovery.ready()
  return {
    recovery: window.__newApiResourceRecovery,
    window,
    document,
    navigator,
    timers,
  }
}

const chunkError = () =>
  Object.assign(new Error('Loading chunk 42 failed.'), {
    name: 'ChunkLoadError',
  })
const settle = async () => {
  for (let i = 0; i < 12; i++) await Promise.resolve()
}

test('transient chunk failures recover, with shared concurrent imports and bounded backoff', async () => {
  const { recovery, timers } = setup()
  let calls = 0
  const load = async () => {
    if (++calls < 4) throw chunkError()
    return 'module'
  }
  const result = recovery.run('42', load)
  assert.equal(recovery.run('42', load), result)
  for (const delay of [1000, 2000, 4000]) {
    await settle()
    const timer = timers.shift()
    assert.equal(timer?.ms, delay)
    timer?.fn()
  }
  assert.equal(await result, 'module')
  assert.equal(calls, 4)
})

test('exhaustion pauses the same import until manual retry; no rejected lazy cache or reload', async () => {
  const { recovery, timers, document } = setup()
  let calls = 0
  const result = recovery.run('42', async () => {
    if (++calls <= 4) throw chunkError()
    return 'module'
  })
  for (let i = 0; i < 3; i++) {
    await settle()
    timers.shift()?.fn()
  }
  await settle()
  assert.equal(calls, 4)
  assert.equal(timers.length, 0)
  const panel = document.body.children[0]
  assert.equal(panel.hidden, false)
  // Heading, description, retry and refresh. Click only retry.
  panel.children[2].dispatchEvent(new Event('click'))
  assert.equal(await result, 'module')
  assert.equal(panel.hidden, true)
})

test('offline imports wait without polling, then resume on online; false online hints do not fetch', async () => {
  const { recovery, window, navigator, timers } = setup(false)
  let calls = 0
  const result = recovery.run('42', async () => {
    calls++
    return 'module'
  })
  await settle()
  assert.equal(calls, 0)
  assert.equal(timers.length, 0)
  window.dispatchEvent(new Event('online'))
  await settle()
  assert.equal(calls, 0)
  navigator.onLine = true
  window.dispatchEvent(new Event('online'))
  assert.equal(await result, 'module')
  assert.equal(calls, 1)
})

test('ordinary program and API errors reject immediately; only chunk/CSS download errors retry', async () => {
  const { recovery, timers } = setup()
  for (const error of [
    new Error('business error'),
    new TypeError('Failed to fetch'),
    new SyntaxError('invalid module'),
  ]) {
    await assert.rejects(
      recovery.run('42', async () => {
        throw error
      }),
      (actual) => actual === error
    )
  }
  assert.equal(timers.length, 0)
  assert.equal(
    recovery.isResourceError({ code: 'CSS_CHUNK_LOAD_FAILED' }),
    true
  )
  assert.equal(recovery.isResourceError(null), false)
})

test('entry fallback works without bundles or storage; app startup cancels slow-load warning', () => {
  const failed = setup(true, false)
  const script = Object.assign(new EventTarget(), {
    tagName: 'SCRIPT',
    defer: true,
  })
  const event = new Event('error')
  Object.defineProperty(event, 'target', { value: script })
  failed.window.dispatchEvent(event)
  assert.equal(failed.document.body.children[0].hidden, false)
  const ready = setup(true, false)
  ready.document.dispatchEvent(new Event('DOMContentLoaded'))
  assert.equal(ready.timers[0].ms, 20000)
  ready.recovery.ready()
  assert.equal(ready.timers.length, 0)
  assert.equal(ready.document.body.children.length, 0)
})
