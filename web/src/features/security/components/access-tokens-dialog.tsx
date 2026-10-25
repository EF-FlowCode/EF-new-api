import { useCallback, useEffect, useState } from 'react'
import { KeyRound, Loader2, Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getRequestErrorMessage } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { CopyButton } from '@/components/copy-button'
import {
  SecureVerificationDialog,
  useSecureVerification,
  type SecurityProof,
} from '@/features/auth/secure-verification'
import {
  createAccessToken,
  deleteAccessToken,
  getAccessTokenCatalog,
  listAccessTokens,
  revokeLegacyAccessToken,
  updateAccessToken,
} from '../api'
import type {
  AccessTokenCatalog,
  AccessTokenItem,
  AccessTokenList,
} from '../types'
import { PermissionPicker } from './permission-picker'

const DAY = 24 * 60 * 60
const EXPIRY_OPTIONS = [
  { value: '7', labelKey: '7 days' },
  { value: '30', labelKey: '30 days' },
  { value: '90', labelKey: '90 days' },
  { value: '365', labelKey: '1 year' },
  { value: 'never', labelKey: 'Never expires' },
] as const

function formatTime(unix?: number | null) {
  if (!unix) return '—'
  return new Date(unix * 1000).toLocaleString([], { hour12: false })
}

function sameScopes(a: string[], b: string[]) {
  return a.length === b.length && [...a].sort().join() === [...b].sort().join()
}

type Editor =
  | { mode: 'create'; name: string; expiry: string; scopes: string[] }
  | { mode: 'edit'; token: AccessTokenItem; name: string; scopes: string[] }

type RevokeTarget =
  | { kind: 'token'; token: AccessTokenItem }
  | { kind: 'legacy' }

interface AccessTokensDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

/**
 * Lists, creates, edits and revokes scoped access tokens. Creating, changing
 * permissions and revoking each need a single-use proof for that exact change.
 */
