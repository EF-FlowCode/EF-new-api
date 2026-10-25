import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  CHANNEL_TYPE_OPTIONS,
  CHANNEL_TYPE_SGLANG,
  CHANNEL_TYPE_VLLM,
  isModelFetchableChannel,
} from '../constants'
import { CHANNEL_FORM_DEFAULT_VALUES, channelFormSchema } from './channel-form'
import { getChannelTypeIcon, getKeyPromptForType } from './channel-utils'

const inferenceTypes = [CHANNEL_TYPE_VLLM, CHANNEL_TYPE_SGLANG]

test('vLLM and SGLang use the upstream channel type numbers', () => {
  assert.equal(CHANNEL_TYPE_VLLM, 62)
  assert.equal(CHANNEL_TYPE_SGLANG, 63)
  const options = new Map(CHANNEL_TYPE_OPTIONS.map((o) => [o.value, o.label]))
  assert.equal(options.get(62), 'vLLM')
  assert.equal(options.get(63), 'SGLang')
  assert.equal(getChannelTypeIcon(62), 'Vllm')
})

test('vLLM and SGLang require an API address and can fetch models', () => {
  for (const type of inferenceTypes) {
    const base = {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'local',
      models: 'qwen3',
      key: 'EMPTY',
      type,
    }
    const missing = channelFormSchema.safeParse({ ...base, base_url: ' ' })
    assert.equal(missing.success, false)
    assert.ok(
      !missing.success &&
        missing.error.issues.some((issue) => issue.path[0] === 'base_url')
    )
    assert.equal(
      channelFormSchema.safeParse({
        ...base,
        base_url: 'http://127.0.0.1:8000',
      }).success,
      true
    )
    assert.equal(isModelFetchableChannel(type), true)
    assert.match(getKeyPromptForType(type), /EMPTY/)
  }
  // Other types keep their optional address.
  assert.equal(
    channelFormSchema.safeParse({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'openai',
      models: 'gpt-4o',
      key: 'sk-test',
      type: 1,
    }).success,
    true
  )
})
