const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const mysql = require('mysql2/promise');
const matrix = require('./task-pricing-matrix.cjs');
assert(fs.existsSync('/.dockerenv'), 'Run through Docker release checks');
const root = process.env.RELEASE_DIR;
const state = JSON.parse(fs.readFileSync(path.join(root, 'state.json'), 'utf8'));
const runtime = JSON.parse(fs.readFileSync(path.join(root, 'ui-input.json'), 'utf8'));
assert.equal(runtime.mysql.host, state.validation_target.mysql);
assert.equal(runtime.origin, 'http://' + state.validation_target.prefix + '-gateway:3000');
assert.equal(state.validation_target.owner, state.id);
const proofPath = path.join(root, 'task_pricing_readiness.json');
const savedPath = path.join(root, 'task-pricing-after.json');
const restart = process.env.TASK_PRICING_RESTART === 'true';
const report = restart ? JSON.parse(fs.readFileSync(proofPath)) : {result: 'failed', candidate_image_id: state.candidate_image_id,
  baseline_generation: state.baseline_generation, clone: state.validation_target.prefix,
  scope: 'complete captured routing/prices, atomic task migration and actual wallet/token HTTP matrix on isolated baseline clone'};
const save = () => fs.writeFileSync(proofPath, JSON.stringify(report, null, 2));
const hash = value => crypto.createHash('sha256').update(value).digest('hex');
// The document the migration writes for these reviewed entries.
const applyTaskEntries = (document, entries) => {
  const expected = structuredClone(document);
  for (const e of entries) {
    if (e.status === 'follows_model_price') continue;
    const record = expected.models[e.model] ||= {};
    if (e.status === 'converted_to_follow') {
      if (record.task_expressions) delete record.task_expressions[e.plugin];
      if (record.task_expressions && !Object.keys(record.task_expressions).length) delete record.task_expressions;
      continue;
    }
    record.task_expressions ||= {};
    record.task_expressions[e.plugin] = e.rule;
  }
  expected.task_priority_version = 1;
  return expected;
};
let db;
(async () => {
  db = await mysql.createConnection({...runtime.mysql, charset: 'utf8mb4'});
  const rows = async (query, args = []) => (await db.execute(query, args))[0];
  const [actor] = await rows('SELECT id,access_token FROM users WHERE role=100 AND deleted_at IS NULL ORDER BY id LIMIT 1');
  assert(actor, 'Synthetic baseline Root required');
  // Candidates with scoped access tokens get a temporary Root nap_ token row;
  // older baselines fall back to the legacy users.access_token.
  const [{count: patTable}] = await rows("SELECT COUNT(*) AS count FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='user_access_tokens'");
  const usePAT = Number(patTable) > 0;
  const token = usePAT ? 'nap_' + crypto.randomBytes(22).toString('hex').slice(0, 43) : crypto.randomBytes(16).toString('hex');
  const headers = {Authorization: 'Bearer ' + token, 'New-Api-User': String(actor.id)};
  const financial = () => rows('SELECT `key`,value FROM options ORDER BY `key`');
  const before = await financial();
  const api = async source => {
    const response = await fetch(runtime.origin + '/api/option/task_pricing_migration', {
      method: source === undefined ? 'GET' : 'POST', headers: {...headers, 'Content-Type': 'application/json'},
      ...(source === undefined ? {} : {body: JSON.stringify({source_hash: source})}), signal: AbortSignal.timeout(45000),
    });
    const body = await response.json(); assert.equal(response.status, 200, JSON.stringify(body)); assert.equal(body.success, true, body.message); return body.data;
  };
  let patId = null;
  if (usePAT) {
    const now = Math.floor(Date.now() / 1000);
    const scopes = JSON.parse(fs.readFileSync(path.join(__dirname, 'release-pat-scopes.json'), 'utf8'));
    const result = await rows('INSERT INTO user_access_tokens (user_id,name,token_hash,token_hint,scopes,expires_at,last_used_at,last_used_ip,created_at) VALUES (?,?,?,?,?,?,0,?,?)',
      [actor.id, '__task_pricing_preflight', crypto.createHash('sha256').update(token).digest('hex'), token.slice(-4), JSON.stringify(scopes), now + 7200, '', now]);
    patId = result.insertId;
  } else {
    await rows('UPDATE users SET access_token=? WHERE id=?', [token, actor.id]);
  }
  try {
    const preview = await api();
    assert.equal(typeof preview.source_hash, 'string'); assert(preview.source_hash.length > 0);
    if (restart) {
      assert.equal(report.result, 'pending_restart');
      assert.deepEqual(before, JSON.parse(fs.readFileSync(savedPath)), 'Restart changed migrated options');
      assert.equal(preview.already_applied, true);
      assert.equal(preview.source_hash, report.source_hash);
      assert.equal((await api(preview.source_hash)).already_applied, true);
      assert.deepEqual(await financial(), before);
      report.preview = preview;
      report.restart_idempotent = true;
      report.result = 'passed';
      return;
    }
    report.preview = preview;
    report.blockers = (preview.entries || []).filter(entry => entry.status === 'blocked');
    report.reason_counts = {};
    for (const entry of report.blockers) report.reason_counts[entry.reason] = (report.reason_counts[entry.reason] || 0) + 1;
    assert.deepEqual(await financial(), before, 'Preview changed options');
    report.preview_options_unchanged = true;
    if (preview.blocked) { report.result = 'blocked'; report.reason = 'unresolved_task_pricing_entries'; process.exitCode = 1; return; }
    const document = JSON.parse(before.find(row => row.key === 'ModelPricing').value);
    report.priced_models = Object.keys(document.models).length;
    // Follow entries price live from the model; their rule is the model-derived
    // contract the HTTP matrix checks. Only stored statuses change the document.
    const priced = new Set(['ready', 'follows_model_price', 'converted_to_follow', 'override_differs_from_model']);
    if ((document.task_priority_version || 0) === 1) {
      // Production already ran the one-time migration, so there is no before/after
      // to compare. The recorded entries still prove the candidate's billing while
      // every task model's price record equals the migration result; unrelated
      // models may change freely. An edited task model price needs its own fixture.
      assert.equal(preview.already_applied, true, 'Migrated baseline must report already_applied');
      const provenanceRow = before.find(row => row.key === 'ModelPricingTaskMigrationV1');
      assert(provenanceRow, 'Migrated baseline lacks task migration provenance');
      const provenance = JSON.parse(provenanceRow.value);
      assert.equal(provenance.source_hash, preview.source_hash);
      const recorded = provenance.entries.filter(e => priced.has(e.status) && e.rule);
      const expected = applyTaskEntries(JSON.parse(provenance.original), recorded);
      // The baseline strips production channel IDs from audit entries; re-attach
      // the clone's own fixed bindings, one per recorded entry.
      const bound = await rows('SELECT b.channel_id, b.`key`, c.models FROM task_plugin_production_bindings b JOIN channels c ON c.id=b.channel_id ORDER BY b.channel_id');
      const entries = [];
      for (const key of new Set(recorded.map(e => e.model + '\u0000' + e.plugin))) {
        const group = recorded.filter(e => e.model + '\u0000' + e.plugin === key);
        const channels = bound.filter(b => b.key === group[0].plugin && b.models.split(',').includes(group[0].model));
        assert.equal(channels.length, group.length, 'Clone bindings for ' + group[0].model + ' do not match the recorded migration entries');
        channels.forEach((ch, i) => entries.push({...group[i], channel_id: ch.channel_id}));
      }
      for (const name of new Set(entries.map(e => e.model)))
        assert.deepEqual(document.models[name], expected.models[name],
          'Task model ' + name + ' price changed after migration; recorded rules no longer prove its billing, add a fixture');
      report.task_models_unchanged = [...new Set(entries.map(e => e.model))].sort();
      report.other_models_changed = [...new Set([...Object.keys(document.models), ...Object.keys(expected.models)])]
        .filter(name => JSON.stringify(document.models[name]) !== JSON.stringify(expected.models[name])).sort();
      report.provenance_matches_current = true;
      report.migrated_baseline = true;
      report.after_http = await matrix({rows, origin: runtime.origin, headers, entries, options: before, label: 'migrated'});
      assert.deepEqual(await financial(), before, 'HTTP fixture changed migrated prices/options');
      assert.equal((await api(preview.source_hash)).already_applied, true);
      assert.deepEqual(await financial(), before, 'Repeated migration overwrote prices');
      report.repeat_apply_noop = true;
      fs.writeFileSync(savedPath, JSON.stringify(before));
      report.source_hash = preview.source_hash;
      report.task_priority_version = 1;
      report.result = 'pending_restart';
      return;
    }
    const entries = preview.entries.filter(e => priced.has(e.status) && e.rule);
    report.before_http = await matrix({rows, origin: runtime.origin, headers, entries, options: before, label: 'legacy'});
    assert.deepEqual(await financial(), before, 'HTTP fixture did not restore options');
    assert.equal((await api()).source_hash, preview.source_hash, 'HTTP fixture changed migration inputs');
    await api(preview.source_hash);
    const migrated = await financial();
    const oldRaw = before.find(row => row.key === 'ModelPricing').value;
    const newRaw = migrated.find(row => row.key === 'ModelPricing').value;
    const next = JSON.parse(newRaw), expected = applyTaskEntries(document, entries);
    assert.deepEqual(next, expected, 'Migration made changes outside reviewed task rules');
    assert.deepEqual(migrated.filter(r => !['ModelPricing', 'ModelPricingTaskMigrationV1'].includes(r.key)), before.filter(r => !['ModelPricing', 'ModelPricingTaskMigrationV1'].includes(r.key)));
    const provenance = JSON.parse(migrated.find(row => row.key === 'ModelPricingTaskMigrationV1').value);
    assert.equal(provenance.original, oldRaw); assert.equal(provenance.source_hash, preview.source_hash);
    assert.equal(provenance.target_hash, hash(newRaw)); assert.deepEqual(provenance.entries, preview.entries);
    report.original_sha256 = hash(oldRaw); report.migrated_sha256 = hash(newRaw);
    report.source_hash = preview.source_hash;
    report.after_http = await matrix({rows, origin: runtime.origin, headers, entries, options: migrated, label: 'migrated'});
    assert.deepEqual(report.after_http.cases, report.before_http.cases, 'Actual task wallet/token billing changed');
    assert.deepEqual(await financial(), migrated, 'HTTP fixture changed migrated prices/options');
    assert.equal((await api(preview.source_hash)).already_applied, true);
    assert.deepEqual(await financial(), migrated, 'Repeated migration overwrote prices');
    fs.writeFileSync(savedPath, JSON.stringify(migrated));
    report.task_priority_version = 1;
    report.migration_verified = true;
    report.http_equivalent = true;
    report.result = 'pending_restart';
  } finally {
    if (patId !== null) {
      await rows('DELETE FROM user_access_tokens WHERE id=?', [patId]);
      assert.equal((await rows('SELECT COUNT(*) AS count FROM user_access_tokens WHERE id=?', [patId]))[0].count, 0);
    } else {
      await rows('UPDATE users SET access_token=? WHERE id=?', [actor.access_token, actor.id]);
      assert.equal((await rows('SELECT access_token FROM users WHERE id=?', [actor.id]))[0].access_token, actor.access_token);
    }
    report.actor_restored = true; save();
  }
})().catch(error => {
  report.result = 'failed'; report.error = error.message; save();
  console.error('Task pricing acceptance failed; see private report'); process.exitCode = 1;
}).finally(async () => { if (db) await db.end(); });
