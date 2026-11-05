import { createInstance } from 'i18next'
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { formatGroupDetail } from '../group-detail'

describe('model group selector group detail', () => {
  test('translates the backend description key and the auto ratio when the language is Chinese', async () => {
    const i18n = createInstance()
    await i18n.init({
      lng: 'zh',
      resources: {
        zh: {
          translation: {
            'User group': '用户分组',
            Auto: '自动',
            'Ratio: {{value}}': '倍率：{{value}}',
          },
        },
      },
    })

    assert.equal(
      formatGroupDetail({ desc: 'User group', ratio: 'auto' }, i18n.t),
      '用户分组 · 倍率：自动'
    )
  })

  test('keeps an administrator description and a numeric ratio as written', async () => {
    const i18n = createInstance()
    await i18n.init({ lng: 'en', resources: { en: { translation: {} } } })

    assert.equal(
      formatGroupDetail({ desc: 'VIP 专线', ratio: 1.5 }, i18n.t),
      'VIP 专线 · Ratio: 1.5'
    )
    assert.equal(
      formatGroupDetail({ description: 'No ratio' }, i18n.t),
      'No ratio'
    )
  })
})
