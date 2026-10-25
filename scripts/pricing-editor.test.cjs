// Run through scripts/docker-check.sh pricing-editor-test (no application server).
const assert = require("node:assert/strict");
const fs = require("node:fs");
const http = require("node:http");
const { chromium } = require("playwright");

assert(fs.existsSync("/.dockerenv"), "Run this browser check inside Docker");
const bundle = fs.readFileSync("/src/.pricing-editor-test/pricing-editor.js");
const server = http.createServer((request, response) => {
  response.setHeader(
    "Content-Type",
    request.url === "/fixture.js" ? "text/javascript" : "text/html",
  );
  response.end(
    request.url === "/fixture.js"
      ? bundle
      : `<!doctype html><html><head>
    <style>svg{width:16px;height:16px}button,input,textarea{margin:4px} [role=dialog]{background:white;position:relative} [role=listbox]{background:white} </style>
    </head><body><div id="root"></div><script type="module" src="/fixture.js"></script></body></html>`,
  );
});
const daytime =
  '(hour("Asia/Shanghai") >= 9 || hour("Asia/Shanghai") < 12 ? 2 : 1)';
const overnight = '(hour("UTC") >= 21 && hour("UTC") < 6 ? 3 : 1)';
const canonical = '(hour("UTC") >= 9 && hour("UTC") < 12 ? 2 : 1)';
const opaque =
  '(param("custom") == "yes" || header("X-Price") == "on" ? 2 : 1)';
let browser;
let cases = 0;

