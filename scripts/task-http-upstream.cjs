// Isolated synthetic provider. Never configure this service on a real channel.
const http = require('node:http');
const {randomUUID} = require('node:crypto');
const tasks = new Map();
process.on('SIGTERM', () => process.exit(0));
let submits = 0;
const send = (res, status, body) => { res.writeHead(status, {'Content-Type': 'application/json'}); res.end(JSON.stringify(body)); };
http.createServer(async (req, res) => {
  try {
    let raw = ''; for await (const chunk of req) raw += chunk;
    const body = raw ? JSON.parse(raw) : {};
    if (req.url === '/control/stats') return send(res, 200, {submits, tasks: [...tasks.values()]});
    if (req.url === '/control/finish' && req.method === 'POST') {
      const task = tasks.get(body.id); if (!task) return send(res, 404, {});
      Object.assign(task, {state: body.state || 'succeeded', tokens: body.tokens ?? 1000, seconds: body.seconds ?? 7});
      return send(res, 200, {ok: true});
    }
    if (req.method === 'POST' && (req.url === '/api/v3/contents/generations/tasks' || req.url.endsWith('/video-synthesis'))) {
      submits++;
      const id = 'mock-' + randomUUID();
      const provider = req.url.startsWith('/api/v3') ? 'doubao' : 'alibaba';
      tasks.set(id, {id, provider, model: body.model, state: process.env.TASK_HTTP_AUTO_COMPLETE === 'true' ? 'succeeded' : 'running', queries: 0});
      return send(res, 200, provider === 'doubao' ? {id} : {output: {task_id: id, task_status: 'PENDING'}});
    }
    const id = req.url.split('/').pop(); const task = tasks.get(id);
    if (req.method === 'GET' && task) {
      task.queries++;
      const url = 'http://upstream:8080/synthetic.mp4';
      return send(res, 200, task.provider === 'doubao'
        ? {id, status: task.state, content: {video_url: url}, usage: {total_tokens: task.tokens}, error: {message: 'synthetic failure'}}
        : {output: {task_id: id, task_status: {running: 'RUNNING', succeeded: 'SUCCEEDED', failed: 'FAILED'}[task.state], video_url: url}, usage: {input_video_duration: 0, output_video_duration: task.seconds, SR: '720P'}, message: 'synthetic failure'});
    }
    send(res, 404, {error: 'synthetic endpoint not found'});
  } catch (err) { send(res, 500, {error: String(err)}); }
}).listen(8080, '0.0.0.0');
