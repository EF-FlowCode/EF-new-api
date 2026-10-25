const http = require('node:http');
http.createServer(async (req, res) => {
  let input = '';
  for await (const part of req) input += part;
  const body = JSON.parse(input || '{}');
  let usage;
  if (body.model === 'jev-1.13' || req.url === '/v1/embeddings') {
    usage = { prompt_tokens: 14, completion_tokens: 0, total_tokens: 14 };
  } else if (body.model === 'qwen3.5-omni-flash') {
    usage = { prompt_tokens: 1000, completion_tokens: 1000, total_tokens: 2000,
      prompt_tokens_details: { text_tokens: 800, audio_tokens: 200, cached_tokens: 0 },
      completion_tokens_details: { text_tokens: 900, audio_tokens: 100 } };
  } else {
    usage = { prompt_tokens: 1000, completion_tokens: 1000, total_tokens: 2000,
      prompt_tokens_details: { cached_tokens: 200 } };
  }
  res.writeHead(200, { 'Content-Type': 'application/json' });
  if (req.url === '/v1/embeddings') {
    res.end(JSON.stringify({ object: 'list', model: body.model,
      data: [{ object: 'embedding', index: 0, embedding: [0.1, 0.2, 0.3] }], usage }));
  } else {
    res.end(JSON.stringify({ id: 'local-billing-matrix', object: 'chat.completion', created: 1, model: body.model,
      choices: [{ index: 0, message: { role: 'assistant', content: 'Local billing verification' }, finish_reason: 'stop' }], usage }));
  }
}).listen(8080, '0.0.0.0');
