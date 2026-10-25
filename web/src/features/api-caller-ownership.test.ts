import { AxiosError, AxiosHeaders, type AxiosAdapter } from 'axios'
import assert from 'node:assert/strict'
import { afterEach, test } from 'node:test'
import { api, type ApiRequestConfig } from '@/lib/api'
import { getOAuthState, login2fa, wechatLoginByCode } from '@/features/auth/api'
import {
  beginPasskeyLogin,
  beginPasskeyRegistration,
  beginPasskeyVerification,
  deletePasskey,
  finishPasskeyLogin,
  finishPasskeyRegistration,
  finishPasskeyVerification,
  getPasskeyStatus,
} from '@/features/auth/passkey/api'
import { verify } from '@/features/auth/secure-verification/api'
import {
  batchDeleteChannels,
  batchSetChannelTag,
  completeCodexOAuth,
  copyChannel,
  createChannel,
  deleteChannel,
  deleteDisabledChannels,
  disableTagChannels,
  editTagChannels,
  enableTagChannels,
  fetchModels,
  fixChannelAbilities,
  getCodexUsage,
  getChannelKey,
  refreshCodexCredential,
  startCodexOAuth,
  testAllChannels,
  testChannel,
  updateChannel,
  updateAllChannelsBalance,
  updateChannelBalance,
} from '@/features/channels/api'
import {
  fetchGroupHistory,
  refreshAdminMonitoring,
  updateGroupMonitoringSettings,
} from '@/features/group-monitoring/api'
import {
  batchDeleteApiKeys,
  createApiKey,
  deleteApiKey,
  fetchTokenKey,
  fetchTokenKeysBatch,
  updateApiKey,
  updateApiKeyStatus,
} from '@/features/keys/api'
import {
  applyUpstreamOverwrite,
  createDeployment,
  createModel,
  createPrefillGroup,
  createVendor,
  deleteDeployment,
  deleteModel,
  deletePrefillGroup,
  deleteVendor,
  extendDeployment,
  getDeploymentSettings,
  previewUpstreamDiff,
  syncUpstream,
  testDeploymentConnection,
  testDeploymentConnectionWithKey,
  updateDeployment,
  updateDeploymentName,
  updateModel,
  updateModelStatus,
  updatePrefillGroup,
  updateVendor,
} from '@/features/models/api'
import {
  bindEmail,
  deleteUserAccount,
  getSelfOAuthBindings,
  getUserProfile,
  performCheckin,
  sendEmailVerification,
  unbindCustomOAuth,
  updateUserLanguage,
  updateUserProfile,
  updateUserSettings,
} from '@/features/profile/api'
import { getRedemption } from '@/features/redemption-codes/api'
import { updateRiskConfig } from '@/features/risk-center/api'
import { createCustomOAuthProvider } from '@/features/system-settings/auth/custom-oauth/api'
import { clearGroupCache } from '@/features/system-settings/general/channel-affinity/api'
import { createTicket, fetchUserQuota } from '@/features/tickets/api'
import {
  adjustUserQuota,
  adminClearUserBinding,
  adminUnbindCustomOAuth,
  createUser,
  deleteUser,
  getUserOAuthBindings,
  manageUser,
  resetUserPasskey,
  resetUserTwoFA,
  setUserRole,
  updateUser,
} from '@/features/users/api'
import { redeemTopupCode, transferAffiliateQuota } from '@/features/wallet/api'

const originalAdapter = api.defaults.adapter

afterEach(() => {
  api.defaults.adapter = originalAdapter
})

function resultAdapter<T>(data: T, configs: ApiRequestConfig[]): AxiosAdapter {
  return async (config) => {
    configs.push(config as ApiRequestConfig)
    return {
      data,
      status: 200,
      statusText: 'OK',
      headers: new AxiosHeaders(),
      config,
    }
  }
}

