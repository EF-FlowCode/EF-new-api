// Called only inside the release-owned clone's Docker tool container.
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
module.exports = async function matrix({rows, origin, headers, entries, options, label}) {
  const api = async (path, data, method = 'PUT') => {
    const response = await fetch(origin + path, {method, headers: {...headers, 'Content-Type': 'application/json'}, body: JSON.stringify(data), signal: AbortSignal.timeout(45000)});
    const body = await response.json(); assert.equal(response.status, 200, JSON.stringify(body)); assert.equal(body.success, true, JSON.stringify(body)); return body.data;
  };
  const parsed = key => JSON.parse(options.find(row => row.key === key)?.value || '{}');
  const groups = parsed('GroupRatio'), tiers = parsed('GroupGroupRatio');
  const quotaPerUnit = Number(options.find(row => row.key === 'QuotaPerUnit')?.value || 500000);
  assert(Number.isFinite(quotaPerUnit) && quotaPerUnit > 0);
  const usable = parsed('UserUsableGroups'), specials = parsed('group_ratio_setting.group_special_usable_group');
  assert.equal(Object.keys(tiers).length, 0, 'Tier pricing needs explicit HTTP fixtures before this gate can pass');
  assert(entries.length > 0, 'Task HTTP inventory is empty');
  assert(entries.every(e => e.plugin === 'happyhorse' && e.rule?.kind === 'unit_price'), 'Add provider/contract fixtures for every captured task before accepting');
  const channels = await rows('SELECT id,status FROM channels WHERE id IN (' + [...new Set(entries.map(e => e.channel_id))].map(() => '?').join(',') + ')', [...new Set(entries.map(e => e.channel_id))]);
  const privateOption = options.find(row => row.key === 'fetch_setting.allow_private_ip');
  const cases = [], tasks = [];
  try {
    await api('/api/option/', {key: 'fetch_setting.allow_private_ip', value: 'true'});
    for (const ch of channels) if (ch.status !== 1) await api('/api/channel/', {id: ch.id, status: 1});
    for (const group of Object.keys(groups).sort()) {
      let customer = 'default';
      if (!Object.hasOwn(usable, group)) {
        customer = Object.keys(specials).find(tier => Object.hasOwn(specials[tier], '+:' + group) || Object.hasOwn(specials[tier], group));
        assert(customer, 'No captured group authorization fixture for ' + group);
      }
      const name = 'tm_' + crypto.randomBytes(7).toString('hex');
      const result = await rows('INSERT INTO users (username,password,display_name,role,status,quota,`group`,aff_code,setting) VALUES (?,?,?,10,1,1000000000,?,?,?)', [name, 'synthetic-no-login', name, customer, name, '{}']);
      const userID = result.insertId, token = crypto.randomBytes(24).toString('hex');
      const tokenResult = await rows('INSERT INTO tokens (user_id,`key`,name,status,remain_quota,used_quota,unlimited_quota,expired_time,`group`,model_limits_enabled,cross_group_retry) VALUES (?,?,?,1,1000000000,0,0,-1,?,0,0)', [userID, token, name, group]);
      const balance = async () => ({user: (await rows('SELECT quota FROM users WHERE id=?', [userID]))[0].quota, token: (await rows('SELECT remain_quota FROM tokens WHERE id=?', [tokenResult.insertId]))[0].remain_quota});
      for (const e of entries) {
        const before = await balance();
        const response = await fetch(origin + '/v1/videos', {method: 'POST', headers: {'Content-Type': 'application/json', Authorization: 'Bearer sk-' + token + '-' + e.channel_id, 'Idempotency-Key': name + '/' + e.channel_id + '/' + e.model}, body: JSON.stringify({model: e.model, prompt: 'isolated billing acceptance', duration: 5, size: '1280x720', input_reference: 'http://upstream:8080/input.png', metadata: {media_url: 'http://upstream:8080/input.mp4'}}), signal: AbortSignal.timeout(45000)});
        const body = await response.json(); assert.equal(response.status, 200, label + '/' + group + '/' + e.model + ': ' + JSON.stringify(body));
        const id = body.id || body.task_id; assert(id);
        const task = (await rows('SELECT task_id,quota,status FROM tasks WHERE task_id=?', [id]))[0]; assert(task);
        const expected = Math.trunc(Math.trunc(e.rule.unit_price * quotaPerUnit * groups[group]) * (e.rule.dimensions === 'none' ? 1 : 5));
        assert.equal(task.quota, expected, 'Independent USD/unit, group and duration arithmetic');
        const after = await balance();
        assert.equal(before.user - after.user, task.quota); assert.equal(before.token - after.token, task.quota);
        cases.push({model: e.model, plugin: e.plugin, channel_id: e.channel_id, group, customer, expected, reserved: task.quota, wallet_delta: before.user - after.user, token_delta: before.token - after.token});
        tasks.push({id, quota: task.quota, userID, tokenID: tokenResult.insertId, after});
      }
    }
    // Query through the application as well as the worker. Completion must not
    // alter the submitted-duration contract or leave any accepted task pending.
    const deadline = Date.now() + 600000;
    while (true) {
      const pending = [];
      for (const t of tasks) {
        const current = (await rows('SELECT status,quota FROM tasks WHERE task_id=?', [t.id]))[0];
        assert.notEqual(current.status, 'FAILURE'); assert.equal(current.quota, t.quota);
        if (current.status !== 'SUCCESS') pending.push(t.id);
      }
      if (!pending.length) break;
      assert(Date.now() < deadline, 'Task completion timeout: ' + pending.length);
      await delay(1000);
    }
    for (const userID of new Set(tasks.map(t => t.userID))) {
      const mine = tasks.filter(t => t.userID === userID), total = mine.reduce((sum,t) => sum + t.quota, 0);
      const user = (await rows('SELECT quota FROM users WHERE id=?', [userID]))[0];
      const token = (await rows('SELECT remain_quota FROM tokens WHERE id=?', [mine[0].tokenID]))[0];
      assert.equal(1000000000 - user.quota, total); assert.equal(1000000000 - token.remain_quota, total);
    }
    return {cases, completed: tasks.length, groups: Object.keys(groups).length, scope: 'all captured task entries and group ratios; forced channels on synthetic admin users, cloned paused channels temporarily enabled; simulated provider, submitted 5s/720P'};
  } finally {
    for (const ch of channels) if (ch.status !== 1) await api('/api/channel/', {id: ch.id, status: ch.status});
    await api('/api/option/', {key: 'fetch_setting.allow_private_ip', value: privateOption?.value || 'false'});
    if (!privateOption) await rows('DELETE FROM options WHERE `key`=?', ['fetch_setting.allow_private_ip']);
  }
};