export function AccessTokensDialog({
  open,
  onOpenChange,
}: AccessTokensDialogProps) {
  const { t } = useTranslation()
  const verification = useSecureVerification()
  const [list, setList] = useState<AccessTokenList | null>(null)
  const [catalog, setCatalog] = useState<AccessTokenCatalog | null>(null)
  const [editor, setEditor] = useState<Editor | null>(null)
  const [created, setCreated] = useState<string | null>(null)
  const [revokeTarget, setRevokeTarget] = useState<RevokeTarget | null>(null)
  const [busy, setBusy] = useState(false)

  const refresh = useCallback(async () => {
    try {
      const [tokens, scopes] = await Promise.all([
        listAccessTokens(),
        getAccessTokenCatalog(),
      ])
      setList(tokens)
      setCatalog(scopes)
    } catch (error) {
      toast.error(
        getRequestErrorMessage(error, t('Failed to load access tokens'))
      )
    }
  }, [t])

  useEffect(() => {
    if (open) void refresh()
  }, [open, refresh])

  const close = (next: boolean) => {
    if (busy) return
    if (!next) {
      setEditor(null)
      setCreated(null)
    }
    onOpenChange(next)
  }

  const runVerified = async (
    operation: { scope: string; context: Record<string, unknown> },
    call: (proof?: SecurityProof) => Promise<void>,
    description: string
  ) => {
    setBusy(true)
    try {
      await verification.startVerification(call, { operation, description })
    } catch (error) {
      toast.error(getRequestErrorMessage(error, t('Operation failed')))
    } finally {
      setBusy(false)
    }
  }

  const submitEditor = async () => {
    if (!editor) return
    const name = editor.name.trim()
    if (!name || name.length > 64) {
      toast.error(t('Access token name must be 1 to 64 characters.'))
      return
    }
    if (editor.scopes.length === 0) {
      toast.error(t('Select at least one valid permission.'))
      return
    }
    const scopes = [...new Set(editor.scopes)].sort()

    if (editor.mode === 'create') {
      const expiresAt =
        editor.expiry === 'never'
          ? 0
          : Math.floor(Date.now() / 1000) + Number(editor.expiry) * DAY
      await runVerified(
        {
          scope: 'access_token.generate',
          context: { scopes, expires_at: expiresAt },
        },
        async (proof) => {
          const result = await createAccessToken(
            { name, scopes, expires_at: expiresAt },
            proof?.proof_token
          )
          setCreated(result.token)
          setEditor(null)
          await refresh()
        },
        t('Confirm your identity before creating an access token.')
      )
      return
    }

    const token = editor.token
    if (sameScopes(scopes, token.scopes)) {
      setBusy(true)
      try {
        await updateAccessToken(token.id, { name })
        toast.success(t('Access token updated'))
        setEditor(null)
        await refresh()
      } catch (error) {
        toast.error(
          getRequestErrorMessage(error, t('Failed to update access token'))
        )
      } finally {
        setBusy(false)
      }
      return
    }
    await runVerified(
      { scope: 'access_token.update', context: { token_id: token.id, scopes } },
      async (proof) => {
        await updateAccessToken(token.id, { name, scopes }, proof?.proof_token)
        toast.success(t('Access token updated'))
        setEditor(null)
        await refresh()
      },
      t('Confirm your identity before changing this token’s permissions.')
    )
  }

  const confirmRevoke = async () => {
    const target = revokeTarget
    setRevokeTarget(null)
    if (!target) return
    await runVerified(
      target.kind === 'legacy'
        ? { scope: 'access_token.revoke', context: { legacy: true } }
        : {
            scope: 'access_token.revoke',
            context: { token_id: target.token.id },
          },
      async (proof) => {
        if (target.kind === 'legacy') {
          await revokeLegacyAccessToken(proof?.proof_token)
        } else {
          await deleteAccessToken(target.token.id, proof?.proof_token)
        }
        toast.success(t('Access token revoked'))
        await refresh()
      },
      t('Confirm your identity before revoking this access token.')
    )
  }

  const now = Math.floor(Date.now() / 1000)
  const atLimit = !!catalog && !!list && list.items.length >= catalog.max_tokens

  return (
    <>
      <Dialog open={open} onOpenChange={close}>
        <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
          <DialogHeader>
            <DialogTitle className='flex items-center gap-2'>
              <KeyRound className='h-5 w-5' />
              {t('Access tokens')}
            </DialogTitle>
            <DialogDescription>
              {t(
                'Access tokens let scripts and tools call the console API with only the permissions you grant. Send them as "Authorization: Bearer nap_…".'
              )}
            </DialogDescription>
          </DialogHeader>

          {created && (
            <div className='space-y-2 rounded-lg border border-emerald-500/40 bg-emerald-500/5 p-3'>
              <Label>{t('New access token')}</Label>
              <div className='flex gap-2'>
                <Input value={created} readOnly className='font-mono text-xs' />
                <CopyButton
                  value={created}
                  variant='outline'
                  className='size-9'
                  iconClassName='size-4'
                  tooltip={t('Copy token')}
                  aria-label={t('Copy token')}
                />
              </div>
              <p className='text-muted-foreground text-xs'>
                {t(
                  "Save this token now. You won't be able to view it again after closing this dialog."
                )}
              </p>
            </div>
          )}

          {editor ? (
            <div className='space-y-4'>
              <div className='space-y-2'>
                <Label htmlFor='access-token-name'>{t('Name')}</Label>
                <Input
                  id='access-token-name'
                  value={editor.name}
                  maxLength={64}
                  disabled={busy}
                  onChange={(e) =>
                    setEditor({ ...editor, name: e.target.value })
                  }
                  placeholder={t('e.g. admin-api')}
                />
              </div>
              {editor.mode === 'create' && (
                <div className='space-y-2'>
                  <Label>{t('Expires')}</Label>
                  <Select
                    value={editor.expiry}
                    disabled={busy}
                    onValueChange={(expiry) => setEditor({ ...editor, expiry })}
                  >
                    <SelectTrigger className='w-full sm:w-56'>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {EXPIRY_OPTIONS.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {t(option.labelKey)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  {editor.expiry === 'never' && (
                    <p className='text-xs text-amber-600 dark:text-amber-400'>
                      {t(
                        'A token that never expires keeps working until you revoke it. Use it only for trusted automation.'
                      )}
                    </p>
                  )}
                </div>
              )}
              <div className='space-y-2'>
                <Label>{t('Permissions')}</Label>
                {catalog ? (
                  <PermissionPicker
                    groups={catalog.groups}
                    value={editor.scopes}
                    disabled={busy}
                    onChange={(scopes) => setEditor({ ...editor, scopes })}
                  />
                ) : (
                  <Skeleton className='h-40 w-full' />
                )}
              </div>
            </div>
          ) : (
            <div className='space-y-3'>
              {list?.legacy && (
                <div className='flex flex-col gap-2 rounded-lg border border-amber-500/40 bg-amber-500/5 p-3 sm:flex-row sm:items-center sm:justify-between'>
                  <div className='space-y-1 text-sm'>
                    <div className='font-medium'>
                      {t('Legacy system access token')}{' '}
                      <span className='text-muted-foreground font-mono text-xs'>
                        …{list.legacy.token_hint}
                      </span>
                    </div>
                    <div className='text-muted-foreground text-xs'>
                      {t(
                        'Stops working on {{date}}. Create a scoped token to replace it.',
                        {
                          date: formatTime(list.legacy.retire_at),
                        }
                      )}
                    </div>
                    <div className='text-muted-foreground text-xs'>
                      {t('Last used')}: {formatTime(list.legacy.last_used_at)}{' '}
                      {list.legacy.last_used_ip}
                    </div>
                  </div>
                  <Button
                    type='button'
                    size='sm'
                    variant='outline'
                    disabled={busy}
                    onClick={() => setRevokeTarget({ kind: 'legacy' })}
                  >
                    <Trash2 className='mr-1 h-3.5 w-3.5' />
                    {t('Revoke')}
                  </Button>
                </div>
              )}

              {!list ? (
                <Skeleton className='h-24 w-full' />
              ) : list.items.length === 0 ? (
                <p className='text-muted-foreground rounded-lg border p-6 text-center text-sm'>
                  {t('No access tokens yet')}
                </p>
              ) : (
                <ul className='divide-y rounded-lg border'>
                  {list.items.map((token) => {
                    const expired =
                      token.expires_at > 0 && token.expires_at <= now
                    return (
                      <li
                        key={token.id}
                        className='flex flex-col gap-2 p-3 sm:flex-row sm:items-center sm:justify-between'
                      >
                        <div className='min-w-0 space-y-1 text-sm'>
                          <div className='flex flex-wrap items-center gap-2 font-medium'>
                            {token.name}
                            <span className='text-muted-foreground font-mono text-xs'>
                              nap_…{token.token_hint}
                            </span>
                            {expired && (
                              <Badge
                                variant='outline'
                                className='text-rose-600'
                              >
                                {t('Expired')}
                              </Badge>
                            )}
                          </div>
                          <div className='text-muted-foreground text-xs'>
                            {t('{{count}} permissions', {
                              count: token.scopes.length,
                            })}{' '}
                            ·{' '}
                            {token.expires_at === 0
                              ? t('Never expires')
                              : t('Expires {{date}}', {
                                  date: formatTime(token.expires_at),
                                })}
                          </div>
                          <div className='text-muted-foreground text-xs'>
                            {t('Last used')}: {formatTime(token.last_used_at)}{' '}
                            {token.last_used_ip}
                          </div>
                        </div>
                        <div className='flex shrink-0 gap-2'>
                          <Button
                            type='button'
                            size='sm'
                            variant='outline'
                            disabled={busy}
                            onClick={() =>
                              setEditor({
                                mode: 'edit',
                                token,
                                name: token.name,
                                scopes: token.scopes,
                              })
                            }
                          >
                            <Pencil className='mr-1 h-3.5 w-3.5' />
                            {t('Edit')}
                          </Button>
                          <Button
                            type='button'
                            size='sm'
                            variant='outline'
                            className='text-destructive'
                            disabled={busy}
                            onClick={() =>
                              setRevokeTarget({ kind: 'token', token })
                            }
                          >
                            <Trash2 className='mr-1 h-3.5 w-3.5' />
                            {t('Revoke')}
                          </Button>
                        </div>
                      </li>
                    )
                  })}
                </ul>
              )}
            </div>
          )}

          <DialogFooter>
            {editor ? (
              <>
                <Button
                  type='button'
                  variant='outline'
                  disabled={busy}
                  onClick={() => setEditor(null)}
                >
                  {t('Back')}
                </Button>
                <Button type='button' disabled={busy} onClick={submitEditor}>
                  {busy && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
                  {editor.mode === 'create' ? t('Create') : t('Save')}
                </Button>
              </>
            ) : (
              <>
                <Button
                  type='button'
                  variant='outline'
                  disabled={busy}
                  onClick={() => close(false)}
                >
                  {t('Close')}
                </Button>
                <Button
                  type='button'
                  disabled={busy || atLimit || !catalog}
                  onClick={() => {
                    setCreated(null)
                    setEditor({
                      mode: 'create',
                      name: '',
                      expiry: String(catalog?.default_expiry_days ?? 30),
                      scopes: [],
                    })
                  }}
                >
                  <Plus className='mr-1 h-4 w-4' />
                  {atLimit ? t('Token limit reached') : t('New access token')}
                </Button>
              </>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={revokeTarget !== null}
        onOpenChange={(next) => !next && setRevokeTarget(null)}
        title={t('Revoke access token?')}
        desc={t(
          'Anything using this token stops working immediately. This cannot be undone.'
        )}
        confirmText={t('Revoke')}
        destructive
        handleConfirm={confirmRevoke}
      />
      <SecureVerificationDialog {...verification.dialogProps} />
    </>
  )
}
