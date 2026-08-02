// eflow: 导出对话框 —— 点「导出」后弹出，选报账时间段再导。
// 时间段在这里独立选（含本月/上月/近 7·30 天快捷），其余筛选（模型/分组/Token/类型）
// 仍沿用筛选栏当前选中值。
import { useEffect, useState } from 'react'
import { Loader2, Mail } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import dayjs from '@/lib/dayjs'
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
import { useAuthStore } from '@/stores/auth-store'
import { CompactDateTimeRangePicker } from '../compact-date-time-range-picker'

interface ExportLogsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  defaultStart?: Date
  defaultEnd?: Date
  exporting: boolean
  onConfirm: (start: Date, end: Date) => void
  // eflow: 离线导出（后台异步发邮件）。可选——传了才显示「离线导出」按钮。
  offlineExporting?: boolean
  onConfirmOffline?: (start: Date, end: Date, email: string) => void
}

export function ExportLogsDialog({
  open,
  onOpenChange,
  defaultStart,
  defaultEnd,
  exporting,
  onConfirm,
  offlineExporting,
  onConfirmOffline,
}: ExportLogsDialogProps) {
  const { t } = useTranslation()
  const { auth } = useAuthStore()
  const [start, setStart] = useState<Date | undefined>(defaultStart)
  const [end, setEnd] = useState<Date | undefined>(defaultEnd)
  // eflow: 离线导出收件邮箱，默认预填绑定邮箱；不填则后端回落到绑定邮箱。
  const [email, setEmail] = useState<string>('')

  useEffect(() => {
    // 打开对话框时把时间段重置成外部默认值——合法的 prop→state 同步
    if (open) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setStart(defaultStart)
      setEnd(defaultEnd)
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setEmail(auth.user?.email || '')
    }
  }, [open, defaultStart, defaultEnd, auth.user?.email])

  const setMonth = (offset: number) => {
    const base = dayjs().add(offset, 'month')
    setStart(base.startOf('month').toDate())
    setEnd(base.endOf('month').toDate())
  }
  const setRecentDays = (days: number) => {
    setEnd(dayjs().toDate())
    setStart(dayjs().subtract(days, 'day').startOf('day').toDate())
  }

  const canConfirm = !!start && !!end && start.getTime() <= end.getTime()

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Export usage logs')}</DialogTitle>
          <DialogDescription>
            {t(
              'Select a time range to export (for reimbursement). Other filters in the bar still apply.'
            )}
          </DialogDescription>
        </DialogHeader>
        <div className='space-y-3'>
          <div className='flex flex-wrap gap-1.5'>
            <Button
              type='button'
              variant='outline'
              size='sm'
              className='h-7'
              onClick={() => setMonth(0)}
            >
              {t('Current month')}
            </Button>
            <Button
              type='button'
              variant='outline'
              size='sm'
              className='h-7'
              onClick={() => setMonth(-1)}
            >
              {t('Previous month')}
            </Button>
            <Button
              type='button'
              variant='outline'
              size='sm'
              className='h-7'
              onClick={() => setRecentDays(7)}
            >
              {t('7 Days')}
            </Button>
            <Button
              type='button'
              variant='outline'
              size='sm'
              className='h-7'
              onClick={() => setRecentDays(30)}
            >
              {t('30 Days')}
            </Button>
          </div>
          <CompactDateTimeRangePicker
            start={start}
            end={end}
            onChange={({ start: s, end: e }) => {
              setStart(s)
              setEnd(e)
            }}
          />
          {onConfirmOffline && (
            <div className='space-y-1.5 rounded-md border border-border/60 bg-muted/20 p-2.5'>
              <div className='flex items-center gap-1.5 text-xs text-muted-foreground'>
                <Mail className='size-3.5' />
                {t('Offline export sends a ZIP to your email in the background')}
              </div>
              <Input
                type='email'
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder={t('Leave blank to use your bound email')}
                className='h-9'
              />
            </div>
          )}
        </div>
        <DialogFooter>
          <Button
            variant='outline'
            onClick={() => onOpenChange(false)}
            disabled={exporting || offlineExporting}
          >
            {t('Cancel')}
          </Button>
          {onConfirmOffline && (
            <Button
              variant='secondary'
              onClick={() => {
                if (start && end) onConfirmOffline(start, end, email.trim())
              }}
              disabled={!canConfirm || exporting || offlineExporting}
            >
              {offlineExporting ? (
                <Loader2 className='size-3.5 animate-spin' />
              ) : (
                <Mail className='size-3.5' />
              )}
              {t('Offline export to email')}
            </Button>
          )}
          <Button
            onClick={() => {
              if (start && end) onConfirm(start, end)
            }}
            disabled={!canConfirm || exporting || offlineExporting}
          >
            {exporting && <Loader2 className='size-3.5 animate-spin' />}
            {t('Export')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
