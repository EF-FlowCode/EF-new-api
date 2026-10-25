const {chromium, request} = require('playwright');
const mysql = require('mysql2/promise');
const fs = require('node:fs');
const assert = require('node:assert/strict');
const {createHash} = require('node:crypto');
const path = require('node:path');
const {responseBody, validateResponse, validateStream} = require('./task-http-protocol.cjs');
assert(fs.existsSync('/.dockerenv'), 'Run checks inside Docker');
const origin = 'http://web:3000';
const upstream = 'http://upstream:8080';
const password = 'TaskHttp2026!';
const statePath = '/evidence/state.json';
const reportPath = '/evidence/report.json';
const report = fs.existsSync(reportPath) ? JSON.parse(fs.readFileSync(reportPath)) : {scope: 'full application HTTP and browser, synthetic DB and providers; not production baseline acceptance', image: process.env.TASK_HTTP_IMAGE, checks: []};
assert.match(process.env.TASK_HTTP_IMAGE || '', /^sha256:[0-9a-f]{64}$/);
assert.equal(report.image, process.env.TASK_HTTP_IMAGE, 'all phases must use the same candidate');
assert.match(process.env.TASK_HTTP_SCRIPT_SHA || '', /^[0-9a-f]{64}$/);
const digest = value => createHash('sha256').update(value).digest('hex');
const manifest = ['task-http-acceptance.sh', 'task-http-acceptance.cjs', 'task-http-protocol.cjs', 'task-http-upstream.cjs'].map(name => digest(fs.readFileSync(path.join(__dirname, name))) + '  scripts/' + name + '\n').join('');
assert.equal(digest(manifest), process.env.TASK_HTTP_SCRIPT_SHA, 'executed scripts differ from frozen manifest');
if (report.scriptSHA) assert.equal(report.scriptSHA, process.env.TASK_HTTP_SCRIPT_SHA, 'acceptance scripts changed between phases');
report.scriptSHA = process.env.TASK_HTTP_SCRIPT_SHA;
const save = () => fs.writeFileSync(reportPath, JSON.stringify(report, null, 2));
const pass = (name, details = {}) => { report.checks.push({name, ...details}); save(); console.log('PASS:', name); };
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
let db, admin, browser, page, actor, state;
async function rows(q, args = []) { return (await db.execute(q, args))[0]; }
async function waitFor(fn, label, timeout = 100000) {
  const until = Date.now() + timeout; let last;
  while (Date.now() < until) { try { const value = await fn(); if (value) return value; } catch (error) { last = error; } await delay(500); }
  throw new Error('Timeout: ' + label + (last ? ': ' + last.message : ''));
}
async function raw(path, data, method, client = admin) {
  const response = await client.fetch(origin + path, {method: method || (data === undefined ? 'GET' : 'POST'), ...(data === undefined ? {} : {data}), headers: {'New-Api-User': String(actor?.id || 1)}});
  const body = await response.json(); return {status: response.status(), body};
}
async function api(path, data, method) { const r = await raw(path, data, method); assert.equal(r.status, 200, JSON.stringify(r)); assert.equal(r.body.success, true, path + ': ' + JSON.stringify(r.body)); return r.body.data; }
async function option(key, value) { await api('/api/option/', {key, value: typeof value === 'string' ? value : JSON.stringify(value)}, 'PUT'); }
async function pricing() { return JSON.parse((await rows("SELECT value FROM options WHERE `key`='ModelPricing'"))[0].value); }
async function balances() { const u = (await rows('SELECT quota,used_quota FROM users WHERE id=?', [actor.id]))[0]; const t = (await rows('SELECT remain_quota,used_quota FROM tokens WHERE id=?', [state.tokenID]))[0]; return {user: u.quota, token: t.remain_quota}; }
async function stats() { return await (await fetch(upstream + '/control/stats')).json(); }
async function task(id) { const r = (await rows('SELECT id,task_id,status,quota,private_data FROM tasks WHERE task_id=?', [id]))[0]; if (r) r.private_data = typeof r.private_data === 'string' ? JSON.parse(r.private_data) : r.private_data; return r; }
async function submit(model, key, expectedStatus = 200) {
  const r = await fetch(origin + '/v1/videos', {method: 'POST', headers: {'Content-Type': 'application/json', Authorization: 'Bearer sk-' + state.token, 'Idempotency-Key': key}, body: JSON.stringify({model, prompt: 'synthetic task', seconds: '5', duration: 5, size: '1280x720', resolution: '720P'})});
  const value = await r.json(); assert.equal(r.status, expectedStatus, JSON.stringify(value));
  if (expectedStatus === 409) { assert.equal(value.code, 'plugin_submission_failed'); return value.data.task_id; }
  assert(value.id || value.task_id, JSON.stringify(value)); return value.id || value.task_id;
}
async function finish(id, status = 'succeeded', expected, protocol = 'videos') {
  const saved = await task(id); const upstreamID = saved.private_data.upstream_task_id;
  assert(upstreamID);
  await fetch(upstream + '/control/finish', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({id: upstreamID, state: status, seconds: 7, tokens: 1000})});
  const final = await waitFor(async () => { const t = await task(id); return t.status === (status === 'failed' ? 'FAILURE' : 'SUCCESS') && t; }, 'worker terminal settlement ' + id);
  assert.equal(final.quota, expected);
  const fetched = await fetch(origin + '/v1/' + protocol + '/' + id, {headers: {Authorization: 'Bearer sk-' + state.token}});
  const publicTask = await fetched.json(); assert.equal(fetched.status, 200, JSON.stringify(publicTask));
  assert.equal(publicTask.status, protocol === 'tasks' ? (status === 'failed' ? 'FAILURE' : 'SUCCESS') : (status === 'failed' ? 'failed' : 'completed'));
  return final;
}
async function checkCost(before, expected) { const after = await balances(); assert.equal(before.user - after.user, expected); assert.equal(before.token - after.token, expected); return after; }
async function one(model, key, reserved, final) {
  const before = await balances(); const id = await submit(model, key);
  assert.equal((await task(id)).quota, reserved); await checkCost(before, reserved);
  await finish(id, 'succeeded', final); await checkCost(before, final);
  pass(key, {reserved, settled: final}); return id;
}

