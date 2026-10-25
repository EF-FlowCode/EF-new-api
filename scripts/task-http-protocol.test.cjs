const {test} = require('node:test');
const assert = require('node:assert/strict');
const {responseBody, validateResponse, validateStream} = require('./task-http-protocol.cjs');
assert(require('node:fs').existsSync('/.dockerenv'), 'Run checks inside Docker');
const id = 'task_fixture';
const text = '/v1/videos/' + id + '/content';
const response = () => ({id, object: 'response', status: 'completed', usage: null, metadata: {task_id: id, retrieval_path: '/v1/responses/' + id}, output: [{content: [{text}]}]});
const events = () => [{type: 'response.created'}, {type: 'response.output_text.delta', delta: text}, {type: 'response.in_progress', response: {output: []}}, {type: 'response.completed', response: response()}];
const wire = items => ': heartbeat\n' + items.map((e, index) => 'event: ' + e.type + '\ndata: ' + JSON.stringify({sequence_number: index, ...e}) + '\n\n').join('');
test('Responses flags preserve combined mode', () => {
  assert.deepEqual(responseBody({background: true, stream: true}), {model: 'http-amount', input: 'synthetic task', seconds: 5, background: true, stream: true});
});
test('SSE heartbeat and outputs match final response', () => {
  assert.equal(validateStream(wire(events()), id).id, id);
});
test('SSE validator rejects duplicate output, sequence and terminal errors', () => {
  const duplicate = events(); duplicate.splice(2, 0, duplicate[1]);
  const sequence = events(); sequence[1].sequence_number = 7;
  const terminal = events(); terminal.push(terminal.at(-1));
  const error = events(); error.splice(1, 0, {type: 'error', code: 'task_render_failed'});
  const progress = events(); progress[2].response.output = [{text: 'duplicate'}];
  for (const value of [duplicate, sequence, terminal, error, progress, events().slice(0, -1)]) assert.throws(() => validateStream(wire(value), id));
});
test('response validator rejects wrong identity, usage and missing delivery', () => {
  for (const value of [{...response(), id: 'foreign'}, {...response(), usage: {}}, {...response(), output: []}]) assert.throws(() => validateResponse(value, id));
});
test('durable failure has one failed terminal', () => {
  const failed = {...response(), status: 'failed', output: [], error: {code: 'task_failed'}};
  assert.equal(validateStream(wire([{type: 'response.created'}, {type: 'response.failed', response: failed}]), id, 'failed').status, 'failed');
});
