import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { classifyChannelAddress, formatChannelAddress } from './channel-address'

describe('channel address classification', () => {
  test('treats IP literals as ip', () => {
    assert.equal(classifyChannelAddress('http://203.0.113.10:8080'), 'ip')
    assert.equal(classifyChannelAddress('https://1.2.3.4'), 'ip')
    assert.equal(classifyChannelAddress('10.0.0.1:3000'), 'ip')
    assert.equal(classifyChannelAddress('http://[2001:db8::1]:8080/v1'), 'ip')
  })

  test('treats hostnames and empty values as domain', () => {
    assert.equal(classifyChannelAddress('https://relay.example.net'), 'domain')
    assert.equal(classifyChannelAddress('http://localhost:3000'), 'domain')
    assert.equal(classifyChannelAddress('http://1.2.3.4.example.com'), 'domain')
    assert.equal(classifyChannelAddress(''), 'domain')
    assert.equal(classifyChannelAddress(null), 'domain')
  })

  test('formats address for display', () => {
    assert.equal(formatChannelAddress('https://ab.example/'), 'ab.example')
    assert.equal(
      formatChannelAddress('http://203.0.113.10:8080'),
      '203.0.113.10:8080'
    )
    assert.equal(formatChannelAddress(''), '')
  })
})