function responseRequest(body, key, signal) {
  return fetch(origin + '/v1/responses', {method: 'POST', headers: {'Content-Type': 'application/json', Authorization: 'Bearer sk-' + state.token, 'Idempotency-Key': key}, body: JSON.stringify(body), signal: signal || AbortSignal.timeout(120000)});
}
async function getResponse(id, status = 'completed') {
  const r = await fetch(origin + '/v1/responses/' + id, {headers: {Authorization: 'Bearer sk-' + state.token}});
  assert.equal(r.status, 200); return validateResponse(await r.json(), id, status);
}
async function responseCase(name, flags, model = 'http-amount', failed = false, disconnect = false) {
  const before = await balances(), sends = (await stats()).submits;
  const previous = (await rows('SELECT COALESCE(MAX(id),0) AS id FROM tasks'))[0].id;
  const body = responseBody(flags, model), key = 'responses-' + name;
  const abort = new AbortController();
  const timeout = setTimeout(() => abort.abort(), 120000);
  // Start the real request, then let the provider control endpoint release it.
  // Observing SQL is read-only; only the real worker is allowed to settle.
  const pending = responseRequest(body, key, abort.signal).then(response => ({response}), error => ({error}));
  try {
    const id = await waitFor(async () => {
      const found = await rows('SELECT task_id FROM tasks WHERE id>? AND user_id=?', [previous, actor.id]);
      assert(found.length <= 1, 'unexpected concurrent synthetic task');
      return found[0] && (await task(found[0].task_id)).private_data.upstream_task_id && found[0].task_id;
    }, name + ' accepted');
    await checkCost(before, 5);
    if (disconnect) {
      const opened = await pending; if (opened.error) throw opened.error;
      assert.equal(opened.response.status, 200);
      assert.equal(opened.response.headers.get('x-task-id'), id);
      abort.abort(); await opened.response.body.cancel().catch(() => {});
      await checkCost(before, 5);
    } else if (flags.background && !flags.stream) {
      const opened = await pending; if (opened.error) throw opened.error;
      assert.equal(opened.response.status, 200);
      const queued = await opened.response.json(); assert.equal(queued.id, id);
      assert(['queued', 'in_progress'].includes(queued.status));
    }
    await finish(id, failed ? 'failed' : 'succeeded', failed ? 0 : 5, 'responses');
    const expectedStatus = failed ? 'failed' : 'completed';
    if (!disconnect && (!flags.background || flags.stream)) {
      const completed = await pending; if (completed.error) throw completed.error;
      assert.equal(completed.response.status, 200);
      assert.equal(completed.response.headers.get('x-task-id'), id);
      const out = flags.stream ? validateStream(await completed.response.text(), id, expectedStatus) : validateResponse(await completed.response.json(), id, expectedStatus);
      assert.equal(out.background, Boolean(flags.background));
    }
    await getResponse(id, expectedStatus);
    const replay = await responseRequest(body, key);
    assert.equal(replay.status, failed ? 409 : 200);
    if (failed) {
      const error = (await replay.json()).error;
      assert.equal(error.code, 'plugin_submission_failed'); assert.equal(error.task_id, id);
    } else if (flags.stream) validateStream(await replay.text(), id);
    else validateResponse(await replay.json(), id);
    await checkCost(before, failed ? 0 : 5);
    assert.equal((await stats()).submits, sends + 1);
    pass('responses ' + name, {task: id, sends: 1, settled: failed ? 0 : 5});
  } finally { clearTimeout(timeout); abort.abort(); await pending; }
}

