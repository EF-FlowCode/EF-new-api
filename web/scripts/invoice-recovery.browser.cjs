// Exercise the actual production bundles with local fixtures and intercepted
// network responses. No application server, accounts or real invoices are used.
const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const { chromium } = require('playwright')

const dist = path.resolve(process.env.FRONTEND_DIST || '/src/web/dist')
const html = fs.readFileSync(path.join(dist, 'index.html'), 'utf8')
const initialScripts = new Set(Array.from(html.matchAll(/<script\b[^>]*\bsrc=["']([^"']+)/g), (match) => new URL(match[1], 'http://invoice.test').pathname))
const t = JSON.parse(fs.readFileSync('/src/web/src/i18n/locales/zh.json', 'utf8')).translation
const workbook = fs.readFileSync('/src/invoice-export-fixture.xlsx')
const tickets = Array.from({ length: 11 }, (_, index) => ({ id: index + 1, user_id: 7, username: 'applicant', subject: '示例大学发票申请 ' + (index + 1), type: 'invoice', status: 1, priority: 2, created_time: 1790000000, updated_time: 1790000000 }))

;(async () => {
  const browser = await chromium.launch({ headless: true })
  let page, diagnostics = () => ({})
  try {
    const context = await browser.newContext({ locale: 'zh-CN', viewport: { width: 1440, height: 1000 }, serviceWorkers: 'block', acceptDownloads: true })
    const user = { id: 50, username: 'invoice-admin', role: 10, status: 1, quota: 100000, group: 'default' }
    await context.addInitScript((user) => {
      localStorage.setItem('user', JSON.stringify(user))
      localStorage.setItem('i18nextLng', 'zh')
    }, user)
    let blockedPath = '', blockedRequests = 0, allowChunk = false, documents = 0
    const searches = [], exports = [], external = [], errors = []
    await context.route('**/*', async (route) => {
      const request = route.request()
      const url = new URL(request.url())
      if (url.origin !== 'http://invoice.test') { external.push(request.url()); return route.abort() }
      const json = (data) => route.fulfill({ json: { success: true, data } })
      if (url.pathname === '/api/setup') return json({ status: true })
      if (url.pathname === '/api/status') return json({ system_name: 'Invoice verification', version: 'test', quota_per_unit: 500000, display_in_currency: true })
      if (url.pathname === '/api/user/self') return json(user)
      if (url.pathname === '/api/notice') return json('')
      if (url.pathname.replace(/\/$/, '') === '/api/ticket/admin') {
        searches.push(Object.fromEntries(url.searchParams))
        const name = url.searchParams.get('company_name') || ''
        const matches = name && !'示例大学'.includes(name) ? [] : tickets
        const page = Number(url.searchParams.get('p') || 1)
        const size = Number(url.searchParams.get('page_size') || 10)
        return json({ items: matches.slice((page - 1) * size, page * size), total: matches.length })
      }
      if (url.pathname === '/api/ticket/admin/invoice-export') {
        exports.push(request.postDataJSON())
        return route.fulfill({ contentType: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet', headers: { 'content-disposition': 'attachment; filename="invoice-requests-20260928-120000.xlsx"' }, body: workbook })
      }
      if (url.pathname.startsWith('/api/')) return json([])
      if (url.pathname === '/__maintenance/status') return route.fulfill({ json: { maintenance: false } })
      if (request.resourceType() === 'document') {
        documents++
        return route.fulfill({ contentType: 'text/html', body: html })
      }
      if (url.pathname.endsWith('.js') && !initialScripts.has(url.pathname)) {
        if (!blockedPath) blockedPath = url.pathname
        if (url.pathname === blockedPath && !allowChunk) { blockedRequests++; return route.abort('failed') }
      }
      const file = path.resolve(dist, '.' + url.pathname)
      if (!file.startsWith(dist + path.sep) || !fs.existsSync(file)) return route.fulfill({ status: 404 })
      const mime = { '.js': 'application/javascript', '.css': 'text/css', '.json': 'application/json', '.woff2': 'font/woff2', '.png': 'image/png', '.svg': 'image/svg+xml', '.ico': 'image/x-icon' }[path.extname(file)] || 'application/octet-stream'
      return route.fulfill({ contentType: mime, body: fs.readFileSync(file) })
    })
    page = await context.newPage()
    page.setDefaultTimeout(10000)
    diagnostics = () => ({ searches, exports, errors, blockedPath, blockedRequests, documents })
    page.on('pageerror', (error) => errors.push(error.message))
    page.on('console', (message) => {
      if (message.type() === 'error' && !message.text().includes('Failed to load resource')) errors.push(message.text())
    })
    await page.goto('http://invoice.test/admin-tickets', { waitUntil: 'domcontentloaded' })
    await page.getByRole('button', { name: t['Retry loading'], exact: true }).waitFor({ timeout: 25000 })
    assert.equal(blockedRequests, 4, 'production Rspack downloads must share the retry budget')
    assert.equal(documents, 1, 'TanStack must not auto-refresh')
    allowChunk = true
    await page.getByRole('button', { name: t['Retry loading'], exact: true }).click()
    await page.getByLabel(t['Search invoice title'], { exact: true }).waitFor({ timeout: 20000 })
    await page.locator('#resource-recovery').waitFor({ state: 'hidden' })
    assert.equal(documents, 1, 'retry must resume the original lazy route')
    const firstSearch = page.waitForResponse((response) => response.url().includes('company_name=') && response.url().includes('/api/ticket/admin')).catch(() => null)
    await page.getByLabel(t['Search invoice title'], { exact: true }).fill('大学')
    await page.locator('form').filter({ has: page.getByLabel(t['Search invoice title'], { exact: true }) }).getByRole('button', { name: t.Search, exact: true }).click()
    if (!await firstSearch) {
      console.error(JSON.stringify({ searches, errors, url: page.url(), body: (await page.locator('body').innerText()).slice(-3500) }))
      throw new Error('Title search did not send company_name')
    }
    await page.getByRole('checkbox', { name: t['Select ticket'], exact: true }).first().check()
    await page.getByRole('button', { name: t.Next, exact: true }).click()
    await page.waitForFunction((subject) => document.body.textContent.includes(subject), '示例大学发票申请 11')
    await page.getByRole('checkbox', { name: t['Select ticket'], exact: true }).first().check()
    const downloadPromise = page.waitForEvent('download')
    await page.getByRole('button', { name: t['Export selected invoices'], exact: true }).click()
    const download = await downloadPromise
    assert.match(download.suggestedFilename(), /\.xlsx$/)
    assert.deepEqual(exports, [{ ids: [1, 11] }])
    const lastSearch = searches.at(-1)
    assert.equal(lastSearch.company_name, '大学')
    assert.equal(lastSearch.type, 'invoice')
    assert.equal(lastSearch.p, '2')
    const output = '/src/.cache/invoice-recovery'
    fs.mkdirSync(output, { recursive: true })
    await download.saveAs(path.join(output, 'invoice-download.xlsx'))
    assert.equal(fs.readFileSync(path.join(output, 'invoice-download.xlsx')).compare(workbook), 0)
    await page.screenshot({ path: path.join(output, 'invoice-search-export.png'), fullPage: true })
    // A new search clears a cross-page selection, preventing stale exports.
    const searchResponse = page.waitForResponse((response) => response.url().includes('company_name=') && response.url().includes('/api/ticket/admin'))
    await page.getByLabel(t['Search invoice title'], { exact: true }).fill('不存在的单位')
    await page.locator('form').filter({ has: page.getByLabel(t['Search invoice title'], { exact: true }) }).getByRole('button', { name: t.Search, exact: true }).click()
    await searchResponse
    await page.getByText(t['No tickets'], { exact: true }).waitFor()
    assert.equal(await page.getByRole('button', { name: t['Export selected invoices'], exact: true }).count(), 0)
    assert.equal(searches.at(-1).p, '1')
    assert.equal(documents, 1)
    assert.equal(external.filter((url) => /fonts\.google/.test(url)).length, 0)
    assert.deepEqual(errors, [])
    console.log(JSON.stringify({ result: 'PASS', productionChunk: blockedPath, failedDownloads: blockedRequests, documentNavigations: documents, exportedIDs: exports[0].ids, searchAndPagination: 'passed', selectionReset: 'passed' }))
  } catch (error) {
    console.error(JSON.stringify({ ...diagnostics(), url: page?.url(), body: page ? (await page.locator('body').innerText()).slice(-4500) : '' }))
    if (page) {
      fs.mkdirSync('/src/.cache/invoice-recovery', { recursive: true })
      await page.screenshot({ path: '/src/.cache/invoice-recovery/failure.png', fullPage: true })
    }
    throw error
  } finally {
    await browser.close()
  }
})().catch((error) => { console.error(error); process.exitCode = 1 })
