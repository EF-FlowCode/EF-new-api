import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { describe, it } from 'node:test'

function readSource(relativePath: string): string {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8')
}

function exportedFunction(source: string, name: string): string {
  const start = source.indexOf(`export async function ${name}(`)
  assert.notEqual(start, -1, `missing ${name}`)
  const nextExport = source.indexOf('\nexport ', start + 1)
  return source.slice(start, nextExport === -1 ? undefined : nextExport)
}

describe('paginated request ownership contract', () => {
  it('gives every independently paginated data flow its own latest-request slot', () => {
    const invitation = readSource('../../features/invitation-codes/index.tsx')
    const distribution = readSource(
      '../../features/risk-center/components/distribution-tab.tsx'
    )
    const enforcement = readSource(
      '../../features/risk-center/components/enforcement-tab.tsx'
    )
    const moderation = readSource(
      '../../features/risk-center/components/moderation-tab.tsx'
    )
    const topupHistory = readSource('../../features/topup-history/index.tsx')

    assert.match(invitation, /const runListRequest = useLatestRequest\(\)/)
    assert.match(
      distribution,
      /const runSubjectsRequest = useLatestRequest\(\)/
    )
    assert.match(
      distribution,
      /const runIncidentsRequest = useLatestRequest\(\)/
    )
    assert.match(enforcement, /const runCountersRequest = useLatestRequest\(\)/)
    assert.match(
      enforcement,
      /const runIncidentsRequest = useLatestRequest\(\)/
    )
    assert.match(moderation, /const runIncidentsRequest = useLatestRequest\(\)/)
    assert.match(topupHistory, /const runListRequest = useLatestRequest\(\)/)
    assert.match(topupHistory, /paramsRef\.current = params/)
    assert.match(topupHistory, /await refresh\(paramsRef\.current\)/)
  })

  it('passes an AbortSignal through every protected list API', () => {
    const invitationApi = readSource('../../features/invitation-codes/api.ts')
    const riskApi = readSource('../../features/risk-center/api.ts')
    const topupApi = readSource('../../features/topup-history/api.ts')
    const protectedApis = [
      [invitationApi, 'fetchInvitationCodes'],
      [topupApi, 'fetchTopups'],
      [riskApi, 'fetchSubjects'],
      [riskApi, 'fetchIncidents'],
      [riskApi, 'fetchModerationIncidents'],
      [riskApi, 'fetchEnforcementCounters'],
      [riskApi, 'fetchEnforcementIncidents'],
    ] as const

    for (const [source, name] of protectedApis) {
      const functionSource = exportedFunction(source, name)
      assert.match(functionSource, /signal\?: AbortSignal/)
      assert.match(
        functionSource,
        /\.\.\.CALLER_HANDLED_REQUEST_CONFIG,\s*signal/
      )
    }
  })

  it('keeps the page-range correction callback referentially stable', () => {
    const source = readSource('../../hooks/use-table-url-state.ts')

    assert.match(source, /const ensurePageInRange = useCallback\(/)
    assert.match(source, /\[navigate, resolvedPaginationCfg, search\]/)
  })
})
