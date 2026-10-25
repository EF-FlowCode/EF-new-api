import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { chatLinkRequiresApiKey, resolveChatUrl } from './chat-links'

describe('chat link templates', () => {
  test('AQBot keeps each configuration field separate and requests a key', () => {
    const template = 'aqbot://providers?{aqbotConfig}'
    assert.equal(chatLinkRequiresApiKey(template), true)
    const url = new URL(
      resolveChatUrl({
        template,
        apiKey: 'sk-a&b=中+#',
        serverAddress: 'https://api.example/p?q=1&x=2',
      })
    )
    assert.equal(url.protocol, 'aqbot:')
    assert.equal(url.searchParams.get('name'), 'New API')
    assert.equal(
      url.searchParams.get('baseurl'),
      'https://api.example/p?q=1&x=2'
    )
    assert.equal(url.searchParams.get('apikey'), 'sk-a&b=中+#')
    assert.equal(url.searchParams.get('type'), 'openai')
    assert.equal([...url.searchParams].length, 4)
    assert.equal(chatLinkRequiresApiKey('https://chat.example/'), false)
  })
  test('DeepChat receives a base64 provider/install payload', () => {
    const template = 'deepchat://provider/install?v=1&data={deepchatConfig}'
    assert.equal(chatLinkRequiresApiKey(template), true)
    const resolved = resolveChatUrl({
      template,
      apiKey: 'sk-a+b/c=?',
      serverAddress: 'https://api.example',
    })
    assert.equal(resolved.includes('{deepchatConfig}'), false)
    const url = new URL(resolved)
    assert.equal(url.protocol, 'deepchat:')
    assert.equal(url.searchParams.get('v'), '1')
    const data = url.searchParams.get('data') ?? ''
    assert.deepEqual(JSON.parse(Buffer.from(data, 'base64').toString('utf8')), {
      id: 'new-api',
      baseUrl: 'https://api.example',
      apiKey: 'sk-a+b/c=?',
    })
  })
  test('URL-encodes the API key placeholder', () => {
    const url = resolveChatUrl({
      template: 'https://chat.example/?key={key}&url={address}',
      apiKey: 'sk-a&b=c',
      serverAddress: 'https://api.example',
    })

    assert.equal(
      url,
      'https://chat.example/?key=sk-a%26b%3Dc&url=https%3A%2F%2Fapi.example'
    )
  })
})
