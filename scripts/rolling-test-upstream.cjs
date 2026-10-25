const http = require('node:http')
const streams = new Set()
const chunk = (choices, usage) => ({
  id: 'chatcmpl-rolling-test', object: 'chat.completion.chunk',
  created: 1788790000, model: 'gpt-4o-mini', choices, ...(usage ? { usage } : {}),
})
http.createServer((req, res) => {
  req.resume()
  if (req.url === '/release') {
    for (const stream of streams) {
      stream.write(`data: ${JSON.stringify(chunk([{ index: 0, delta: {}, finish_reason: 'stop' }]))}\n\n`)
      stream.write(`data: ${JSON.stringify(chunk([], { prompt_tokens: 2, completion_tokens: 3, total_tokens: 5 }))}\n\n`)
      stream.end('data: [DONE]\n\n')
    }
    streams.clear()
    res.end('released')
    return
  }
  if (req.url === '/v1/chat/completions') {
    res.writeHead(200, { 'Content-Type': 'text/event-stream' })
    res.write(`data: ${JSON.stringify(chunk([{ index: 0, delta: { role: 'assistant', content: 'hello' }, finish_reason: null }]))}\n\n`)
    res.write(`data: ${JSON.stringify(chunk([{ index: 0, delta: { content: ' world' }, finish_reason: null }]))}\n\n`)
    streams.add(res)
    res.on('close', () => streams.delete(res))
    return
  }
  res.end('{}')
}).listen(8080, '0.0.0.0')
