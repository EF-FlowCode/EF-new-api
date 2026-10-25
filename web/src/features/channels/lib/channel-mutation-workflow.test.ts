import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { channelSchema, type Channel } from '../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  type ChannelFormValues,
} from './channel-form'
import {
  cancelPendingChannelMutationConfirmations,
  createChannelKeyRequestGuard,
  executeChannelMutation,
  executeModelDiscovery,
  hydrateChannelMutation,
  loadUnlockedChannelKey,
  planModelDiscovery,
  settlePendingConfirmation,
  type ChannelMutationBaseline,
  type ChannelMutationDependencies,
  type ChannelMutationUpdatePayload,
  type MissingModelsResolution,
} from './channel-mutation-workflow'

function createFormValues(
  overrides: Partial<ChannelFormValues> = {}
): ChannelFormValues {
  return {
    ...CHANNEL_FORM_DEFAULT_VALUES,
    group: [...CHANNEL_FORM_DEFAULT_VALUES.group],
    rate_limit: CHANNEL_FORM_DEFAULT_VALUES.rate_limit
      ? { ...CHANNEL_FORM_DEFAULT_VALUES.rate_limit }
      : undefined,
    risk_control_headers: [],
    error_filter_rules: [],
    name: 'Primary channel',
    key: 'sk-test',
    models: 'gpt-4o',
    ...overrides,
  }
}

function createChannel(overrides: Partial<Channel> = {}): Channel {
  return channelSchema.parse({
    id: 17,
    type: 1,
    key: 'server-only-secret',
    status: 1,
    name: 'Existing channel',
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
    base_url: 'https://api.example.com',
    models: 'gpt-4o',
    ...overrides,
  })
}

function createBaseline(
  overrides: Partial<ChannelMutationBaseline> = {}
): ChannelMutationBaseline {
  return {
    models: [],
    modelMapping: '',
    statusCodeMapping: '',
    ...overrides,
  }
}

function createDependencies(
  overrides: Partial<ChannelMutationDependencies> = {}
): ChannelMutationDependencies {
  return {
    confirmStatusCodeRisk: async () => true,
    confirmMissingModels: async () => 'submit',
    createChannel: async () => ({ success: true }),
    updateChannel: async () => ({ success: true }),
    refreshChannels: () => undefined,
    ...overrides,
  }
}

test('saves multi-key selection mode without unlocked keys or runtime state', async () => {
  for (const isMultiKey of [true, false]) {
    for (const mode of ['random', 'polling'] as const) {
      let submitted: ChannelMutationUpdatePayload | undefined
      const outcome = await executeChannelMutation(
        {
          formValues: createFormValues({ key: '', multi_key_type: mode }),
          baseline: createBaseline(),
          target: { kind: 'update', channelId: 17, isMultiKey },
        },
        createDependencies({
          updateChannel: async (_id, payload) => {
            submitted = payload
            return { success: true }
          },
        })
      )
      assert.equal(outcome.status, 'updated')
      assert.equal(submitted?.multi_key_mode, isMultiKey ? mode : undefined)
      assert.equal(submitted?.key, undefined)
      assert.equal(submitted?.channel_info, undefined)
    }
  }
})

describe('channel mutation hydration and key access', () => {
  test('invalidates key responses after switching or closing the channel owner', () => {
    const guard = createChannelKeyRequestGuard()
    guard.setOwner(17)
    const first = guard.begin(17)

    guard.setOwner(18)
    assert.equal(first.isCurrent(), false)

    const second = guard.begin(18)
    assert.equal(second.isCurrent(), true)

    guard.setOwner(null)
    assert.equal(second.isCurrent(), false)
  })

  test('only lets the latest key request for an owner commit', () => {
    const guard = createChannelKeyRequestGuard()
    guard.setOwner(17)
    const first = guard.begin(17)
    const second = guard.begin(17)

    assert.equal(first.isCurrent(), false)
    assert.equal(second.isCurrent(), true)
  })

  test('hydrates create and edit state without exposing the saved key', () => {
    const createState = hydrateChannelMutation()
    assert.equal(createState.formValues.key, '')
    assert.deepEqual(createState.baseline, {
      models: [],
      modelMapping: '',
      statusCodeMapping: '',
    })

    createState.formValues.group.push('mutated')
    assert.deepEqual(hydrateChannelMutation().formValues.group, ['default'])

    const editState = hydrateChannelMutation(
      createChannel({
        models: 'gpt-4o, claude-3-5-sonnet',
        model_mapping: '{"legacy":"gpt-4o"}',
        status_code_mapping: '{"429":503}',
        channel_info: {
          is_multi_key: true,
          multi_key_size: 2,
          multi_key_polling_index: 0,
          multi_key_mode: 'polling',
        },
      })
    )

    assert.equal(editState.formValues.key, '')
    assert.equal(editState.formValues.key_mode, 'append')
    assert.equal(editState.formValues.multi_key_type, 'polling')
    assert.deepEqual(editState.baseline, {
      models: ['gpt-4o', 'claude-3-5-sonnet'],
      modelMapping: '{"legacy":"gpt-4o"}',
      statusCodeMapping: '{"429":503}',
      status: 1,
    })
  })

  test('only returns a key after the key endpoint accepts the request', async () => {
    assert.equal(
      await loadUnlockedChannelKey(17, async (id) => ({
        success: id === 17,
        data: { key: 'revealed-secret' },
      })),
      'revealed-secret'
    )

    await assert.rejects(
      () =>
        loadUnlockedChannelKey(17, async () => ({
          success: false,
          message: 'verification expired',
        })),
      /verification expired/
    )
  })
})

