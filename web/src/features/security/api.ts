import {
  api,
  CALLER_HANDLED_REQUEST_CONFIG,
  type ApiResult,
  unwrapApiResult,
} from '@/lib/api'
import { securityProofHeaders } from '@/lib/secure-verification'
import type {
  AccessTokenCatalog,
  AccessTokenItem,
  AccessTokenList,
  CreatedAccessToken,
} from './types'

const BASE = '/api/user/access_tokens'

function withProof(proofToken?: string) {
  return {
    ...CALLER_HANDLED_REQUEST_CONFIG,
    headers: securityProofHeaders(proofToken),
  }
}

export async function listAccessTokens(): Promise<AccessTokenList> {
  const res = await api.get<ApiResult<AccessTokenList>>(
    BASE,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return unwrapApiResult(res.data, 'Failed to load access tokens')
}

export async function getAccessTokenCatalog(): Promise<AccessTokenCatalog> {
  const res = await api.get<ApiResult<AccessTokenCatalog>>(
    `${BASE}/catalog`,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return unwrapApiResult(res.data, 'Failed to load access token permissions')
}

/** Creating a token needs an access_token.generate proof for the exact grant. */
export async function createAccessToken(
  body: { name: string; scopes: string[]; expires_at: number },
  proofToken?: string
): Promise<CreatedAccessToken> {
  const res = await api.post<ApiResult<CreatedAccessToken>>(
    BASE,
    body,
    withProof(proofToken)
  )
  return unwrapApiResult(res.data, 'Failed to create access token')
}

/** Renaming needs no proof; passing scopes also replaces the grant and needs one. */
export async function updateAccessToken(
  id: number,
  body: { name: string; scopes?: string[] },
  proofToken?: string
): Promise<AccessTokenItem> {
  const res = await api.patch<ApiResult<AccessTokenItem>>(
    `${BASE}/${id}`,
    body,
    withProof(proofToken)
  )
  return unwrapApiResult(res.data, 'Failed to update access token')
}

export async function deleteAccessToken(
  id: number,
  proofToken?: string
): Promise<void> {
  const res = await api.delete<ApiResult<null>>(
    `${BASE}/${id}`,
    withProof(proofToken)
  )
  unwrapApiResult(res.data, 'Failed to revoke access token')
}

export async function revokeLegacyAccessToken(
  proofToken?: string
): Promise<void> {
  const res = await api.delete<ApiResult<null>>(
    `${BASE}/legacy`,
    withProof(proofToken)
  )
  unwrapApiResult(res.data, 'Failed to revoke access token')
}
