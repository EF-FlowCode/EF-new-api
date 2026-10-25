import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { describe, it } from 'node:test'

function readSource(relativePath: string): string {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8')
}

const callerContracts = [
  {
    name: 'announcements',
    source: readSource('../content/announcements-section.tsx'),
    removedMessages: [
      'Failed to update setting',
      'Failed to save announcements',
    ],
  },
  {
    name: 'FAQ',
    source: readSource('../content/faq-section.tsx'),
    removedMessages: ['Failed to update setting', 'Failed to save FAQ'],
  },
  {
    name: 'Uptime Kuma',
    source: readSource('../content/uptime-kuma-section.tsx'),
    removedMessages: [
      'Failed to update setting',
      'Failed to save Uptime Kuma groups',
    ],
  },
] as const

describe('useUpdateOption caller notification ownership', () => {
  for (const contract of callerContracts) {
    it(`${contract.name} leaves mutation errors to the shared hook`, () => {
      const ownershipComments = contract.source.match(
        /useUpdateOption owns the error notification\./g
      )

      assert.equal(ownershipComments?.length, 2)
      for (const message of contract.removedMessages) {
        assert.equal(
          contract.source.includes(`toast.error(t('${message}'))`),
          false
        )
      }
    })

    it(`${contract.name} keeps pre-submit validation feedback`, () => {
      assert.equal(
        contract.source.includes(
          "toast.error(t('Please select items to delete'))"
        ),
        true
      )
    })
  }
})