function errorAdapter(
  status: number,
  message: string,
  configs: ApiRequestConfig[]
): AxiosAdapter {
  return async (config) => {
    configs.push(config as ApiRequestConfig)
    throw new AxiosError(
      'Transport failed',
      AxiosError.ERR_BAD_RESPONSE,
      config,
      undefined,
      {
        data: { message },
        status,
        statusText: 'Request failed',
        headers: new AxiosHeaders(),
        config,
      }
    )
  }
}

function assertCallerHandled(config: ApiRequestConfig | undefined) {
  assert.equal(config?.skipBusinessError, true)
  assert.equal(config?.skipErrorNotification, true)
}

test('risk API leaves business and HTTP error presentation to its callers', async () => {
  const configs: ApiRequestConfig[] = []
  api.defaults.adapter = resultAdapter(
    { success: false, message: 'Risk update denied' },
    configs
  )

  await assert.rejects(updateRiskConfig({} as never), /Risk update denied/)
  assertCallerHandled(configs[0])
})

test('ticket API unwraps business failures under caller-owned notification', async () => {
  const configs: ApiRequestConfig[] = []
  api.defaults.adapter = resultAdapter(
    { success: false, message: 'Ticket creation denied' },
    configs
  )

  await assert.rejects(createTicket({} as never), /Ticket creation denied/)
  assertCallerHandled(configs[0])
})

test('ticket quota probe stays silent and degrades HTTP failures to null', async () => {
  const configs: ApiRequestConfig[] = []
  api.defaults.adapter = errorAdapter(503, 'Quota unavailable', configs)

  assert.equal(await fetchUserQuota(42), null)
  assertCallerHandled(configs[0])
})

test('group history keeps its business-failure fallback without global notification', async () => {
  const configs: ApiRequestConfig[] = []
  api.defaults.adapter = resultAdapter(
    { success: false, message: 'History unavailable' },
    configs
  )

  assert.deepEqual(await fetchGroupHistory(true, 'default'), {
    history: [],
    intervalMinutes: 5,
  })
  assertCallerHandled(configs[0])
})

test('group mutations reject once and are presented by their callers', async () => {
  const updateConfigs: ApiRequestConfig[] = []
  api.defaults.adapter = resultAdapter(
    { success: false, message: 'Monitoring update denied' },
    updateConfigs
  )

  await assert.rejects(
    updateGroupMonitoringSettings({
      monitoring_groups: ['default'],
      group_categories: { default: 'core' },
      category_order: ['core'],
    }),
    /Monitoring update denied/
  )
  assert.equal(updateConfigs.length, 1)
  assertCallerHandled(updateConfigs[0])
  assert.equal(updateConfigs[0]?.method, 'put')
  assert.equal(updateConfigs[0]?.url, '/api/option/bulk')
  const updateBody =
    typeof updateConfigs[0]?.data === 'string'
      ? JSON.parse(updateConfigs[0].data)
      : updateConfigs[0]?.data
  assert.deepEqual(updateBody, {
    options: [
      {
        key: 'group_monitoring_setting.monitoring_groups',
        value: '["default"]',
      },
      {
        key: 'group_monitoring_setting.group_categories',
        value: '{"default":"core"}',
      },
      {
        key: 'group_monitoring_setting.category_order',
        value: '["core"]',
      },
    ],
  })

  const refreshConfigs: ApiRequestConfig[] = []
  api.defaults.adapter = resultAdapter(
    { success: false, message: 'Refresh denied' },
    refreshConfigs
  )
  await assert.rejects(refreshAdminMonitoring(), /Refresh denied/)
  assertCallerHandled(refreshConfigs[0])
})

test('channel create and update expose rejected results without global notification', async () => {
  const configs: ApiRequestConfig[] = []
  api.defaults.adapter = resultAdapter(
    { success: false, message: 'Duplicate channel' },
    configs
  )

  const created = await createChannel({} as never)
  const updated = await updateChannel(7, {})

  assert.equal(created.success, false)
  assert.equal(updated.success, false)
  assertCallerHandled(configs[0])
  assertCallerHandled(configs[1])
})

