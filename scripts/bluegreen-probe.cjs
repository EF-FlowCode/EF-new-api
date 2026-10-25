const fs = require('node:fs');
const assert = require('node:assert/strict');
const { createHash } = require('node:crypto');
const { chromium } = require('playwright');
const mysql = require('mysql2/promise');
const c = JSON.parse(fs.readFileSync('/work/input.json', 'utf8'));
assert(fs.existsSync('/.dockerenv'));
const output = '/work/output'; fs.mkdirSync(output, {recursive:true});
let db, browser, cookie, polling = true, stream, streamReader;
const probes = [], errors = [], report = {result:'running'};
const pause = ms => new Promise(r => setTimeout(r, ms));
const mark = name => fs.writeFileSync('/work/' + name, '');
async function wait(name) { const end=Date.now()+240000; while(!fs.existsSync('/work/'+name)){if(fs.existsSync('/work/abort')||Date.now()>end)throw Error('Stage timeout: '+name);await pause(250);} }
async function api(path, body, base=c.origin) {
  const r=await fetch(base+path,{method:body===undefined?'GET':'POST',headers:{'Content-Type':'application/json','New-Api-User':'2',...(cookie?{Cookie:cookie}:{})},body:body===undefined?undefined:JSON.stringify(body)});
  const data=await r.json(); assert.equal(data.success,true,JSON.stringify({path,status:r.status,data}));return data;
}
async function snapshot() {
  const [r]=await db.query("SELECT `key`,`value` FROM options ORDER BY `key`");
  return JSON.stringify(r);
}
async function schema() {
  const [r]=await db.query("SELECT TABLE_NAME,COLUMN_NAME,COLUMN_TYPE,IS_NULLABLE,COLUMN_DEFAULT FROM information_schema.COLUMNS WHERE TABLE_SCHEMA='drill' ORDER BY TABLE_NAME,ORDINAL_POSITION");
  return JSON.stringify(r);
}
async function verifyArchivedAssets() {
  const manifests=JSON.parse(fs.readFileSync('/work/asset-manifests.json','utf8'));
  const assets=Object.assign({},...manifests.map(m=>m.assets));
  let compressed=0;
  for(const [name,hash] of Object.entries(assets)) {
    const response=await fetch(c.origin+'/'+name,{headers:{Connection:'close','Accept-Encoding':'gzip'}});
    assert.equal(response.status,200,name);
    assert.match(response.headers.get('cache-control'),/immutable/,name);
    if(name.endsWith('.js'))assert.match(response.headers.get('content-type'),/javascript/,name);
    if(name.endsWith('.css'))assert.match(response.headers.get('content-type'),/text\/css/,name);
    const body=Buffer.from(await response.arrayBuffer());
    assert.equal(createHash('sha256').update(body).digest('hex'),hash,name);
    if(/\.(js|css)$/.test(name)&&body.length>=1024) {
      assert.equal(response.headers.get('content-encoding'),'gzip',name);
      assert.match(response.headers.get('vary'),/Accept-Encoding/i,name);
      compressed++;
    }
  }
  assert(compressed>0);
  return {files:Object.keys(assets).length,compressed,images:manifests.map(m=>m.image),allContentHashesMatch:true};
}
async function context() {
  const ctx=await browser.newContext({viewport:{width:1440,height:1000},locale:'en',acceptDownloads:true});
  const pair=cookie.split('='); await ctx.addCookies([{name:pair.shift(),value:pair.join('='),url:c.origin}]);
  await ctx.addInitScript(user=>{localStorage.setItem('user',JSON.stringify(user));localStorage.setItem('uid',String(user.id));localStorage.setItem('i18nextLng','en')},(await api('/api/user/self')).data);
  await ctx.route('**/*',route=>route.request().url().startsWith(c.origin+'/')?route.continue():route.abort());
  return ctx;
}
(async()=>{
  db=await mysql.createConnection({...c.mysql,charset:'utf8mb4'});
  const login=await fetch(c.origin+'/api/user/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:'local_drill',password:c.password})});
  assert.equal((await login.json()).success,true); cookie=login.headers.get('set-cookie').split(';')[0];
  assert('0_Tools_Media' in (await api('/api/user/self/groups')).data);
  for(let i=1;i<=11;i++) {
    const id=90000+i;
    await db.execute('INSERT INTO tickets (id,user_id,username,subject,type,status,priority,created_time,updated_time) VALUES (?,2,?,?,?,?,2,1790000000,1790000000)',[id,'local_drill','示例大学发票申请 '+i,'invoice',1]);
    await db.execute('INSERT INTO top_ups (id,user_id,money,trade_no,status) VALUES (?,2,123.45,?,?)',[id,'bluegreen-order-'+i,'success']);
    await db.execute('INSERT INTO ticket_invoices (ticket_id,user_id,company_name,tax_number,bank_account,email,top_up_order_ids,total_money,invoice_status,created_time) VALUES (?,2,?,?,?,?,?,123.45,1,1790000000)',[id,'示例大学','001234567890123456','001234567890123456789','invoice@example.test',JSON.stringify([id])]);
    await db.execute('INSERT INTO ticket_messages (ticket_id,user_id,username,role,content,created_time) VALUES (?,2,?,100,?,1790000000)',[id,'local_drill','申请备注：拆票要求']);
  }
  const invoiceBefore=JSON.stringify((await db.query('SELECT * FROM ticket_invoices ORDER BY id'))[0]);
  const prices=await snapshot(), beforeSchema=await schema();
  await api('/api/channel/',{mode:'single',channel:{type:1,name:'bluegreen-mock',key:'local-only',base_url:c.upstream,models:'qwen3.5-omni-flash',group:'0_Tools_Media',status:1,priority:99999}});
  await api('/api/token/',{name:'bluegreen-drill',remain_quota:1000000,expired_time:-1,group:'0_Tools_Media'});
  // Web instances refresh the shared channel cache once per configured second.
  await pause(2500);
  const [[token]]=await db.query('SELECT `key`,id FROM tokens WHERE user_id=2 ORDER BY id DESC LIMIT 1');
  const [[before]]=await db.query('SELECT quota,used_quota FROM users WHERE id=2');
  stream=await fetch(c.origin+'/v1/chat/completions',{method:'POST',headers:{'Content-Type':'application/json',Authorization:'Bearer sk-'+token.key},body:JSON.stringify({model:'qwen3.5-omni-flash',messages:[{role:'user',content:'hello'}],stream:true,stream_options:{include_usage:true}})});
  if(stream.status!==200)throw Error('Old stream setup: '+stream.status+' '+await stream.text());assert.equal(stream.headers.get('x-new-api-instance'),c.blue);
  const reader=streamReader=stream.body.getReader();let text=new TextDecoder().decode((await reader.read()).value);assert(text.includes('hello'));
  browser=await chromium.launch({headless:true});
  const oldContext=await context(), oldPage=await oldContext.newPage();
  const oldScripts=[]; oldPage.on('request',r=>{if(r.resourceType()==='script')oldScripts.push(r.url())});
  await oldPage.goto(c.origin+'/pricing');
  await oldPage.getByPlaceholder(/Search/i).first().waitFor();
  await oldPage.getByPlaceholder(/Search/i).first().fill('unsaved-bluegreen-draft');
  const oldErrors=[];oldPage.on('pageerror',e=>oldErrors.push(e.message));
  const health=(async()=>{while(polling){try{const r=await fetch(c.origin+'/api/status',{headers:{Connection:'close'},signal:AbortSignal.timeout(4000)});probes.push({status:r.status,instance:r.headers.get('x-new-api-instance')});}catch(e){probes.push({error:e.message});}await pause(500);}})();
  mark('ready');await wait('switched');
  await api('/api/user/self');await api('/api/user/self/groups');
  assert.equal((await api('/api/ticket/admin/?type=invoice')).data.total,11);
  const oldList=await api('/api/ticket/admin/?type=invoice',undefined,'http://'+c.blue+':3000');assert.equal(oldList.data.total,11);
  report.originalCookieAcceptedByBoth=true;
  assert.equal(await oldPage.getByPlaceholder(/Search/i).first().inputValue(),'unsaved-bluegreen-draft');
  report.oldPageDraftPreserved=true;
  const assetResults=[];for(const url of [...new Set(oldScripts)]){const r=await fetch(url,{headers:{Connection:'close'}});assetResults.push({path:new URL(url).pathname,status:r.status});}
  report.oldLoadedAssetsOnNewWeb=assetResults;
  // An actual SPA navigation exercises the old runtime's not-yet-loaded route.
  // The first search field is the shell navigation filter; clear the preserved
  // draft before selecting a route, otherwise every sidebar link is filtered out.
  await oldPage.getByPlaceholder(/Search/i).first().fill('');
  const failed=[];oldPage.on('response',r=>{if(r.status()>=400&&r.url().includes('/static/'))failed.push({path:new URL(r.url()).pathname,status:r.status()});});
  const link=oldPage.locator('a[href="/tickets"]').first();
  if(await link.count()){const destination=await link.getAttribute('href');await link.click();await pause(5000);report.oldTabNavigation={destination,url:oldPage.url(),failedResources:failed,errors:oldErrors,body:(await oldPage.locator('body').innerText()).slice(-2000)};}else{throw Error('Old-page navigation link absent');}
  await oldPage.screenshot({path:output+'/old-tab-after-switch.png',fullPage:true});
  await oldPage.waitForFunction(()=>document.body.innerText.includes('90011'));
  assert.deepEqual(failed,[]);assert.deepEqual(oldErrors,[]);
  assert(assetResults.every(x=>x.status===200));
  report.oldTabNavigationWithoutReload=true;
  // Close old keepalive connections before using a new context against the active slot.
  await oldContext.close();
  const freshContext=await context(), page=await freshContext.newPage();page.setDefaultTimeout(20000);
  page.on('pageerror',e=>errors.push(e.message));
  const pageResponse=await page.goto(c.origin+'/admin-tickets');assert.equal(pageResponse.headers()['x-new-api-instance'],c.green);
  const search=page.getByLabel('Search invoice title',{exact:true});await search.fill('大学');
  await page.locator('form').filter({has:search}).getByRole('button',{name:'Search',exact:true}).click();
  await page.getByRole('row').filter({hasText:'示例大学发票申请 11'}).getByRole('checkbox').check();
  const nextPage=page.waitForResponse(r=>r.url().includes('/api/ticket/admin')&&new URL(r.url()).searchParams.get('p')==='2');
  await page.getByRole('button',{name:'Next',exact:true}).click();await nextPage;
  await page.getByText('Page 2 / 2',{exact:true}).waitFor();
  await page.getByRole('row').filter({has:page.getByText('90001',{exact:true})}).getByRole('checkbox').check();
  const downloadPromise=page.waitForEvent('download');await page.getByRole('button',{name:'Export selected invoices',exact:true}).click();
  const download=await downloadPromise;await download.saveAs(output+'/invoices.xlsx');
  const bytes=fs.readFileSync(output+'/invoices.xlsx');assert.equal(bytes.subarray(0,2).toString(),'PK');
  assert.equal(JSON.stringify((await db.query('SELECT * FROM ticket_invoices ORDER BY id'))[0]),invoiceBefore);
  assert.deepEqual(errors,[]);await page.screenshot({path:output+'/new-invoice-export.png',fullPage:true});
  report.freshPageInvoiceSearchAndCrossPageExport=true;report.invoiceStateUnchanged=true;
  await freshContext.close();
  await fetch(c.upstream+'/release');while(true){const part=await reader.read();if(part.done)break;text+=new TextDecoder().decode(part.value);}assert(text.includes('[DONE]'));
  for(let i=0;i<50;i++){const [[r]]=await db.query('SELECT COUNT(*) n FROM logs WHERE type=2 AND user_id=2');if(r.n===1)break;await pause(200);}
  const [[after]]=await db.query('SELECT quota,used_quota FROM users WHERE id=2');
  const [[logs]]=await db.query('SELECT COUNT(*) n,SUM(quota) quota FROM logs WHERE type=2 AND user_id=2');
  const [[tok]]=await db.query('SELECT used_quota FROM tokens WHERE id=?',[token.id]);
  assert.equal(logs.n,1);assert.equal(before.quota-after.quota,Number(logs.quota));assert.equal(tok.used_quota,Number(logs.quota));assert(Number(logs.quota)>0);
  report.originalSSE={completed:true,charges:logs.n,quota:Number(logs.quota),walletTokenLogAgree:true};
  assert.equal(await snapshot(),prices);assert.equal(await schema(),beforeSchema);
  mark('validated');await wait('drained');await api('/api/user/self');
  report.assetsAfterOldWebStopped=await verifyArchivedAssets();
  const missing=await fetch(c.origin+'/static/js/missing.1234567890.js');
  assert.equal(missing.status,404);assert.match(missing.headers.get('cache-control'),/no-store/);
  // Load a candidate page before rollback, then request a fresh lazy route after
  // the candidate slot has stopped. Browser route loading must remain usable.
  const rollbackContext=await context(), rollbackPage=await rollbackContext.newPage();
  const rollbackErrors=[];rollbackPage.on('pageerror',e=>rollbackErrors.push(e.message));
  await rollbackPage.goto(c.origin+'/pricing');
  await rollbackPage.getByPlaceholder(/Search/i).first().waitFor();
  report.schemaAndPricesUnchanged=true;
  mark('ready-rollback');await wait('rolled-back');await api('/api/user/self');
  report.rollbackCookieAccepted=true;
  report.assetsAfterCandidateStopped=await verifyArchivedAssets();
  await rollbackPage.locator('a[href="/tickets"]').first().click();
  await rollbackPage.waitForFunction(()=>document.body.innerText.includes('90011'));
  assert.deepEqual(rollbackErrors,[]);
  report.newTabNavigationAfterRollback=true;
  await rollbackPage.screenshot({path:output+'/new-tab-after-rollback.png',fullPage:true});
  await rollbackContext.close();
  if(c.log_bridge) {
    mark('ready-log-migration'); await wait('log-migration-ready');
    await checkLogMigration();
  }
  polling=false;await health;
  assert(probes.length>10);assert(probes.every(p=>p.status===200));assert(probes.some(p=>p.instance===c.blue)&&probes.some(p=>p.instance===c.green));
  report.health={requests:probes.length,failures:0,instances:[...new Set(probes.map(p=>p.instance))]};
  report.backendBlueGreen='passed';
  report.result='passed';
  fs.writeFileSync(output+'/report.json',JSON.stringify(report,null,2));console.log(report.result);
})().catch(async error=>{report.result='failed';report.error=error.stack;if(browser){for(const ctx of browser.contexts()){for(const p of ctx.pages()){report.failurePage={url:p.url(),body:(await p.locator('body').innerText()).slice(-3500)};await p.screenshot({path:output+'/failure.png'}).catch(()=>{});}}}fs.writeFileSync(output+'/report.json',JSON.stringify(report,null,2));console.error(error);process.exitCode=1;}).finally(async()=>{polling=false;await fetch(c.upstream+'/release').catch(()=>{});if(streamReader)await streamReader.cancel().catch(()=>{});if(browser)await browser.close();if(db)await db.end();});

async function checkLogMigration() {
  const {spawnSync}=require('node:child_process');
  const actorPassword=c.password.slice(0,16);
  const group=(await api('/api/user/self')).data.group;
  const actors={};
  for(const role of [1,10]) {
    const username='log_bridge_'+role;
    await api('/api/user/',{username,password:actorPassword,role,group});
    const [[actor]]=await db.query('SELECT id,username FROM users WHERE username=?',[username]);
    const login=await fetch(c.origin+'/api/user/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username,password:actorPassword})});
    assert.equal((await login.json()).success,true);
    actors[role]={...actor,cookie:login.headers.get('set-cookie').split(';')[0]};
  }
  const user=actors[1], admin=actors[10];
  async function as(actor,path) {
    const response=await fetch(c.origin+path,{headers:{'New-Api-User':String(actor.id),Cookie:actor.cookie}});
    assert.equal(response.status,200);
    const json=await response.json(); assert.equal(json.success,true); return json.data;
  }
  const now=Math.floor(Date.now()/1000), name='__log_bridge_history__';
  const metadata='{"channel_id":77,"channel_name":"private-channel","channel_type":17,"reject_reason":"log-bridge-private-reason","root_info":{"private":1},"audit_info":{"actor":2},"reasoning_effort":"high","large":9007199254740993}';
  await db.execute('INSERT INTO logs(user_id,username,created_at,type,model_name,quota,prompt_tokens,completion_tokens,channel_id,other) VALUES (?,?,?,2,?,125,8,2,77,?)',[user.id,user.username,now,name,metadata]);
  const suffix='?model_name='+name+'&start_timestamp='+(now-60)+'&end_timestamp='+(now+3600);
  async function assertProjection() {
    const root=(await api('/api/log/'+suffix)).data.items[0];
    const administrator=(await as(admin,'/api/log/'+suffix)).items[0];
    const ordinary=(await as(user,'/api/log/self/'+suffix)).items[0];
    const ro=JSON.parse(root.other),ao=JSON.parse(administrator.other),uo=JSON.parse(ordinary.other);
    assert(ro.root_info); assert(!ao.root_info); assert(ao.audit_info);
    assert.equal(ro.reject_reason,'log-bridge-private-reason');
    assert.equal(ao.reject_reason,ao.admin_info.reject_reason);
    assert(root.other.includes('9007199254740993'));
    assert.equal(ordinary.channel,0); assert.equal(uo.reasoning_effort,'high');
    for(const key of ['channel_id','channel_name','channel_type','reject_reason','admin_info','root_info','audit_info'])assert(!(key in uo),key);
  }
  await assertProjection();
  const ctx=await context(), page=await ctx.newPage();
  // The bridge carries the exact old frontend. Load it before the database changes.
  await page.goto(c.origin+'/usage-logs/common');
  await page.getByText(name,{exact:true}).first().waitFor();
  const columns='id,user_id,created_at,type,content,username,token_name,model_name,quota,prompt_tokens,completion_tokens,use_time,is_stream,channel_id,token_id,`group`,ip,request_id';
  const before=JSON.stringify((await db.query('SELECT '+columns+' FROM logs ORDER BY id'))[0]);
  const env={...process.env,LOG_MIGRATION_DSN:c.mysql.user+':'+c.mysql.password+'@tcp('+c.mysql.host+':3306)/'+c.mysql.database+'?charset=utf8mb4'};
  function migrate(args) {
    const result=spawnSync('/work/log-migrate',args,{env,encoding:'utf8',timeout:60000});
    if(result.status!==0)throw Error('log-migrate '+args[0]+' failed: '+result.stderr);
    return JSON.parse(result.stdout.trim());
  }
  migrate(['plan','--state','/work/output/log-online.json']);
  const refs=['--compatibility-ref',c.candidate_image_id,'--backup-ref','isolated-before-bluegreen.sql'];
  migrate(['run','--state','/work/output/log-online.json','--batch','1','--delay','0s','--max-batches','1',...refs]);
  migrate(['run','--state','/work/output/log-online.json','--delay','0s',...refs]);
  const verified=migrate(['verify','--state','/work/output/log-online.json','--delay','0s','--report','/work/output/log-verified.json']);
  assert(verified.clean_observation); assert.equal(verified.invalid,0); assert.equal(verified.pending,0);
  assert.equal(JSON.stringify((await db.query('SELECT '+columns+' FROM logs ORDER BY id'))[0]),before);
  const [[stored]]=await db.query('SELECT other FROM logs WHERE model_name=?',[name]);
  assert(!('reject_reason' in JSON.parse(stored.other)));
  assert(stored.other.includes('9007199254740993'));
  await assertProjection();
  // Requery without reloading this old frontend document, then open details.
  await page.getByRole('main').getByRole('button',{name:'Search',exact:true}).click();
  const row=page.getByRole('row').filter({hasText:name}).first();
  await row.getByTitle('Click to view full details',{exact:true}).click();
  await page.getByText('log-bridge-private-reason',{exact:true}).waitFor();
  await page.screenshot({path:output+'/old-page-migrated-log.png',fullPage:true});
  await ctx.close();
  report.log_migration={passed:true,oldPageRejectReason:true,roleProjection:true,nonMetadataColumnsUnchanged:true,resume:true,largeIntegerPreserved:true,verification:verified};
}
