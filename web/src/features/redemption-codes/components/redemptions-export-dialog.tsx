// eflow: 按上游 524455fac —— 兑换码创建完成后弹出，可选把本次生成的码存成文件
// （TXT / Markdown，可选是否带名称与额度）；不勾选直接关闭。
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import {
  buildRedemptionExportContent,
  redemptionExportMimeType,
  type RedemptionExportData,
  type RedemptionExportFormat,
} from '../lib/export'

type RedemptionsExportDialogProps = {
  data: RedemptionExportData
  onClose: () => void
}

export function RedemptionsExportDialog(props: RedemptionsExportDialogProps) {
  const { t } = useTranslation()
  const id = useId()
  const [saveToFile, setSaveToFile] = useState(false)
  const [format, setFormat] = useState<RedemptionExportFormat>('txt')
  const [includeName, setIncludeName] = useState(true)
  const [includeQuota, setIncludeQuota] = useState(true)

  const handleComplete = () => {
    if (saveToFile) {
      const content = buildRedemptionExportContent(props.data, {
        format,
        includeName,
        includeQuota,
        headers: {
          name: t('Name'),
          code: t('Redemption Code'),
          quota: t('Quota'),
        },
      })
      const blob = new Blob([content], {
        type: redemptionExportMimeType(format),
      })
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = `redemption-codes-${Date.now()}.${format}`
      document.body.append(link)
      link.click()
      link.remove()
      URL.revokeObjectURL(url)
    }
    props.onClose()
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
    >
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Redemption codes created')}</DialogTitle>
          <DialogDescription>
            {t('Successfully created {{count}} redemption codes', {
              count: props.data.keys.length,
            })}
          </DialogDescription>
        </DialogHeader>
        <div className='space-y-5'>
          <div className='flex items-center gap-2'>
            <Checkbox
              id={`${id}-save-file`}
              checked={saveToFile}
              onCheckedChange={(value) => setSaveToFile(value === true)}
            />
            <Label htmlFor={`${id}-save-file`}>{t('Save as a file')}</Label>
          </div>
          {saveToFile && (
            <div className='space-y-4 pl-6'>
              <RadioGroup
                value={format}
                onValueChange={(value) => {
                  if (value === 'txt' || value === 'md') setFormat(value)
                }}
                aria-label={t('Save redemption codes')}
                className='flex flex-wrap gap-x-5 gap-y-3'
              >
                {[
                  { value: 'txt', label: t('Save as TXT') },
                  { value: 'md', label: t('Save as Markdown') },
                ].map((option) => (
                  <div key={option.value} className='flex items-center gap-2'>
                    <RadioGroupItem
                      value={option.value}
                      id={`${id}-${option.value}`}
                    />
                    <Label htmlFor={`${id}-${option.value}`}>
                      {option.label}
                    </Label>
                  </div>
                ))}
              </RadioGroup>
              <div className='flex flex-wrap gap-6'>
                <div className='flex items-center gap-2'>
                  <Checkbox
                    id={`${id}-name`}
                    checked={includeName}
                    onCheckedChange={(value) => setIncludeName(value === true)}
                  />
                  <Label htmlFor={`${id}-name`}>{t('Include name')}</Label>
                </div>
                <div className='flex items-center gap-2'>
                  <Checkbox
                    id={`${id}-quota`}
                    checked={includeQuota}
                    onCheckedChange={(value) => setIncludeQuota(value === true)}
                  />
                  <Label htmlFor={`${id}-quota`}>{t('Include quota')}</Label>
                </div>
              </div>
            </div>
          )}
        </div>
        <DialogFooter>
          <Button onClick={handleComplete}>{t('Done')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
