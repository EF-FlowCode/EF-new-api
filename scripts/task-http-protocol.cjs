const assert = require('node:assert/strict');

const responseBody = (flags = {}, model = 'http-amount') => ({model, input: 'synthetic task', seconds: 5, ...flags});
const outputText = response => response.output.flatMap(item => item.content).map(part => part.text).join('');

function validateResponse(response, id, status = 'completed') {
  assert.equal(response.id, id);
  assert.equal(response.object, 'response');
  assert.equal(response.status, status);
  assert.equal(response.usage, null);
  assert.equal(response.metadata.task_id, id);
  assert.equal(response.metadata.retrieval_path, '/v1/responses/' + id);
  if (status === 'completed') assert(outputText(response).includes('/v1/videos/' + id + '/content'));
  else assert.equal(response.error.code, 'task_failed');
  return response;
}

function validateStream(text, id, status = 'completed') {
  const events = text.split(/\r?\n/).filter(line => line.startsWith('data: ')).map(line => JSON.parse(line.slice(6)));
  assert.equal(events[0]?.type, 'response.created');
  events.forEach((event, index) => assert.equal(event.sequence_number, index));
  assert.equal(events.filter(e => e.type === 'error' || e.type === 'response.incomplete').length, 0, text);
  const terminal = events.filter(e => e.type === 'response.completed' || e.type === 'response.failed');
  assert.equal(terminal.length, 1);
  assert.equal(terminal[0], events.at(-1));
  assert.equal(terminal[0].type, 'response.' + status);
  const result = validateResponse(terminal[0].response, id, status);
  if (status === 'completed') assert.equal(events.filter(e => e.type === 'response.output_text.delta').map(e => e.delta).join(''), outputText(result));
  for (const e of events.filter(e => e.type === 'response.in_progress')) assert.deepEqual(e.response.output, []);
  return result;
}

module.exports = {responseBody, validateResponse, validateStream};