describe('model discovery workflow', () => {
  test('plans creation fetches and blocks missing credentials', () => {
    const blocked = planModelDiscovery({
      formValues: createFormValues({ key: '' }),
    })
    assert.deepEqual(blocked, {
      kind: 'blocked',
      reason: 'create-key-required',
    })

    const plan = planModelDiscovery({
      formValues: createFormValues({
        key: '  sk-preserve-spacing  ',
        base_url: 'https://api.example.com/v1',
      }),
    })
    assert.equal(plan.kind, 'fetch')
    if (plan.kind !== 'fetch') return
    assert.deepEqual(plan.request, {
      type: 1,
      key: '  sk-preserve-spacing  ',
      base_url: 'https://api.example.com/v1',
      advanced_custom: undefined,
      header_override: undefined,
      proxy: undefined,
    })
  })

  test('uses the saved channel for unchanged edits and unsaved values after a change', () => {
    const currentChannel = {
      id: 17,
      type: 1,
      base_url: 'https://api.example.com',
    }

    assert.deepEqual(
      planModelDiscovery({
        formValues: createFormValues({
          key: '',
          base_url: 'https://api.example.com',
        }),
        currentChannel,
      }),
      { kind: 'preview', request: null }
    )

    assert.deepEqual(
      planModelDiscovery({
        formValues: createFormValues({
          key: '',
          base_url: 'https://other.example.com',
        }),
        currentChannel,
      }),
      {
        kind: 'blocked',
        reason: 'changed-connection-key-required',
      }
    )

    const changedPlan = planModelDiscovery({
      formValues: createFormValues({
        key: ' replacement-key ',
        base_url: ' https://other.example.com ',
      }),
      currentChannel,
    })
    assert.equal(changedPlan.kind, 'preview')
    if (changedPlan.kind !== 'preview') return
    assert.deepEqual(changedPlan.request, {
      type: 1,
      key: 'replacement-key',
      base_url: 'https://other.example.com',
      channel_id: undefined,
      advanced_custom: undefined,
      header_override: undefined,
      proxy: undefined,
    })
  })

  test('executes only fetch plans and returns the fetched models', async () => {
    const plan = planModelDiscovery({ formValues: createFormValues() })
    assert.equal(plan.kind, 'fetch')
    if (plan.kind !== 'fetch') return

    const requests: unknown[] = []
    const result = await executeModelDiscovery(plan, async (request) => {
      requests.push(request)
      return { success: true, data: ['gpt-4o', 'o3'] }
    })

    assert.equal(requests.length, 1)
    assert.deepEqual(result, {
      kind: 'fetched',
      models: ['gpt-4o', 'o3'],
    })
  })
})