test('channel key access and model discovery leave errors to the drawer', async () => {
  const configs: ApiRequestConfig[] = []
  api.defaults.adapter = resultAdapter(
    { success: false, message: 'Channel operation denied' },
    configs
  )

  await getChannelKey(7, '123456')
  await fetchModels({ type: 1 } as never)

  assert.equal(configs.length, 2)
  configs.forEach(assertCallerHandled)
})

test('channel action commands leave error presentation to channel-actions', async () => {
  const configs: ApiRequestConfig[] = []
  api.defaults.adapter = resultAdapter(
    { success: false, message: 'Channel action denied' },
    configs
  )

  await deleteChannel(1)
  await batchDeleteChannels({ ids: [1] })
  await batchSetChannelTag({ ids: [1], tag: 'test' })
  await testChannel(1)
  await updateChannelBalance(1)
  await copyChannel(1)
  await fixChannelAbilities()
  await deleteDisabledChannels()
  await enableTagChannels('test')
  await disableTagChannels('test')
  await editTagChannels({ tag: 'test', priority: 1 })
  await testAllChannels()
  await updateAllChannelsBalance()

  assert.equal(configs.length, 13)
  configs.forEach(assertCallerHandled)
})

test('Codex channel operations leave both error classes to their dialogs', async () => {
  const configs: ApiRequestConfig[] = []
  api.defaults.adapter = resultAdapter(
    { success: false, message: 'Codex request denied' },
    configs
  )

  await startCodexOAuth()
  await completeCodexOAuth('callback')
  await refreshCodexCredential(7)
  await getCodexUsage(7)

  assert.equal(configs.length, 4)
  configs.forEach(assertCallerHandled)
  assert.equal(configs[3]?.disableDuplicate, true)
})

test('Passkey APIs are caller-owned across registration, login, and verification', async () => {
  const configs: ApiRequestConfig[] = []
  api.defaults.adapter = resultAdapter(
    { success: false, message: 'Passkey request denied' },
    configs
  )

  await getPasskeyStatus()
  await beginPasskeyRegistration('proof')
  await finishPasskeyRegistration({ flow_token: 'flow', credential: {} })
  await deletePasskey('proof')
  await beginPasskeyLogin()
  await finishPasskeyLogin({})
  await beginPasskeyVerification({ scope: 'passkey.delete' })
  await finishPasskeyVerification({ flow_token: 'flow', credential: {} })
  await assert.rejects(
    verify('2fa', { scope: 'account.delete' }, '123456'),
    /Passkey request denied/
  )

  assert.equal(configs.length, 9)
  configs.forEach(assertCallerHandled)
})

test('authentication continuations with local feedback are caller-owned', async () => {
  const configs: ApiRequestConfig[] = []
  api.defaults.adapter = resultAdapter(
    { success: false, message: 'Authentication continuation denied' },
    configs
  )

  await login2fa({} as never)
  assert.equal(await getOAuthState(), '')
  await wechatLoginByCode('invalid-code')

  assert.equal(configs.length, 3)
  configs.forEach(assertCallerHandled)
})

test('profile commands and silent probes use caller-owned notifications', async () => {
  const configs: ApiRequestConfig[] = []
  api.defaults.adapter = resultAdapter(
    { success: false, message: 'Profile request denied' },
    configs
  )

  await getUserProfile()
  await updateUserProfile({})
  await updateUserSettings({})
  await updateUserLanguage('zh')
  await deleteUserAccount()
  await sendEmailVerification('user@example.com')
  await bindEmail('user@example.com', '123456')
  await getSelfOAuthBindings()
  await unbindCustomOAuth(3)
  await performCheckin()

  assert.equal(configs.length, 10)
  configs.forEach(assertCallerHandled)
})

test('user administration commands and binding probes are caller-owned', async () => {
  const configs: ApiRequestConfig[] = []
  api.defaults.adapter = resultAdapter(
    { success: false, message: 'User request denied' },
    configs
  )

  await createUser({} as never)
  await updateUser({ id: 1 } as never)
  await deleteUser(1)
  await manageUser(1, 'disable')
  await setUserRole(1, 5)
  await adjustUserQuota({} as never)
  await resetUserPasskey(1)
  await resetUserTwoFA(1)
  await getUserOAuthBindings(1)
  await adminClearUserBinding(1, 'github')
  await adminUnbindCustomOAuth(1, 2)

  assert.equal(configs.length, 11)
  configs.forEach(assertCallerHandled)
})

