import { useEffect, useMemo, useState } from 'react'
import { getRouteApi } from '@tanstack/react-router'
import {
  Copy,
  Download,
  Eraser,
  History,
  MoreHorizontal,
  Plus,
  RefreshCw,
  Search,
  Trash2,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getRequestErrorMessage } from '@/lib/api'
import { cn } from '@/lib/utils'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  ServerPagination,
  useLatestRequest,
  useServerPagination,
} from '@/components/data-table'
import { DateTimePicker } from '@/components/datetime-picker'
import {
  createInvitationCode,
  deleteInvalidInvitationCodes,
  deleteInvitationCode,
  fetchInvitationCode,
  fetchInvitationCodeUsages,
  fetchInvitationCodes,
  updateInvitationCode,
} from './api'
import {
  INVITATION_CODE_STATUS,
  type InvitationCode,
  type InvitationCodeSavePayload,
  type InvitationCodeUsage,
} from './types'

const route = getRouteApi('/_authenticated/invitation-codes/')

function formatTimestamp(unixSec: number | undefined | null): string {
  if (!unixSec) return '—'
  return new Date(unixSec * 1000).toLocaleString([], {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}

function isExpired(record: InvitationCode): boolean {
  return (
    record.expired_time !== 0 &&
    record.expired_time < Math.floor(Date.now() / 1000)
  )
}

function isExhausted(record: InvitationCode): boolean {
  return record.max_uses > 0 && record.used_count >= record.max_uses
}

function StatusBadge({ record }: { record: InvitationCode }) {
  const { t } = useTranslation()
  if (record.status === INVITATION_CODE_STATUS.DISABLED) {
    return (
      <Badge
        variant='outline'
        className='border-rose-500/40 bg-rose-500/10 text-rose-600 dark:text-rose-400'
      >
        {t('Disabled')}
      </Badge>
    )
  }
  if (isExpired(record)) {
    return (
      <Badge
        variant='outline'
        className='border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400'
      >
        {t('Expired')}
      </Badge>
    )
  }
  if (isExhausted(record)) {
    return (
      <Badge
        variant='outline'
        className='border-violet-500/40 bg-violet-500/10 text-violet-600 dark:text-violet-400'
      >
        {t('Exhausted')}
      </Badge>
    )
  }
  return (
    <Badge
      variant='outline'
      className='border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
    >
      {t('Active')}
    </Badge>
  )
}

// ============================================================================
// Edit drawer
// ============================================================================

type EditFormValues = {
  name: string
  count: number
  max_uses: number
  owner_user_id: number
  expired_time?: Date
}

const EMPTY_FORM: EditFormValues = {
  name: '',
  count: 1,
  max_uses: 1,
  owner_user_id: 0,
  expired_time: undefined,
}

type EditDrawerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  editingId: number | null
  onSaved: () => void
}

function EditInvitationCodeDrawer({
  open,
  onOpenChange,
  editingId,
  onSaved,
}: EditDrawerProps) {
  const { t } = useTranslation()
  const isEdit = editingId !== null
  const [values, setValues] = useState<EditFormValues>(EMPTY_FORM)
  const [loading, setLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [createdCodes, setCreatedCodes] = useState<string[] | null>(null)
  const [editingStatus, setEditingStatus] = useState<
    | typeof INVITATION_CODE_STATUS.ENABLED
    | typeof INVITATION_CODE_STATUS.DISABLED
  >(INVITATION_CODE_STATUS.ENABLED)

  // Reset / load when opening
  useEffect(() => {
    if (!open) return
    if (isEdit && editingId !== null) {
      setLoading(true)
      fetchInvitationCode(editingId)
        .then((data) => {
          setValues({
            name: data.name || '',
            count: 1,
            max_uses: data.max_uses ?? 0,
            owner_user_id: data.owner_user_id ?? 0,
            expired_time:
              data.expired_time && data.expired_time !== 0
                ? new Date(data.expired_time * 1000)
                : undefined,
          })
          setEditingStatus(data.status)
        })
        .catch((e) =>
          toast.error(
            getRequestErrorMessage(e, t('Failed to load invitation code'))
          )
        )
        .finally(() => setLoading(false))
    } else {
      setValues(EMPTY_FORM)
      setEditingStatus(INVITATION_CODE_STATUS.ENABLED)
    }
  }, [open, editingId, isEdit, t])

  const handleSubmit = async () => {
    if (!values.name.trim()) {
      toast.error(t('Please enter a name'))
      return
    }
    setSubmitting(true)
    try {
      const payload: InvitationCodeSavePayload = {
        name: values.name.trim(),
        max_uses: Math.max(0, Math.floor(values.max_uses) || 0),
        owner_user_id: Math.max(0, Math.floor(values.owner_user_id) || 0),
        expired_time: values.expired_time
          ? Math.floor(values.expired_time.getTime() / 1000)
          : 0,
      }
      if (isEdit && editingId !== null) {
        payload.id = editingId
        payload.status = editingStatus
        await updateInvitationCode(payload)
        toast.success(t('Invitation code updated'))
        onSaved()
        onOpenChange(false)
      } else {
        payload.count = Math.max(1, Math.floor(values.count) || 1)
        const created = await createInvitationCode(payload)
        toast.success(t('Invitation code created'))
        if (Array.isArray(created) && created.length > 0) {
          setCreatedCodes(created)
        } else {
          onSaved()
          onOpenChange(false)
        }
      }
    } catch (e) {
      toast.error(getRequestErrorMessage(e, t('Save failed')))
    } finally {
      setSubmitting(false)
    }
  }

  const handleDownloadCreated = () => {
    if (!createdCodes || createdCodes.length === 0) return
    const text = createdCodes.join('\n')
    const blob = new Blob([text], { type: 'text/plain;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${values.name || 'invitation-codes'}.txt`
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
  }

  return (
    <>
      <Sheet open={open} onOpenChange={onOpenChange}>
        <SheetContent className='flex w-full flex-col gap-0 p-0 sm:max-w-md'>
          <SheetHeader className='border-b px-4 py-3 sm:px-6 sm:py-4'>
            <SheetTitle>
              {isEdit
                ? t('Update invitation code')
                : t('Create invitation code')}
            </SheetTitle>
            <SheetDescription>
              {isEdit
                ? t('Adjust name, expiry, max uses and owner')
                : t('Generate one or more invitation codes')}
            </SheetDescription>
          </SheetHeader>

          <div className='flex-1 space-y-5 overflow-y-auto p-4 sm:p-6'>
            {loading ? (
              <Skeleton className='h-40 w-full' />
            ) : (
              <>
                <div className='space-y-1.5'>
                  <Label>{t('Name')}</Label>
                  <Input
                    value={values.name}
                    onChange={(e) =>
                      setValues((v) => ({ ...v, name: e.target.value }))
                    }
                    placeholder={t('Enter name')}
                  />
                </div>

                <div className='space-y-1.5'>
                  <Label>{t('Expiry time')}</Label>
                  <DateTimePicker
                    value={values.expired_time}
                    onChange={(d) =>
                      setValues((v) => ({ ...v, expired_time: d }))
                    }
                    placeholder={t('Select expiry (leave empty for never)')}
                  />
                </div>

                <div className='grid gap-3 sm:grid-cols-2'>
                  <div className='space-y-1.5'>
                    <Label>{t('Max uses')}</Label>
                    <Input
                      type='number'
                      min={0}
                      value={values.max_uses}
                      onChange={(e) =>
                        setValues((v) => ({
                          ...v,
                          max_uses: Number(e.target.value) || 0,
                        }))
                      }
                      placeholder={t('0 = unlimited')}
                    />
                  </div>
                  <div className='space-y-1.5'>
                    <Label>{t('Owner user ID')}</Label>
                    <Input
                      type='number'
                      min={0}
                      value={values.owner_user_id}
                      onChange={(e) =>
                        setValues((v) => ({
                          ...v,
                          owner_user_id: Number(e.target.value) || 0,
                        }))
                      }
                      placeholder={t('0 = no owner')}
                    />
                  </div>
                  {!isEdit && (
                    <div className='space-y-1.5'>
                      <Label>{t('Generate count')}</Label>
                      <Input
                        type='number'
                        min={1}
                        value={values.count}
                        onChange={(e) =>
                          setValues((v) => ({
                            ...v,
                            count: Number(e.target.value) || 1,
                          }))
                        }
                      />
                    </div>
                  )}
                </div>
              </>
            )}
          </div>

          <SheetFooter className='border-t px-4 py-3 sm:px-6 sm:py-4'>
            <Button
              variant='outline'
              onClick={() => onOpenChange(false)}
              disabled={submitting}
            >
              {t('Cancel')}
            </Button>
            <Button onClick={handleSubmit} disabled={submitting || loading}>
              {t('Submit')}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      <Dialog
        open={createdCodes !== null}
        onOpenChange={(o) => {
          if (!o) {
            setCreatedCodes(null)
            onSaved()
            onOpenChange(false)
          }
        }}
      >
        <DialogContent className='sm:max-w-md'>
          <DialogHeader>
            <DialogTitle>{t('Invitation codes created')}</DialogTitle>
            <DialogDescription>
              {t('Generated {{n}} codes. Download them as a text file?', {
                n: createdCodes?.length ?? 0,
              })}
            </DialogDescription>
          </DialogHeader>
          {createdCodes && createdCodes.length > 0 && (
            <div className='bg-muted max-h-48 overflow-y-auto rounded-md p-3 font-mono text-xs'>
              {createdCodes.map((code) => (
                <div key={code}>{code}</div>
              ))}
            </div>
          )}
          <DialogFooter>
            <Button
              variant='outline'
              onClick={() => {
                setCreatedCodes(null)
                onSaved()
                onOpenChange(false)
              }}
            >
              {t('Close')}
            </Button>
            <Button onClick={handleDownloadCreated}>
              <Download className='mr-1.5 h-3.5 w-3.5' />
              {t('Download')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

// ============================================================================
// Usages dialog
// ============================================================================

type UsagesDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  record: InvitationCode | null
}

function UsagesDialog({ open, onOpenChange, record }: UsagesDialogProps) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [usages, setUsages] = useState<InvitationCodeUsage[]>([])

  useEffect(() => {
    if (!open || !record) return
    // async fetch 之前的合法同步 loading
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setLoading(true)
    fetchInvitationCodeUsages(record.id)
      .then((data) => setUsages(data))
      .catch((e) =>
        toast.error(getRequestErrorMessage(e, t('Failed to load usages')))
      )
      .finally(() => setLoading(false))
  }, [open, record, t])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{t('Usage history')}</DialogTitle>
          <DialogDescription>
            {record ? t('Code: {{code}}', { code: record.code }) : ''}
          </DialogDescription>
        </DialogHeader>
        <div className='max-h-[60vh] overflow-y-auto'>
          {loading ? (
            <Skeleton className='h-32 w-full' />
          ) : usages.length === 0 ? (
            <div className='text-muted-foreground py-8 text-center text-sm'>
              {t('No usage records')}
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className='w-24'>{t('User ID')}</TableHead>
                  <TableHead>{t('Username')}</TableHead>
                  <TableHead className='w-44'>{t('Used at')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {usages.map((u) => (
                  <TableRow key={u.id}>
                    <TableCell className='font-mono'>{u.user_id}</TableCell>
                    <TableCell>{u.username || '—'}</TableCell>
                    <TableCell className='text-muted-foreground text-xs'>
                      {formatTimestamp(u.used_time)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}

// ============================================================================
// Main page
// ============================================================================

export function InvitationCodes() {
  const { t } = useTranslation()
  const { copyToClipboard } = useCopyToClipboard({ notify: false })
  const [items, setItems] = useState<InvitationCode[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const pagination = useServerPagination({
    search,
    navigate,
    total,
    defaultPageSize: 10,
  })
  const { page, pageSize } = pagination
  const runListRequest = useLatestRequest()
  const appliedKeyword = search.filter ?? ''
  const [keyword, setKeyword] = useState(appliedKeyword)

  const [editOpen, setEditOpen] = useState(false)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [usagesOpen, setUsagesOpen] = useState(false)
  const [usagesRecord, setUsagesRecord] = useState<InvitationCode | null>(null)
  const [pendingDelete, setPendingDelete] = useState<InvitationCode | null>(
    null
  )
  const [pendingClearInvalid, setPendingClearInvalid] = useState(false)

  useEffect(() => setKeyword(appliedKeyword), [appliedKeyword])

  const params = useMemo(
    () => ({ page, pageSize, keyword: appliedKeyword }),
    [page, pageSize, appliedKeyword]
  )

  const refresh = async () => {
    setLoading(true)
    await runListRequest((signal) => fetchInvitationCodes(params, signal), {
      onSuccess: (data) => {
        setItems(data.items || [])
        setTotal(data.total || 0)
      },
      onError: (error) => {
        toast.error(
          getRequestErrorMessage(error, t('Failed to load invitation codes'))
        )
        setItems([])
        setTotal(0)
      },
      onSettled: () => setLoading(false),
    })
  }

  useEffect(() => {
    refresh()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params])

  const applyKeyword = () => {
    const nextKeyword = keyword.trim()
    pagination.resetPage({ filter: nextKeyword || undefined })
  }

  const handleCopy = async (text: string) => {
    if (await copyToClipboard(text)) {
      toast.success(t('Copied to clipboard'))
    } else {
      toast.error(t('Copy failed; please copy manually'))
    }
  }

  const handleToggleStatus = async (record: InvitationCode) => {
    const next =
      record.status === INVITATION_CODE_STATUS.ENABLED
        ? INVITATION_CODE_STATUS.DISABLED
        : INVITATION_CODE_STATUS.ENABLED
    try {
      await updateInvitationCode({
        id: record.id,
        name: record.name,
        max_uses: record.max_uses,
        owner_user_id: record.owner_user_id,
        expired_time: record.expired_time,
        status: next,
      })
      toast.success(t('Operation succeeded'))
      refresh()
    } catch (e) {
      toast.error(getRequestErrorMessage(e, t('Operation failed')))
    }
  }

  const handleDelete = async (record: InvitationCode) => {
    try {
      await deleteInvitationCode(record.id)
      toast.success(t('Deleted'))
      refresh()
    } catch (e) {
      toast.error(getRequestErrorMessage(e, t('Delete failed')))
    } finally {
      setPendingDelete(null)
    }
  }

  const handleClearInvalid = async () => {
    try {
      const count = await deleteInvalidInvitationCodes()
      toast.success(t('Deleted {{count}} invalid codes', { count }))
      refresh()
    } catch (e) {
      toast.error(getRequestErrorMessage(e, t('Operation failed')))
    } finally {
      setPendingClearInvalid(false)
    }
  }

  return (
    <div className='px-4 py-6 sm:px-8'>
      <div className='mx-auto w-full max-w-[1440px] space-y-5'>
        <div className='flex flex-col gap-1'>
          <h1 className='text-xl font-semibold tracking-tight'>
            {t('Invitation codes')}
          </h1>
          <p className='text-muted-foreground text-sm'>
            {t('Manage registration invitation codes and review their usage')}
          </p>
        </div>

        {/* Action bar */}
        <div className='bg-card flex flex-wrap items-center gap-2 rounded-xl border p-3'>
          <Button
            onClick={() => {
              setEditingId(null)
              setEditOpen(true)
            }}
          >
            <Plus className='mr-1.5 h-3.5 w-3.5' />
            {t('Create code')}
          </Button>
          <Button variant='outline' onClick={refresh}>
            <RefreshCw
              className={cn('mr-1.5 h-3.5 w-3.5', loading && 'animate-spin')}
            />
            {t('Refresh')}
          </Button>
          <Button
            variant='outline'
            className='text-destructive hover:text-destructive'
            onClick={() => setPendingClearInvalid(true)}
          >
            <Eraser className='mr-1.5 h-3.5 w-3.5' />
            {t('Clear invalid codes')}
          </Button>
          <div className='ml-auto flex w-full items-center gap-2 sm:w-auto'>
            <Input
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  applyKeyword()
                }
              }}
              placeholder={t('Search name, code, creator or owner')}
              className='sm:w-72'
            />
            <Button
              variant='outline'
              size='sm'
              className='h-9 shrink-0 sm:h-8'
              onClick={applyKeyword}
            >
              <Search className='size-3.5' />
              {t('Search')}
            </Button>
          </div>
        </div>

        {/* Table */}
        <div className='bg-card overflow-x-auto rounded-xl border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className='w-16'>ID</TableHead>
                <TableHead>{t('Name')}</TableHead>
                <TableHead>{t('Code')}</TableHead>
                <TableHead className='w-24'>{t('Status')}</TableHead>
                <TableHead className='w-24'>{t('Uses')}</TableHead>
                <TableHead className='w-20'>{t('Owner')}</TableHead>
                <TableHead className='w-20'>{t('Creator')}</TableHead>
                <TableHead className='w-20'>{t('Source')}</TableHead>
                <TableHead className='w-40'>{t('Created')}</TableHead>
                <TableHead className='w-40'>{t('Expires')}</TableHead>
                <TableHead className='w-44'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading && items.length === 0 ? (
                Array.from({ length: 5 }).map((_, i) => (
                  <TableRow key={i}>
                    {Array.from({ length: 11 }).map((_, j) => (
                      <TableCell key={j}>
                        <Skeleton className='h-4 w-full' />
                      </TableCell>
                    ))}
                  </TableRow>
                ))
              ) : items.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={11}
                    className='text-muted-foreground py-12 text-center text-sm'
                  >
                    {t('No invitation codes')}
                  </TableCell>
                </TableRow>
              ) : (
                items.map((record) => (
                  <TableRow key={record.id}>
                    <TableCell className='font-mono'>{record.id}</TableCell>
                    <TableCell>{record.name || '—'}</TableCell>
                    <TableCell>
                      <Badge variant='outline' className='font-mono'>
                        {record.code}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <StatusBadge record={record} />
                    </TableCell>
                    <TableCell className='font-mono text-xs'>
                      {record.used_count}/
                      {record.max_uses === 0 ? '∞' : record.max_uses}
                    </TableCell>
                    <TableCell className='font-mono'>
                      {record.owner_user_id === 0 ? '—' : record.owner_user_id}
                    </TableCell>
                    <TableCell className='font-mono'>
                      {record.created_by}
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant='outline'
                        className={cn(
                          'font-normal',
                          record.is_admin
                            ? 'border-blue-500/40 bg-blue-500/10 text-blue-600 dark:text-blue-400'
                            : 'border-cyan-500/40 bg-cyan-500/10 text-cyan-600 dark:text-cyan-400'
                        )}
                      >
                        {record.is_admin ? t('Admin') : t('User')}
                      </Badge>
                    </TableCell>
                    <TableCell className='text-muted-foreground text-xs'>
                      {formatTimestamp(record.created_time)}
                    </TableCell>
                    <TableCell className='text-muted-foreground text-xs'>
                      {record.expired_time === 0
                        ? t('Never')
                        : formatTimestamp(record.expired_time)}
                    </TableCell>
                    <TableCell>
                      <div className='flex items-center gap-1'>
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() => handleCopy(record.code)}
                        >
                          <Copy className='mr-1.5 h-3 w-3' />
                          {t('Copy')}
                        </Button>
                        <Button
                          size='sm'
                          variant='ghost'
                          onClick={() => {
                            setUsagesRecord(record)
                            setUsagesOpen(true)
                          }}
                        >
                          <History className='mr-1.5 h-3 w-3' />
                          {t('Usages')}
                        </Button>
                        <DropdownMenu>
                          <DropdownMenuTrigger asChild>
                            <Button
                              size='icon'
                              variant='ghost'
                              className='h-8 w-8'
                            >
                              <MoreHorizontal className='h-3.5 w-3.5' />
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align='end'>
                            <DropdownMenuItem
                              onClick={() => {
                                setEditingId(record.id)
                                setEditOpen(true)
                              }}
                            >
                              {t('Edit')}
                            </DropdownMenuItem>
                            <DropdownMenuItem
                              onClick={() => handleToggleStatus(record)}
                            >
                              {record.status === INVITATION_CODE_STATUS.ENABLED
                                ? t('Disable')
                                : t('Enable')}
                            </DropdownMenuItem>
                            <DropdownMenuItem
                              onClick={() => setPendingDelete(record)}
                              className='text-destructive focus:text-destructive'
                            >
                              <Trash2 className='mr-1.5 h-3 w-3' />
                              {t('Delete')}
                            </DropdownMenuItem>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>

        <ServerPagination
          table={pagination.table}
          total={total}
          loading={loading}
        />
      </div>

      <EditInvitationCodeDrawer
        open={editOpen}
        onOpenChange={setEditOpen}
        editingId={editingId}
        onSaved={refresh}
      />

      <UsagesDialog
        open={usagesOpen}
        onOpenChange={setUsagesOpen}
        record={usagesRecord}
      />

      <AlertDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Delete this invitation code?')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t('This action cannot be undone.')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              className='bg-destructive text-destructive-foreground hover:bg-destructive/90'
              onClick={() => {
                if (pendingDelete) handleDelete(pendingDelete)
              }}
            >
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog
        open={pendingClearInvalid}
        onOpenChange={setPendingClearInvalid}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Clear all invalid invitation codes?')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This will delete codes that are disabled, expired, or exhausted. The action cannot be undone.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              className='bg-destructive text-destructive-foreground hover:bg-destructive/90'
              onClick={handleClearInvalid}
            >
              {t('Clear')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
