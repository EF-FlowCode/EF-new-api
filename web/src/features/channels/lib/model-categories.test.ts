import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { categorizeModels, getModelCategory } from './model-categories'

describe('model categorization', () => {
  test('classifies provider-specific model families', () => {
    assert.equal(getModelCategory('nvidia/llama-3.1-nemotron'), 'NVIDIA')
    assert.equal(getModelCategory('qwen-tts-1.7b'), 'Qwen')
    assert.equal(getModelCategory('tts-1'), 'OpenAI')
    assert.equal(getModelCategory('moonshot-v1-8k'), 'Moonshot')
    assert.equal(getModelCategory('flux.1-dev'), 'Black Forest Labs')
    assert.equal(getModelCategory('unrecognised-model'), 'Other')
  })

  test('preserves input order within each category', () => {
    assert.deepEqual(
      categorizeModels(['gpt-4o', 'claude-3-7-sonnet', 'gpt-4.1']),
      {
        OpenAI: ['gpt-4o', 'gpt-4.1'],
        Anthropic: ['claude-3-7-sonnet'],
      }
    )
  })
})
