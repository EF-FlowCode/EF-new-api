const assert = require('node:assert/strict');
const fs = require('node:fs');
const http = require('node:http');
const { chromium } = require('playwright');
assert(fs.existsSync('/.dockerenv'), 'Run inside Docker');
const bundle = fs.readFileSync('/src/.task-review-test/task-review.js');
const server = http.createServer((req,res)=>{
 res.setHeader('Content-Type',req.url==='/fixture.js'?'text/javascript':'text/html');
 res.end(req.url==='/fixture.js'?bundle:'<html><body><div id="root"></div><script type="module" src="/fixture.js"></script></body></html>');
});
let browser;
(async()=>{
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 browser=await chromium.launch({headless:true});
 const page=await browser.newPage();
 const errors=[];page.on('pageerror',error=>errors.push(error.message));
 for(const mode of ['token','ready','invalid','legacy','missing']) {
  await page.goto(`http://127.0.0.1:${server.address().port}/?mode=${mode}`);
  await page.getByRole('button',{name:'Preview reviewed action'}).waitFor();
  assert.equal(await page.getByText('Price basis',{exact:true}).count(),mode==='missing'?1:0);
  if(['token','ready','invalid'].includes(mode)) {assert.equal(await page.getByText('1000',{exact:true}).count(),1);assert.equal(await page.getByText('Model ratio',{exact:true}).count(),1);}
  if(mode==='missing') {
   await page.getByLabel('Recorded unit quota',{exact:true}).fill('250');
   await page.getByLabel('Verified resolution multiplier',{exact:true}).fill('1');
  }
  await page.getByLabel('Reason',{exact:true}).fill('verified result');
  await page.getByLabel('Evidence',{exact:true}).fill('submission record and provider usage');
  await page.getByRole('checkbox').check();
  await page.getByRole('button',{name:'Preview reviewed action'}).click();
  await page.waitForFunction(()=>document.getElementById('payload').textContent!=='');
  const payload=JSON.parse(await page.locator('#payload').textContent());
  assert.equal(payload.action,'settle');
  assert.equal(Object.hasOwn(payload,'billing'),mode==='missing');
  assert.equal(Object.hasOwn(payload,'seconds'),false);
  if(mode==='invalid') await page.getByText('Review failed. Refresh the task before trying again.',{exact:true}).waitFor();
  else await page.getByRole('button',{name:'Confirm reviewed action'}).waitFor();
 }
 assert.deepEqual(errors,[]);
 console.log('PASS: 5 task review browser cases (real form, preserved snapshots and legacy input)');
})().catch(error=>{console.error(error);process.exitCode=1;}).finally(async()=>{await browser?.close();server.close();});