test('API key commands and secret reveals are caller-owned', async () => {
  const configs: ApiRequestConfig[] = []
  api.defaults.adapter = resultAdapter(
    { success: false, message: 'API key request denied' },
    configs
  )

  await createApiKey({} as never)
  await updateApiKey({ id: 1 } as never)
  await deleteApiKey(1)
  await batchDeleteApiKeys([1, 2])
  await updateApiKeyStatus(1, 1)
  await fetchTokenKey(1)
  await fetchTokenKeysBatch([1, 2])

  assert.equal(configs.length, 7)
  configs.forEach(assertCallerHandled)
})

test('redemption edit lookup leaves failure presentation to its drawer', async () => {
  const configs: ApiRequestConfig[] = []
  api.defaults.adapter = resultAdapter(
    { success: false, message: 'Redemption lookup denied' },
    configs
  )

  await getRedemption(1)

  assert.equal(configs.length, 1)
  assertCallerHandled(configs[0])
})

test('model, vendor, sync, and prefill commands are caller-owned', async () => {
  const configs: ApiRequestConfig[] = []
  api.defaults.adapter = resultAdapter(
    { success: false, message: 'Model command denied' },
    configs
  )

  await createModel({})
  await updateModel({ id: 1 })
  await updateModelStatus(1, 1)
  await deleteModel(1)
  await createVendor({})
  await updateVendor({ id: 1 })
  await deleteVendor(1)
  await syncUpstream()
  await previewUpstreamDiff()
  await applyUpstreamOverwrite({ overwrite: [] })
  await createPrefillGroup({} as never)
  await updatePrefillGroup({ id: 1 })
  await deletePrefillGroup(1)

  assert.equal(configs.length, 13)
  configs.forEach(assertCallerHandled)
})

test('deployment commands and silent settings probes are caller-owned', async () => {
  const configs: ApiRequestConfig[] = []
  api.defaults.adapter = resultAdapter(
    { success: false, message: 'Deployment request denied' },
    configs
  )

  await getDeploymentSettings()
  await testDeploymentConnection()
  await testDeploymentConnectionWithKey('secret')
  await createDeployment({} as never)
  await updateDeployment(1, {})
  await updateDeploymentName(1, 'renamed')
  await extendDeployment(1, 1)
  await deleteDeployment(1)

  assert.equal(configs.length, 8)
  configs.forEach(assertCallerHandled)
  assert.equal(configs[0]?.skipErrorHandler, undefined)
  assert.equal(configs[1]?.skipErrorHandler, true)
  assert.equal(configs[2]?.skipErrorHandler, true)
})

test('channel affinity cache errors are owned by the settings page', async () => {
  const configs: ApiRequestConfig[] = []
  api.defaults.adapter = resultAdapter(
    { success: false, message: 'Cache clear denied' },
    configs
  )

  const result = await clearGroupCache('default')

  assert.equal(result.success, false)
  assertCallerHandled(configs[0])
})

test('custom OAuth mutations reject business failures for the mutation handler', async () => {
  const configs: ApiRequestConfig[] = []
  api.defaults.adapter = resultAdapter(
    { success: false, message: 'Provider rejected' },
    configs
  )

  await assert.rejects(
    createCustomOAuthProvider({} as never),
    /Provider rejected/
  )
  assertCallerHandled(configs[0])
})

test('wallet commands with local error toasts suppress global notifications', async () => {
  const configs: ApiRequestConfig[] = []
  api.defaults.adapter = resultAdapter(
    { success: false, message: 'Wallet command denied' },
    configs
  )

  await redeemTopupCode({ key: 'invalid-code' })
  await transferAffiliateQuota({ quota: 1 })

  assert.equal(configs.length, 2)
  configs.forEach(assertCallerHandled)
})