describe('channel mutation submission workflow', () => {
  test('settles a pending confirmation at most once', async () => {
    let settlementCount = 0
    const pending = {
      current: null as ((value: MissingModelsResolution) => void) | null,
    }
    const result = new Promise<MissingModelsResolution>((resolve) => {
      pending.current = (value) => {
        settlementCount += 1
        resolve(value)
      }
    })

    settlePendingConfirmation(pending, 'cancel')
    settlePendingConfirmation(pending, 'submit')

    assert.equal(await result, 'cancel')
    assert.equal(settlementCount, 1)
    assert.equal(pending.current, null)
  })

  test('cancels every pending confirmation when the outer drawer closes', async () => {
    const missingModels = {
      current: null as ((value: MissingModelsResolution) => void) | null,
    }
    const statusCodeRisk = {
      current: null as ((value: boolean) => void) | null,
    }
    const missingModelsResult = new Promise<MissingModelsResolution>(
      (resolve) => {
        missingModels.current = resolve
      }
    )
    const statusCodeRiskResult = new Promise<boolean>((resolve) => {
      statusCodeRisk.current = resolve
    })

    cancelPendingChannelMutationConfirmations(missingModels, statusCodeRisk)

    assert.equal(await missingModelsResult, 'cancel')
    assert.equal(await statusCodeRiskResult, false)
    assert.equal(missingModels.current, null)
    assert.equal(statusCodeRisk.current, null)
  })

  test('does not write when a new high-risk status redirect is cancelled', async () => {
    let creates = 0
    let confirmationItems: string[] = []
    const outcome = await executeChannelMutation(
      {
        formValues: createFormValues({
          status_code_mapping: '{"504":200}',
        }),
        baseline: createBaseline(),
        target: { kind: 'create' },
      },
      createDependencies({
        confirmStatusCodeRisk: async (items) => {
          confirmationItems = items
          return false
        },
        createChannel: async () => {
          creates += 1
          return { success: true }
        },
      })
    )

    assert.deepEqual(confirmationItems, ['504 -> 200'])
    assert.deepEqual(outcome, {
      status: 'cancelled',
      reason: 'status-code-risk',
    })
    assert.equal(creates, 0)
  })

  test('does not write when missing model confirmation is cancelled', async () => {
    let creates = 0
    const outcome = await executeChannelMutation(
      {
        formValues: createFormValues({
          model_mapping: '{"legacy-model":"gpt-4o"}',
        }),
        baseline: createBaseline(),
        target: { kind: 'create' },
      },
      createDependencies({
        confirmMissingModels: async () => 'cancel',
        createChannel: async () => {
          creates += 1
          return { success: true }
        },
      })
    )

    assert.deepEqual(outcome, {
      status: 'cancelled',
      reason: 'missing-model-mapping',
    })
    assert.equal(creates, 0)
  })

  test('adds missing models to the form and create payload before writing', async () => {
    let createdPayload:
      | Parameters<ChannelMutationDependencies['createChannel']>[0]
      | undefined
    let displayedModels = ''
    let refreshes = 0
    const requestStates: boolean[] = []

    const outcome = await executeChannelMutation(
      {
        formValues: createFormValues({
          models: 'gpt-4o',
          model_mapping: '{"legacy-model":"gpt-4o"}',
        }),
        baseline: createBaseline(),
        target: { kind: 'create' },
      },
      createDependencies({
        confirmMissingModels: async () => 'add',
        createChannel: async (payload) => {
          createdPayload = payload
          return { success: true }
        },
        refreshChannels: () => {
          refreshes += 1
        },
        onModelsChanged: (models) => {
          displayedModels = models
        },
        onRequestStateChange: (pending) => requestStates.push(pending),
      })
    )

    assert.deepEqual(outcome, { status: 'created' })
    assert.equal(displayedModels, 'gpt-4o,legacy-model')
    assert.equal(createdPayload?.channel.models, 'gpt-4o,legacy-model')
    assert.equal(createdPayload?.mode, 'single')
    assert.equal(refreshes, 1)
    assert.deepEqual(requestStates, [true, false])
  })

  test('preserves edit multi-key mode in the update payload', async () => {
    let updatedId = 0
    let updatedPayload: ChannelMutationUpdatePayload | undefined
    let refreshes = 0
    const outcome = await executeChannelMutation(
      {
        formValues: createFormValues({
          key: 'replacement-a\nreplacement-b',
          key_mode: 'replace',
        }),
        baseline: createBaseline({ models: ['gpt-4o'] }),
        target: { kind: 'update', channelId: 17, isMultiKey: true },
      },
      createDependencies({
        updateChannel: async (id, payload) => {
          updatedId = id
          updatedPayload = payload
          return { success: true }
        },
        refreshChannels: () => {
          refreshes += 1
        },
      })
    )

    assert.deepEqual(outcome, { status: 'updated' })
    assert.equal(updatedId, 17)
    assert.equal(updatedPayload?.id, 17)
    assert.equal(updatedPayload?.key, 'replacement-a\nreplacement-b')
    assert.equal(updatedPayload?.key_mode, 'replace')
    assert.equal(refreshes, 1)
  })

  test('does not refresh the cache when the API rejects the mutation', async () => {
    let refreshes = 0
    const outcome = await executeChannelMutation(
      {
        formValues: createFormValues(),
        baseline: createBaseline(),
        target: { kind: 'create' },
      },
      createDependencies({
        createChannel: async () => ({
          success: false,
          message: 'duplicate channel',
        }),
        refreshChannels: () => {
          refreshes += 1
        },
      })
    )

    assert.deepEqual(outcome, {
      status: 'rejected',
      operation: 'create',
      message: 'duplicate channel',
    })
    assert.equal(refreshes, 0)
  })
})