(async () => {
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const origin = `http://127.0.0.1:${server.address().port}`;
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({
    viewport: { width: 1400, height: 1200 },
  });
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.setDefaultTimeout(5000);
  async function load(rule, dialog = false, amount = false) {
    await page.goto(
      `${origin}/?${new URLSearchParams({ rule, ...(dialog ? { dialog: "1" } : {}), ...(amount ? { amount: "1" } : {}) })}`,
    );
    await page.getByRole("combobox").first().waitFor();
  }
  async function mode(name) {
    await page.getByRole("combobox").first().click();
    await page.getByRole("option", { name, exact: true }).click();
  }
  async function state() {
    return page.getByTestId("rule").textContent();
  }
  async function save() {
    await page
      .getByRole("button", { name: "Save fixture", exact: true })
      .click();
    return JSON.parse(await page.getByTestId("saved").textContent());
  }
  // Plugin task prices are edited in the model price dialog section.
  await page.goto(`${origin}/?task=1&usage=ready&model=alias`);
  await page.getByText('Set separately', {exact:true}).waitFor();
  assert.equal(await page.getByRole('button', {name:'Save',exact:true}).isEnabled(), false);
  assert((await page.getByText('Mapped models', {exact:false}).textContent()).includes('happyhorse-1.0-t2v'));
  await page.getByLabel('Plugin expression', {exact:true}).fill('v2:u("seconds") * 0.02');
  await page.getByRole('button', {name:'Save',exact:true}).click();
  assert.deepEqual(JSON.parse(await page.getByTestId('saved').textContent()), {name:'alias',plugin:'happyhorse',expression:'v2:u("seconds") * 0.02'});
  cases++;
  await page.goto(`${origin}/?task=1&usage=ready&model=follower`);
  await page.getByText('Follows model price', {exact:true}).waitFor();
  await page.getByText('Same as the model price above; price changes take effect immediately.', {exact:true}).waitFor();
  assert.equal(await page.getByLabel('Plugin expression', {exact:true}).count(), 0);
  await page.getByRole('button', {name:'Set plugin price separately',exact:true}).click();
  assert.equal(await page.getByLabel('Plugin expression', {exact:true}).inputValue(), '');
  assert.equal(await page.getByRole('button', {name:'Restore following model price',exact:true}).isEnabled(), false);
  await page.getByLabel('Plugin expression', {exact:true}).fill('v2:0');
  await page.getByRole('button', {name:'Save',exact:true}).click();
  assert.deepEqual(JSON.parse(await page.getByTestId('saved').textContent()), {name:'follower',plugin:'happyhorse',expression:'v2:0'});
  await page.getByText('Set separately', {exact:true}).waitFor();
  cases++;
  await page.goto(`${origin}/?task=1&usage=ready&model=follower&locked=1`);
  await page.getByText('fixture locked', {exact:true}).waitFor();
  assert.equal(await page.getByRole('button', {name:'Set plugin price separately',exact:true}).isEnabled(), false);
  cases++;
  await page.goto(`${origin}/?task=1&usage=ready&model=blocked`);
  await page.getByText('Price required', {exact:true}).waitFor();
  cases++;
  await page.goto(`${origin}/?task=1&usage=ready&model=orphan`);
  assert.equal(await page.getByLabel('Plugin expression', {exact:true}).inputValue(), 'v2:0');
  await page.getByRole('button', {name:'Restore following model price',exact:true}).click();
  assert.equal(await page.getByTestId('saved').textContent(), '');
  await page.getByRole('alertdialog').getByRole('button', {name:'Restore',exact:true}).click();
  assert.deepEqual(JSON.parse(await page.getByTestId('saved').textContent()), {name:'orphan',plugin:'happyhorse',expression:null});
  cases++;
  // The overview is read-only; editing jumps to the model price dialog.
  await page.goto(`${origin}/?task=1&usage=ready&overview=1`);
  await page.getByText('Plugin tasks that need a price: 1', {exact:true}).waitFor();
  assert.equal(await page.locator('textarea, input[type=number]').count(), 0);
  assert.equal(await page.getByRole('button', {name:'Save',exact:true}).count(), 0);
  assert((await page.getByTestId('task-row-happyhorse-blocked').textContent()).includes('Price required'));
  assert((await page.getByTestId('task-row-happyhorse-follower').textContent()).includes('Follows model price'));
  assert((await page.getByTestId('task-row-happyhorse-orphan').textContent()).includes('v2:0'));
  await page.getByRole('button', {name:'Edit follower',exact:true}).click();
  assert.equal(await page.getByTestId('saved').textContent(), 'follower');
  cases++;
  await page.goto(`${origin}/?task=1&usage=ready&fail=1`);
  await page.getByLabel('Plugin expression', {exact:true}).fill('v2:0.9');
  await page.getByRole('button', {name:'Save',exact:true}).click();
  assert.equal(await page.getByLabel('Plugin expression', {exact:true}).inputValue(), 'v2:0.9');
  cases++;
  for (const kind of ['unit','token']) {
    await page.goto(`${origin}/?task=1&usage=ready&${kind}=1`);
    const label=kind==='unit'?'Unit price (USD)':'USD per million completion tokens';
    const input=page.getByLabel(label,{exact:true});
    await input.focus(); await input.press('Tab');
    assert.equal(await page.getByRole('button',{name:'Save',exact:true}).isEnabled(),false);
    assert.equal(await page.getByTestId('saved').textContent(),'');
    await input.fill('0');
    await page.getByRole('button',{name:'Save',exact:true}).click();
    const rule=JSON.parse(await page.getByTestId('saved').textContent()).expression;
    assert.equal(rule[kind==='unit'?'unit_price':'model_ratio'],0);
    assert.equal(rule.dimensions,'provider_v1');
    assert.equal(rule.settlement,'submit');
    cases++;
  }
  await page.getByRole('button',{name:'Change to total amount',exact:true}).click();
  await page.getByText('Switching to a total amount changes rounding and completion billing. Enter the intended new price.',{exact:true}).waitFor();
  await page.getByLabel('Plugin expression',{exact:true}).fill('v2:0.02');
  await page.getByRole('button',{name:'Save',exact:true}).click();
  assert.equal(JSON.parse(await page.getByTestId('saved').textContent()).expression,'v2:0.02');
  cases++;
  for (const rule of [
    daytime,
    overnight,
    canonical,
    `${daytime} * ${overnight}`,
    opaque,
  ]) {
    await load(rule);
    if (await page.getByPlaceholder("start", { exact: true }).count()) {
      await page.getByPlaceholder("start", { exact: true }).first().focus();
      await page
        .getByPlaceholder("start", { exact: true })
        .first()
        .press("Tab");
    }
    await mode("Expression editor");
    assert(
      (await page.locator("textarea").first().inputValue()).includes(rule),
    );
    await mode("Visual editor");
    await mode("Expression editor");
    await mode("Visual editor");
    assert.equal(await state(), rule);
    assert.equal(
      await page.getByTestId("calls").textContent(),
      "[]",
      "view changes must not emit pricing edits",
    );
    assert.equal((await save()).rule, rule);
    cases++;
  }
  // Edit one condition; another legacy range in the same group stays intact.
  const mixed =
    '((hour("Asia/Shanghai") >= 9 || hour("Asia/Shanghai") < 12) && hour("UTC") >= 21 && hour("UTC") < 6 ? 2 : 1)';
  await load(mixed);
  await page.getByPlaceholder("start", { exact: true }).first().fill("10");
  await page.getByPlaceholder("start", { exact: true }).first().press("Tab");
  const edited =
    '(hour("Asia/Shanghai") >= 10 && hour("Asia/Shanghai") < 12 && hour("UTC") >= 21 && hour("UTC") < 6 ? 2 : 1)';
  assert.equal(await state(), edited);
  await mode("Expression editor");
  await mode("Visual editor");
  assert.equal((await save()).rule, edited);
  cases++;

  // Multiplier edits and adding/removing another group do not opt into fixing time bounds.
  await load(`${daytime} * ${overnight}`);
  await page.getByPlaceholder("1.0", { exact: true }).first().fill("4");
  await page.getByPlaceholder("1.0", { exact: true }).first().press("Tab");
  const multiplied = `${daytime.replace("? 2 :", "? 4 :")} * ${overnight}`;
  assert.equal(await state(), multiplied);
  await page
    .getByRole("button", { name: "Add time rule group", exact: true })
    .click();
  const newGroup = page
    .getByRole("button", { name: "Remove rule group", exact: true })
    .last()
    .locator("../..");
  await newGroup.getByRole("combobox").last().click();
  await page.getByRole("option", { name: "Time range", exact: true }).click();
  await newGroup.getByPlaceholder("start", { exact: true }).fill("20");
  await newGroup.getByPlaceholder("end", { exact: true }).fill("5");
  await newGroup.getByPlaceholder("1.0", { exact: true }).fill("2");
  await newGroup.getByPlaceholder("1.0", { exact: true }).press("Tab");
  assert((await state()).startsWith(multiplied + " * "), await state());
  assert((await state()).includes(">= 20 ||"));
  await page
    .getByRole("button", { name: "Remove rule group", exact: true })
    .last()
    .click();
  assert.equal((await save()).rule, multiplied);
  cases++;

  // Same-model external updates remain authoritative, including after a local edit.
  await page
    .getByRole("button", { name: "External refresh", exact: true })
    .click();
  await mode("Expression editor");
  await mode("Visual editor");
  assert.equal(
    (await save()).rule,
    '(hour("UTC") >= 22 && hour("UTC") < 5 ? 4 : 1)',
  );
  assert.equal((await save()).billing, 'tier("base", p * 7 + c * 8)');
  cases++;

  // Explicit raw editing still publishes both parts; switching back preserves that edit.
  await load(daytime);
  await mode("Expression editor");
  await page
    .locator("textarea")
    .first()
    .fill(`(tier("base", p * 5 + c * 6)) * ${overnight}`);
  await mode("Visual editor");
  assert.equal((await save()).rule, overnight);
  assert.equal((await save()).billing, 'tier("base", p * 5 + c * 6)');
  cases++;

  await load(daytime);
  const inputPrice = page
    .getByText("Input price", { exact: true })
    .locator("..")
    .getByRole("spinbutton");
  await inputPrice.fill("7");
  await inputPrice.press("Tab");
  assert.equal((await save()).rule, daytime);
  assert((await save()).billing.includes("p * 7"));
  cases++;
  await page.getByRole("button", { name: "Flat", exact: true }).click();
  assert.equal((await save()).billing, 'v2:(tier("base", p * 2 + c * 4)\n) / 1000000');
  assert.equal((await save()).rule, "");
  cases++;

  // Production parent: no-op save must not dispatch an accidental pricing update.
  await load(daytime, true);
  await mode("Expression editor");
  await mode("Visual editor");
  await page.getByRole("button", { name: "Update", exact: true }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  assert.equal(await page.getByTestId("saved").textContent(), "");
  cases++;

  // Production parent: an explicit edit saves only the intended change.
  await load(`${daytime} * ${overnight}`, true);
  await page.getByPlaceholder("start", { exact: true }).first().fill("10");
  await page.getByPlaceholder("start", { exact: true }).first().press("Tab");
  await page.getByRole("button", { name: "Update", exact: true }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  const saved = JSON.parse(await page.getByTestId("saved").textContent());
  assert.equal(
    saved.requestRuleExpr,
    `${daytime.replace(">= 9 ||", ">= 10 &&")} * ${overnight}`,
  );
  cases++;
  // Migrated expressions keep exact text on no-op save and publish amounts on edit.
  await load(daytime, false, true);
  await mode("Expression editor");
  await mode("Visual editor");
  assert.equal((await save()).billing, 'v2:(tier("base", p * 2 + c * 3)\n) / 1000000');
  assert.equal((await save()).rule, daytime);
  cases++;
  await page.getByText("Input price", { exact: true }).locator("..").getByRole("spinbutton").fill("7");
  await page.getByText("Input price", { exact: true }).locator("..").getByRole("spinbutton").press("Tab");
  assert.equal((await save()).billing, 'v2:(tier("base", p * 7 + c * 3)\n) / 1000000');
  assert.equal((await save()).rule, daytime);
  cases++;
  await load(daytime, true, true);
  await mode("Expression editor");
  await mode("Visual editor");
  await page.getByRole("button", { name: "Update", exact: true }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  assert.equal(await page.getByTestId("saved").textContent(), "");
  cases++;
  // Usage facts come from the binding schema, never from a token estimate.
  for (const status of ["ready", "conflict", "missing"]) {
    await page.goto(`${origin}/?usage=${status}`);
    const message = status === "ready" ? "Plugin usage fields" : status === "conflict"
      ? "Enabled channels have conflicting usage schemas. Resolve the bindings before saving u() rules."
      : "No enabled plugin usage schema is available for this model. New u() rules cannot be saved.";
    await page.getByText(message, { exact: true }).waitFor();
    assert.equal(await page.getByText("Token estimator", { exact: true }).count(), 0);
    if (status === "ready") {
      await page.getByText('u("seconds")', { exact: true }).waitFor();
      await page.getByText("number / second", { exact: true }).waitFor();
      await page.getByText("720p | 1080p", { exact: true }).waitFor();
    }
    assert.equal((await save()).billing, 'v2:u("seconds") * 0.01');
    cases++;
  }
  await page.goto(`${origin}/?usage=ready&dialog=1`);
  await page.getByText("Plugin usage fields", { exact: true }).waitFor();
  await page.getByRole("button", { name: "Update", exact: true }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  assert.equal(await page.getByTestId("saved").textContent(), "");
  cases++;
  await page.goto(`${origin}/?usage=ready&dialog=1`);
  await page.getByText("Plugin usage fields", { exact: true }).waitFor();
  await page.locator("textarea").first().fill('v2:u("seconds") * 0.02');
  await page.getByRole("button", { name: "Update", exact: true }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  assert.equal(JSON.parse(await page.getByTestId("saved").textContent()).billingExpr, 'v2:u("seconds") * 0.02');
  cases++;
  // Plugin price buttons sit outside the model form: saving one must neither
  // submit the model price nor close the dialog.
  await page.goto(`${origin}/?usage=ready&dialog=1&taskdialog=1`);
  await page.getByText("Task plugins", { exact: true }).waitFor();
  await page.getByRole("button", { name: "Set plugin price separately", exact: true }).click();
  await page.getByLabel("Plugin expression", { exact: true }).fill("v2:0.5");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  assert.deepEqual(JSON.parse(await page.getByTestId("task-saved").textContent()), { plugin: "happyhorse", expression: "v2:0.5" });
  assert.equal(await page.getByTestId("closed").textContent(), "false");
  assert.equal(await page.getByTestId("saved").textContent(), "");
  cases++;
  for (const status of ['blocked','ready','stale']) {
    await page.goto(`${origin}/?migration=${status}`);
    await page.getByRole('button',{name:'Preview task price migration',exact:true}).click();
    if(status==='blocked') {
      await page.getByText('Task price migration needs review',{exact:true}).waitFor();
      assert.equal(await page.getByRole('button',{name:'Confirm task price migration',exact:true}).count(),0);
    } else {
      assert((await page.locator('li[data-status=converted_to_follow]').textContent()).includes('Identical plugin price removed; follows the model price again'));
      const differs=page.locator('li[data-status=override_differs_from_model]');
      assert((await differs.textContent()).includes('Plugin price kept because it differs from the model price'));
      assert((await differs.getAttribute('class')).includes('text-amber'));
      await page.getByRole('button',{name:'Confirm task price migration',exact:true}).click();
      await page.getByText(status==='stale'?'fixture preview stale':'Task price migration is complete',{exact:true}).waitFor();
      assert.deepEqual(JSON.parse(await page.evaluate(()=>document.documentElement.dataset.migrationRequest)),{source_hash:'a'.repeat(64)});
      assert.equal(await page.getByRole('button',{name:'Confirm task price migration',exact:true}).count(),0);
    }
    cases++;
  }
  assert.deepEqual(errors, []);
  console.log(
    `PASS: ${cases} pricing editor browser cases (real React/Radix and parent save)`,
  );
})()
  .catch((error) => {
    console.error(error);
    process.exitCode = 1;
  })
  .finally(async () => {
    await browser?.close();
    server.close();
  });
