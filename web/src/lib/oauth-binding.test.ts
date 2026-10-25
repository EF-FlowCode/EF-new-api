import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  buildCustomOAuthAuthorizationUrl,
  indexCustomOAuthBindings,
  type CustomOAuthBinding,
} from './oauth'

const bindings: CustomOAuthBinding[] = [
  {
    provider_id: 7,
    provider_name: 'Example OAuth',
    provider_slug: 'example',
    provider_icon: '',
    provider_user_id: 'user-42',
  },
]

describe('custom OAuth bindings', () => {
  test('indexes bindings by numeric provider id', () => {
    const indexed = indexCustomOAuthBindings(bindings)
    assert.equal(indexed.get(7)?.provider_user_id, 'user-42')
    assert.equal(indexed.get(8), undefined)
  })

  test('builds the provider authorization URL with the binding state', () => {
    const authorization = new URL(
      buildCustomOAuthAuthorizationUrl(
        'https://app.example/',
        'example-provider',
        'https://idp.example/authorize?prompt=login',
        'client-123',
        'openid profile',
        'state-456'
      )
    )

    assert.equal(authorization.pathname, '/authorize')
    assert.equal(authorization.searchParams.get('prompt'), 'login')
    assert.equal(authorization.searchParams.get('client_id'), 'client-123')
    assert.equal(
      authorization.searchParams.get('redirect_uri'),
      'https://app.example/oauth/example-provider'
    )
    assert.equal(authorization.searchParams.get('response_type'), 'code')
    assert.equal(authorization.searchParams.get('state'), 'state-456')
    assert.equal(authorization.searchParams.get('scope'), 'openid profile')
  })
})
