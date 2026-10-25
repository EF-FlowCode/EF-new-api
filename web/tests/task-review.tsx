import { createRoot } from 'react-dom/client'
import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import {
  ReviewForm,
  type Detail,
} from '../src/features/usage-logs/components/dialogs/task-review-dialog'
import { api } from '../src/lib/api'

await i18n.use(initReactI18next).init({
  lng: 'en',
  fallbackLng: 'en',
  resources: {},
  interpolation: { escapeValue: false },
})
const mode = new URLSearchParams(location.search).get('mode') ?? 'token'
const detail: Detail = {
  id: 1,
  task_id: 'fixture',
  status: 'BILLING_PENDING',
  revision: 1,
  quota: 400,
  reason: 'settlement_pending',
  ready: mode === 'ready',
  edit_seconds: false,
  edit_resolution: false,
  locked_until: 0,
  billing: null,
  seconds: null,
  resolution: null,
}
if (mode === 'legacy')
  detail.billing = { version: 1, mode: 'fixed', base_quota: 250 }
if (!['legacy', 'missing'].includes(mode)) {
  detail.token_billing = {
    version: mode === 'invalid' ? 99 : 1,
    mode: 'tokens',
    model_ratio: 0.5,
    group_ratio: 0.5,
    other_multiplier: 1,
  }
  detail.completion_tokens = 1000
}
api.defaults.adapter = async (config) => {
  const payload =
    typeof config.data === 'string' ? JSON.parse(config.data) : config.data
  document.getElementById('payload')!.textContent = JSON.stringify(payload)
  if (mode === 'invalid') throw new Error('Invalid snapshot')
  return {
    config,
    status: 200,
    statusText: 'OK',
    headers: {},
    data: {
      data: {
        operation_id: 'fixture-op',
        before_quota: 400,
        after_quota: 250,
        expires: Math.floor(Date.now() / 1000) + 300,
      },
    },
  }
}
createRoot(document.getElementById('root')!).render(
  <>
    <ReviewForm detail={detail} done={() => {}} />
    <output id='payload' />
  </>
)
