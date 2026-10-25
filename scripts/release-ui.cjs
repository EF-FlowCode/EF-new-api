const { chromium } = require('playwright');
const mysql = require('mysql2/promise');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const root = process.env.RELEASE_DIR;
assert(root && process.env.RELEASE_IMAGE_ID, 'Run through the release validator');
assert(fs.existsSync('/.dockerenv'), 'API/UI checks must run inside Docker');
const releaseState = JSON.parse(fs.readFileSync(path.join(root, 'state.json'), 'utf8'));
const auditedOAuth = releaseState.plan.compatibility?.id === 'oauth-critical-limit-20260921';
const target = releaseState.validation_target;
assert(target && /^newapi-upgrade-[a-f0-9]{12}-web$/.test(target.web), 'A baseline clone is required');
assert(target.owner === releaseState.id && target.baseline_generation === releaseState.baseline_generation);
assert(/^http:\/\/127\.0\.0\.1:\d+$/.test(target.origin), 'Only loopback clone URLs are accepted');
const runtime = JSON.parse(fs.readFileSync(path.join(root, 'ui-input.json'), 'utf8'));
assert.equal(runtime.origin, 'http://' + target.prefix + '-gateway:3000');
assert.equal(runtime.mysql.host, target.mysql);
assert.equal(runtime.mysql.database, 'new_api');
const origin = runtime.origin;
const model = '__release_ui_' + crypto.randomBytes(6).toString('hex');
let connection, actor, oldToken, original;
async function sql(q) {
  const [rows] = await connection.query(q);
  return Array.isArray(rows) ? rows.map(row => row.join('\t')).join('\n').trim() : '';
}
async function pricing() { return JSON.parse(Buffer.from(await sql("SELECT HEX(value) FROM options WHERE `key`='ModelPricing'"),'hex').toString()); }
// Candidates with scoped access tokens get a temporary Root nap_ token row
// (removed in finally); older baselines fall back to the legacy users.access_token.
let token = crypto.randomBytes(16).toString('hex');
let patId = null;
async function grantTemporaryToken() {
  const hasTable = await sql("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='user_access_tokens'");
  if (hasTable === '0') {
    oldToken = await sql('SELECT IF(access_token IS NULL,\'NULL\',HEX(access_token)) FROM users WHERE id='+actor.id);
    await sql("UPDATE users SET access_token='"+token+"' WHERE id="+actor.id);
    return;
  }
  token = 'nap_' + crypto.randomBytes(22).toString('hex').slice(0, 43);
  const scopes = fs.readFileSync(path.join(__dirname, 'release-pat-scopes.json'), 'utf8');
  const hash = crypto.createHash('sha256').update(token).digest('hex');
  const now = Math.floor(Date.now() / 1000);
  await connection.query('INSERT INTO user_access_tokens (user_id,name,token_hash,token_hint,scopes,expires_at,last_used_at,last_used_ip,created_at) VALUES (?,?,?,?,?,?,0,\'\',?)',
    [actor.id, '__release_ui', hash, token.slice(-4), JSON.stringify(JSON.parse(scopes)), now + 7200, now]);
  patId = Number(await sql("SELECT id FROM user_access_tokens WHERE token_hash='"+hash+"'"));
}
async function revokeTemporaryToken() {
  if (patId !== null) {
    await sql('DELETE FROM user_access_tokens WHERE id='+patId);
    return;
  }
  if (oldToken !== undefined) {
    await sql('UPDATE users SET access_token='+(oldToken==='NULL'?'NULL':"CONVERT(0x"+oldToken+" USING utf8mb4)")+' WHERE id='+actor.id);
  }
}
const keys = ['ModelRatio','CompletionRatio','CacheRatio','CreateCacheRatio','ImageRatio','AudioRatio','AudioCompletionRatio','BatchInputRatio','ModelPrice','billing_setting.billing_mode','billing_setting.billing_expr'];
let browser;
async function api(endpoint, options={}) {
  const response = await fetch(origin+endpoint,{...options, headers:{'Authorization':'Bearer '+token,'New-Api-User':String(actor.id),'Content-Type':'application/json'}});
  const data = await response.json(); assert(data.success, data.message); return data;
}
(async()=>{
  connection = await mysql.createConnection({...runtime.mysql, charset:'utf8mb4', rowsAsArray:true, jsonStrings:true});
  actor = JSON.parse(await sql("SELECT JSON_OBJECT('id',id,'username',username,'display_name',display_name,'role',role,'status',status,'quota',quota,'group',`group`) FROM users WHERE role=100 AND deleted_at IS NULL ORDER BY id LIMIT 1"));
  original = await pricing();
  await grantTemporaryToken();
  try {
    browser = await chromium.launch({headless:true});
    const context=await browser.newContext({viewport:{width:1440,height:1100}});
    let maintenanceMode = false;
    let writeRequests = 0;
    context.on('request', request => {
      if (request.url().startsWith(origin + '/api/') && ['POST', 'PUT', 'DELETE'].includes(request.method())) writeRequests++;
    });
    await context.route('**/*', route=>{
      if (!route.request().url().startsWith(origin+'/')) return route.abort();
      const headers=route.request().headers();
      if (route.request().url().startsWith(origin+'/api/')) Object.assign(headers,{'authorization':'Bearer '+token,'new-api-user':String(actor.id)});
      return route.continue({headers});
    });
    await context.route('**/__maintenance/status', route => route.fulfill({
      status: 200, contentType: 'application/json', headers: { 'Cache-Control': 'no-store' },
      body: JSON.stringify({ maintenance: maintenanceMode }),
    }));
    await context.addInitScript(user=>{ localStorage.setItem('user',JSON.stringify(user)); localStorage.setItem('i18nextLng','en'); },actor);
    const page=await context.newPage();
    page.setDefaultTimeout(15000);
    await page.goto(origin+'/system-settings/models/ratio');
    await page.getByRole('button',{name:'Add model',exact:true}).click();
    const fields={inputPrice:'2',outputPrice:'3',imageInputPrice:'4',imageOutputPrice:'5',audioInputPrice:'6',audioOutputPrice:'7',batchInputPrice:'0'};
    await page.locator('#pricing-model-name').fill(model);
    for (const [field,value] of Object.entries(fields)) await page.locator('#token-price-'+field).fill(value);
    await page.getByRole('dialog').getByRole('button',{name:'Add',exact:true}).click();
    async function save() {
      const response=page.waitForResponse(r=>r.url().endsWith('/api/option/bulk') && r.request().method()==='PUT');
      await page.getByRole('button',{name:'Save model ratios',exact:true}).click();
      assert((await (await response).json()).success);
    }
    async function edit() {
      await page.reload();
      await page.getByPlaceholder('Search models...').fill(model);
      await page.getByRole('row').filter({hasText:model}).getByRole('button').first().click();
    }
    await save(); await edit();
    for (const [field,value] of Object.entries(fields)) assert.equal(await page.locator('#token-price-'+field).inputValue(),value);
    await page.locator('#token-price-inputPrice').fill('0');
    await page.getByRole('dialog').getByRole('button',{name:'Update',exact:true}).click();
    await save(); await edit();
    assert.equal(await page.locator('#token-price-inputPrice').inputValue(),'0');
    assert.equal(await page.locator('#token-price-batchInputPrice').inputValue(),'0');
    await page.screenshot({path:path.join(root,'ui-saved-prices.png'),fullPage:true,animations:'disabled'});
    await page.locator('#token-price-inputPrice').fill('9');
    if (!auditedOAuth) {
    const beforeMaintenanceWrites = writeRequests;
    maintenanceMode = true;
    await page.getByText('Maintenance in progress', { exact: true }).waitFor({timeout:40000});
    assert.equal(await page.locator('#token-price-inputPrice').inputValue(), '9');
    assert.equal(writeRequests, beforeMaintenanceWrites, 'Maintenance must not replay writes');
    await page.screenshot({path:path.join(root,'ui-maintenance.png'),fullPage:true,animations:'disabled'});
    maintenanceMode = false;
    await page.getByText('Maintenance in progress', { exact: true }).waitFor({state:'hidden',timeout:25000});
    assert.equal(await page.locator('#token-price-inputPrice').inputValue(), '9');
    assert.equal(writeRequests, beforeMaintenanceWrites, 'Recovery must not replay writes');
    }
    await page.getByRole('dialog').getByRole('button',{name:'Cancel',exact:true}).click();
    await page.getByRole('row').filter({hasText:model}).getByRole('button').last().click();
    await save();
    assert.deepEqual(await pricing(),original);
    await page.goto(origin+'/pricing');
    await page.getByPlaceholder(/Search/i).first().waitFor({state:'visible'});
    await page.screenshot({path:path.join(root,'ui-public-pricing.png'),fullPage:true,animations:'disabled'});
    const oauth = await fetch(origin + '/api/oauth/state');
    assert.equal(oauth.status, 200);
    assert.equal((await oauth.json()).success, true);
    const invalidCallback = await fetch(origin + '/api/oauth/github?state=invalid&code=invalid');
    assert.equal(invalidCallback.status, 403);
    fs.writeFileSync(path.join(root,'api_and_ui.json'),JSON.stringify({result:'passed',candidate_image_id:process.env.RELEASE_IMAGE_ID,
      browser:'Chromium',checks:{nativePriceFieldsReadWrite:true,optionalImageAudioBatch:true,zeroTextAndBatchPersist:true,refreshReopen:true,temporaryModelRemoved:true,allOriginalPricingUnchanged:true,publicPricingRendered:true},
      oauthStateAccessible:true,oauthInvalidStateRejected:true,
      maintenanceNotice:auditedOAuth ? 'not included in audited baseline UI' : 'draft and no replay verified',
      models:Object.keys(original.models).length,screenshots:['ui-saved-prices.png','ui-public-pricing.png',...(!auditedOAuth ? ['ui-maintenance.png'] : [])]},null,2));
    console.log('PRICING_BROWSER_READ_MODIFY_SAVE_RELOAD_PASSED');
  } catch(error) {
    if(browser) { const pages=browser.contexts().flatMap(x=>x.pages()); if(pages.length) { fs.writeFileSync(path.join(root,'ui-failure.txt'),await pages[0].locator('body').innerText()); await pages[0].screenshot({path:path.join(root,'ui-failure.png')}); } }
    throw error;
  } finally {
    try {
      const current=await pricing();
      if(current.models[model]) {
        const options=(await api('/api/option/')).data.filter(x=>keys.includes(x.key)).map(x=>{const value=JSON.parse(x.value);delete value[model];return {key:x.key,value:JSON.stringify(value)};});
        await api('/api/option/bulk',{method:'PUT',body:JSON.stringify({options})});
      }
      assert.deepEqual(await pricing(),original);
    } finally {
      await revokeTemporaryToken();
      if(browser) await browser.close();
    }
  }
})().catch(error=>{console.error(error.message);process.exitCode=1;}).finally(async()=>{if(connection) await connection.end();});
