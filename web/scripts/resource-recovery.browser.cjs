// Run in new-api-release-ui:pw1.58.2 against the synchronized Linux source volume.
// No application server or production data is needed for these bootstrap checks.
const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const { chromium } = require('playwright')

const web = path.resolve(__dirname, '..')
const messages = Object.fromEntries(
  ['en', 'zh', 'fr', 'ja', 'ru', 'vi'].map((locale) => [
    locale,
    JSON.parse(fs.readFileSync(path.join(web, 'src/i18n/locales', locale + '.json'), 'utf8')).translation,
  ])
)
const bootstrap = fs.readFileSync(path.join(web, 'bootstrap/resource-recovery.js'), 'utf8')
  .replace('__RESOURCE_RECOVERY_MESSAGES__', JSON.stringify(messages).replaceAll('<', '\\u003c'))

;(async () => {
  const browser = await chromium.launch({ headless: true })
  try {
    const context = await browser.newContext({ locale: 'zh-CN', viewport: { width: 900, height: 730 } })
    const page = await context.newPage()
    const errors = []
    page.on('pageerror', (error) => errors.push(error.message))
    let available = false
    let requests = 0
    let navigations = 0
    await context.route('http://recovery.test/**', async (route) => {
      const url = new URL(route.request().url())
      if (url.pathname === '/') {
        navigations++
        return route.fulfill({ contentType: 'text/html', body: `<!doctype html><html lang="zh"><head><meta charset="UTF-8"><script>${bootstrap}</script></head><body><textarea aria-label="草稿">未保存的内容</textarea></body></html>` })
      }
      requests++
      if (!available) return route.abort('failed')
      return route.fulfill({ contentType: 'application/javascript', body: 'window.fixtureLoaded = true' })
    })
    await page.goto('http://recovery.test/')
    await page.evaluate(() => {
      window.__newApiResourceRecovery.ready()
      window.startFixture = () => {
        window.fixturePromise = window.__newApiResourceRecovery.run('fixture', () => new Promise((resolve, reject) => {
          const script = document.createElement('script')
          script.src = '/static/js/fixture.js'
          script.onload = () => { script.remove(); resolve('loaded') }
          script.onerror = () => {
            script.remove()
            reject(Object.assign(new Error('Loading chunk fixture failed.'), { name: 'ChunkLoadError' }))
          }
          document.head.appendChild(script)
        }))
      }
      window.startFixture()
    })
    await page.getByRole('button', { name: '重试加载', exact: true }).waitFor({ timeout: 15000 })
    assert.equal(requests, 4)
    assert.equal(navigations, 1)
    assert.equal(await page.getByRole('textbox', { name: '草稿' }).inputValue(), '未保存的内容')
    fs.mkdirSync('/src/.cache/resource-recovery', { recursive: true })
    await page.screenshot({ path: '/src/.cache/resource-recovery/retry.png' })
    available = true
    await page.getByRole('button', { name: '重试加载', exact: true }).click()
    assert.equal(await page.evaluate(() => window.fixturePromise), 'loaded')
    await page.locator('#resource-recovery').waitFor({ state: 'hidden' })
    assert.equal(requests, 5)
    assert.equal(navigations, 1)

    await context.setOffline(true)
    await page.evaluate(() => window.startFixture())
    await page.getByText('网络连接已断开', { exact: true }).waitFor()
    assert.equal(requests, 5)
    await context.setOffline(false)
    assert.equal(await page.evaluate(() => window.fixturePromise), 'loaded')
    assert.equal(requests, 6)
    assert.equal(await page.getByRole('textbox', { name: '草稿' }).inputValue(), '未保存的内容')
    assert.equal(navigations, 1)

    // Entry failure is reported before React or the stylesheet exists.
    const entryPage = await context.newPage()
    available = false
    await entryPage.route('http://entry.test/**', (route) => {
      if (route.request().url() !== 'http://entry.test/') return route.abort('failed')
      return route.fulfill({ contentType: 'text/html', body: `<!doctype html><html><head><meta charset="UTF-8"><script>${bootstrap}</script><script defer src="/static/js/entry.js"></script></head><body><div id="root"></div></body></html>` })
    })
    await entryPage.goto('http://entry.test/')
    await entryPage.getByText('页面资源加载失败', { exact: true }).waitFor()
    await entryPage.getByRole('button', { name: '刷新页面', exact: true }).waitFor()
    assert.equal(await entryPage.locator('#root').innerHTML(), '')
    assert.deepEqual(errors, [])
    console.log('PASS: bounded network retries, manual recovery without navigation, offline resume, draft retention, entry fallback')
  } finally {
    await browser.close()
  }
})().catch((error) => { console.error(error); process.exitCode = 1 })