async function publicProtocolChecks() {
  for (const [name, flags] of [['sync', {}], ['background', {background: true}], ['stream', {stream: true}], ['combined', {background: true, stream: true}]]) await responseCase(name, flags);
  await responseCase('disconnect', {stream: true}, 'http-amount', false, true);
  await responseCase('failure refunds once', {stream: true}, 'http-amount', true);
  const before = await balances(), sends = (await stats()).submits;
  const payload = {model: 'http-amount', prompt: 'generic synthetic task', seconds: 5};
  const submitGeneric = () => fetch(origin + '/v1/tasks/doubao', {method: 'POST', headers: {'Content-Type': 'application/json', Authorization: 'Bearer sk-' + state.token, 'Idempotency-Key': 'generic-once'}, body: JSON.stringify(payload)});
  const r = await submitGeneric(); const value = await r.json(); assert.equal(r.status, 200, JSON.stringify(value));
  assert(value.task_id); await finish(value.task_id, 'succeeded', 5, 'tasks');
  const replay = await submitGeneric(); assert.equal(replay.status, 200); assert.equal((await replay.json()).task_id, value.task_id);
  assert.equal((await stats()).submits, sends + 1); await checkCost(before, 5);
  pass('generic entry real auth dispatch worker and replay');
  const original = 'doubao-seedance-1-0-pro-250528';
  const unrestricted = {token: state.token, tokenID: state.tokenID};
  try {
    for (const [allowed, denied, label] of [['http-amount', original, 'alias'], [original, 'http-amount', 'original']]) {
      await api('/api/token/', {name: 'protocol-' + label, remain_quota: 1000000, expired_time: -1, group: 'default', model_limits_enabled: true, model_limits: allowed});
      const token = (await rows('SELECT id,`key` FROM tokens WHERE user_id=? ORDER BY id DESC LIMIT 1', [actor.id]))[0];
      state.token = token.key; state.tokenID = token.id;
      const balance = await balances(), sent = (await stats()).submits, count = (await rows('SELECT COUNT(*) AS n FROM tasks'))[0].n;
      const reject = await responseRequest(responseBody({background: true}, denied), 'deny-' + label);
      assert.equal(reject.status, 403, await reject.text());
      assert.deepEqual(await balances(), balance); assert.equal((await stats()).submits, sent);
      assert.equal((await rows('SELECT COUNT(*) AS n FROM tasks'))[0].n, count);
      await responseCase(label + ' permission', {background: true}, allowed);
    }
  } finally { Object.assign(state, unrestricted); }
  pass('original and alias permissions deny before billing');
}
async function login() {
  admin = await request.newContext();
  await waitFor(async () => (await admin.get(origin + '/api/status')).ok(), 'web startup', 90000);
  actor = await api('/api/user/login', {username: 'task_http_root', password});
}
async function beforeRestart() {
  admin = await request.newContext();
  await waitFor(async () => (await admin.get(origin + '/api/status')).ok(), 'web startup', 90000);
  await api('/api/setup', {username: 'task_http_root', password, confirmPassword: password, SelfUseModeEnabled: false, DemoSiteEnabled: false});
  await admin.dispose(); await login();
  state = {actor, tokenID: 0};
  await option('fetch_setting.allow_private_ip', true);
  await option('GroupRatio', {default: 1});
  await option('ModelPricing', {version: 2, models: {'http-unit': {unit_price: 0.000003}, 'http-token': {input_ratio: 0.5}, 'http-amount': {mode: 'tiered_expr', expression: 'v2:0.00001'}, 'doubao-seedance-1-0-pro-250528': {mode: 'tiered_expr', expression: 'v2:0.00001'}}});
  await api('/api/plugin/task/runtime/settings', {enabled: true, factory_enabled: true}, 'PUT');
  for (const [type, key, version, names, mapping] of [
    [17, 'alibaba', '1.3.0', 'http-unit', {'http-unit': 'wan3.0-video'}],
    [54, 'doubao', '1.0.2', 'http-token,http-amount,doubao-seedance-1-0-pro-250528', {'http-token': 'doubao-seedance-1-0-pro-250528', 'http-amount': 'doubao-seedance-1-0-pro-250528'}],
  ]) {
    await api('/api/channel/', {mode: 'single', channel: {name: 'synthetic-' + key, type, key: 'synthetic-key', base_url: upstream, models: names, model_mapping: JSON.stringify(mapping), group: 'default', status: 1, priority: 0, weight: 1}});
    const cid = (await rows('SELECT id FROM channels WHERE name=?', ['synthetic-' + key]))[0].id;
    await api('/api/plugin/task/runtime/production/bindings/' + cid, {key, version, factory: true, enabled: true}, 'PUT');
  }
  await api('/api/token/', {name: 'synthetic', remain_quota: 10000000, expired_time: -1, group: 'default'});
  const token = (await rows('SELECT id,`key` FROM tokens WHERE user_id=? ORDER BY id DESC LIMIT 1', [actor.id]))[0];
  state.tokenID = token.id; state.token = token.key;
  // Synthetic fixture credentials are read only by the test, never included in public evidence.
  const anon = await request.newContext();
  for (const path of ['/api/option/task_pricing_migration', '/api/option/task_pricing_usage', '/api/plugin/task/runtime/settings']) {
    const r = await raw(path, undefined, 'GET', anon); assert.equal(r.status, 401);
  }
  const noToken = await anon.post(origin + '/v1/videos', {data: {model: 'http-unit', prompt: 'no-auth'}}); assert.equal(noToken.status(), 401);
  await anon.dispose(); pass('unauthenticated management and submission denied');
  await api('/api/user/', {username: 'task_http_user', password, role: 1, group: 'default'});
  const ordinary = await request.newContext();
  const ordinaryLogin = await ordinary.post(origin + '/api/user/login', {data: {username: 'task_http_user', password}});
  const ordinaryBody = await ordinaryLogin.json(); assert.equal(ordinaryBody.success, true);
  for (const method of ['GET', 'POST']) {
    const denied = await ordinary.fetch(origin + '/api/option/task_pricing_migration', {method, headers: {'New-Api-User': String(ordinaryBody.data.id)}, ...(method === 'POST' ? {data: {source_hash: '0'.repeat(64)}} : {})});
    const value = await denied.json(); assert.equal(value.success, false); assert([200, 403].includes(denied.status()));
  }
  await ordinary.dispose(); pass('ordinary authenticated user cannot preview or apply migration');
  await delay(1500);
  await one('http-unit', 'legacy-wan', 10, 14);
  await one('http-token', 'legacy-token', 125000, 500);
  await one('http-amount', 'legacy-amount', 5, 5);
  const beforeMigration = await pricing();
  const preview = await api('/api/option/task_pricing_migration'); assert.equal(preview.blocked, false, JSON.stringify(preview));
  const stale = await raw('/api/option/task_pricing_migration', {source_hash: '0'.repeat(64)}); assert.equal(stale.status, 409);
  assert.deepEqual(await pricing(), beforeMigration); pass('stale preview leaves prices unchanged');
  await api('/api/option/task_pricing_migration', {source_hash: preview.source_hash});
  const migrated = await pricing(); assert.equal(migrated.task_priority_version, 1);
  assert.equal(migrated.models['http-unit'].task_expressions.alibaba.kind, 'unit_price');
  assert.equal(migrated.models['http-token'].task_expressions.doubao.kind, 'token_completion');
  state.migrationHash = preview.source_hash;
  const backup = (await rows("SELECT value FROM options WHERE `key`='ModelPricingTaskMigrationV1'"))[0].value;
  state.backup = backup;
  await one('http-unit', 'migrated-wan', 10, 14);
  await one('http-token', 'migrated-token', 125000, 500);
  await one('http-amount', 'migrated-amount', 5, 5);
  await publicProtocolChecks();
  const before = await balances(); const initialStats = await stats();
  const pending = await submit('http-token', 'changed-price-once');
  const snapshot = (await task(pending)).private_data.plugin_context;
  const rules = Object.fromEntries(Object.entries(migrated.models).map(([name, value]) => [name, value.task_expressions]));
  rules['http-token'].doubao.model_ratio = 99;
  await option('billing_setting.task_expressions', rules);
  const replay = await submit('http-token', 'changed-price-once');
  assert.equal(replay, pending); assert.equal((await stats()).submits, initialStats.submits + 1);
  assert.deepEqual((await task(pending)).private_data.plugin_context, snapshot);
  await finish(pending, 'succeeded', 500); await checkCost(before, 500);
  pass('changed price and duplicate submission preserve original snapshot', {submits: 1, settled: 500});
  rules['http-token'].doubao.model_ratio = 0.5; await option('billing_setting.task_expressions', rules);
  const preFailure = await balances(); const failing = await submit('http-unit', 'failure-refund');
  await finish(failing, 'failed', 0); await checkCost(preFailure, 0);
  const stable = await balances(); const failureSubmits = (await stats()).submits;
  assert.equal(await submit('http-unit', 'failure-refund', 409), failing); assert.deepEqual(await balances(), stable);
  assert.equal((await stats()).submits, failureSubmits);
  pass('provider terminal failure refunds once');
  state.responseBefore = await balances();
  const responsePending = await responseRequest(responseBody({background: true}), 'responses-restart');
  assert.equal(responsePending.status, 200);
  state.responsePending = (await responsePending.json()).id;
  assert(state.responsePending);
  state.responseSnapshot = (await task(state.responsePending)).private_data.plugin_context;
  await checkCost(state.responseBefore, 5);
  state.pendingBefore = await balances(); state.pending = await submit('http-token', 'restart-inflight');
  state.pendingSnapshot = (await task(state.pending)).private_data.plugin_context;
  state.pricing = await pricing(); state.submits = (await stats()).submits;
  fs.writeFileSync(statePath, JSON.stringify(state));
  pass('inflight task persisted before process restart');
}
async function afterRestart() {
  state = JSON.parse(fs.readFileSync(statePath)); await login();
  assert.deepEqual(await pricing(), state.pricing);
  assert.equal((await rows("SELECT value FROM options WHERE `key`='ModelPricingTaskMigrationV1'"))[0].value, state.backup);
  const again = await api('/api/option/task_pricing_migration', {source_hash: state.migrationHash}); assert.equal(again.already_applied, true);
  assert.deepEqual(await pricing(), state.pricing);
  assert.equal(await submit('http-token', 'restart-inflight'), state.pending);
  assert.equal((await stats()).submits, state.submits);
  assert.deepEqual((await task(state.pending)).private_data.plugin_context, state.pendingSnapshot);
  await finish(state.pending, 'succeeded', 500); await checkCost(state.pendingBefore, 500);
  const responseReplay = await responseRequest(responseBody({background: true}), 'responses-restart');
  assert.equal(responseReplay.status, 200);
  assert.equal((await responseReplay.json()).id, state.responsePending);
  assert.equal((await stats()).submits, state.submits);
  assert.deepEqual((await task(state.responsePending)).private_data.plugin_context, state.responseSnapshot);
  await finish(state.responsePending, 'succeeded', 5, 'responses');
  await getResponse(state.responsePending); await checkCost(state.responseBefore, 505);
  pass('responses restart restores inflight task without second reservation');
  pass('web and worker restart retain migration and inflight billing');
  await browserChecks();
}
async function browserChecks() {
  browser = await chromium.launch({headless: true});
  const context = await browser.newContext({viewport: {width: 1440, height: 1000}});
  await context.addInitScript(() => { localStorage.setItem('i18nextLng', 'en'); });
  page = await context.newPage();
  page.setDefaultTimeout(20000);
  const errors = []; page.on('pageerror', error => errors.push(error.message));
  await page.goto(origin + '/system-settings/models/ratio');
  await page.getByLabel('Username or Email', {exact: true}).fill('task_http_root');
  await page.getByLabel('Password', {exact: true}).fill(password);
  const loginResponse = page.waitForResponse(r => r.url().includes('/api/user/login') && r.request().method() === 'POST');
  await page.getByRole('button', {name: 'Sign in', exact: true}).click();
  assert.equal((await (await loginResponse).json()).success, true);
  await page.waitForURL(url => !url.pathname.includes('sign-in'));
  await page.goto(origin + '/system-settings/models/ratio');
  await page.getByRole('tab', {name: 'Plugin task prices', exact: true}).click();
  await page.getByLabel('Model name', {exact: true}).fill('http-unit');
  const priceInput = page.getByLabel('Unit price (USD)', {exact: true});
  assert.equal(Number(await priceInput.inputValue()), 0.000003);
  assert.equal(await page.getByRole('button', {name: 'Save', exact: true}).isDisabled(), true);
  await priceInput.fill('0.000004');
  const saved = page.waitForResponse(r => r.url().endsWith('/api/option/') && r.request().method() === 'PUT');
  await page.getByRole('button', {name: 'Save', exact: true}).click();
  assert.equal((await (await saved).json()).success, true);
  assert.equal((await pricing()).models['http-unit'].task_expressions.alibaba.unit_price, 0.000004);
  await page.reload(); await page.getByRole('tab', {name: 'Plugin task prices', exact: true}).click();
  await page.getByLabel('Model name', {exact: true}).fill('http-unit');
  assert.equal(Number(await priceInput.inputValue()), 0.000004);
  await page.screenshot({path: '/evidence/task-price-saved.png', fullPage: true});
  assert.deepEqual(errors, []);
  pass('browser actual session reads edits saves and reloads structured price', {apiMocked: false, pageErrors: errors});
  await one('http-unit', 'browser-saved-price', 20, 28);
  const required = ['responses sync', 'responses background', 'responses stream', 'responses combined', 'responses disconnect', 'responses failure refunds once', 'generic entry real auth dispatch worker and replay', 'original and alias permissions deny before billing', 'responses restart restores inflight task without second reservation', 'web and worker restart retain migration and inflight billing'];
  for (const name of required) assert(report.checks.some(check => check.name === name), 'missing required check: ' + name);
  report.completed = true; report.provider = await stats();
  if (report.failure) { report.previous_failure = report.failure; delete report.failure; }
  save();
}
(async () => {
  try {
    db = await mysql.createConnection({host: 'mysql', user: 'root', password: 'TaskHttpSyntheticOnly', database: 'task_http'});
    if (process.argv[2] === 'before-restart') await beforeRestart();
    else if (process.argv[2] === 'after-restart') await afterRestart();
    else if (process.argv[2] === 'browser-only') { state = JSON.parse(fs.readFileSync(statePath)); await login(); await browserChecks(); }
    else throw new Error('unknown phase');
  } catch (error) { report.failure = error.stack; save(); if (page) { await page.screenshot({path: '/evidence/failure.png', fullPage: true}).catch(() => {}); fs.writeFileSync('/evidence/failure.html', await page.content().catch(() => '')); } console.error(error); process.exitCode = 1; }
  finally { if (browser) await browser.close(); if (admin) await admin.dispose(); if (db) await db.end(); }
})();
