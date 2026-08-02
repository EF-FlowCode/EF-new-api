import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Eye, RotateCcw, Save } from 'lucide-react'
import { api } from '@/lib/api'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
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
import { Textarea } from '@/components/ui/textarea'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { SettingsSection } from '../components/settings-section'

// ----------------------------------------------------------------------------
// Types
// ----------------------------------------------------------------------------

type TemplateVariable = {
  name: string
  description?: string
}

type EmailTemplate = {
  key: string
  name: string
  description?: string
  default_subject: string
  default_body: string
  current_subject: string
  current_body: string
  customized: boolean
  variables: TemplateVariable[]
}

// ----------------------------------------------------------------------------
// Component
// ----------------------------------------------------------------------------

export function EmailTemplateSettingsSection() {
  const { t } = useTranslation()

  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [previewing, setPreviewing] = useState(false)
  const [templates, setTemplates] = useState<EmailTemplate[]>([])
  const [activeKey, setActiveKey] = useState('')
  const [drafts, setDrafts] = useState<
    Record<string, { subject: string; body: string }>
  >({})
  const [previewOpen, setPreviewOpen] = useState(false)
  const [resetOpen, setResetOpen] = useState(false)
  const [previewSubject, setPreviewSubject] = useState('')
  const [previewBody, setPreviewBody] = useState('')

  const subjectRef = useRef<HTMLInputElement>(null)
  const bodyRef = useRef<HTMLTextAreaElement>(null)
  const lastFocusedRef = useRef<'subject' | 'body'>('body')

  const activeTemplate = useMemo(
    () => templates.find((tpl) => tpl.key === activeKey),
    [templates, activeKey]
  )
  const currentDraft = drafts[activeKey] || { subject: '', body: '' }

  const fetchTemplates = async () => {
    setLoading(true)
    try {
      const res = await api.get('/api/option/email_templates')
      const { success, message, data } = res.data
      if (!success) {
        toast.error(message || t('Failed to load email templates'))
        return
      }
      const list: EmailTemplate[] = data || []
      setTemplates(list)
      if (list.length > 0) {
        const initial: Record<string, { subject: string; body: string }> = {}
        list.forEach((tpl) => {
          initial[tpl.key] = {
            subject: tpl.current_subject || '',
            body: tpl.current_body || '',
          }
        })
        setDrafts(initial)
        setActiveKey((prev) => prev || list[0].key)
      }
    } catch {
      toast.error(t('Failed to load email templates'))
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchTemplates()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const updateDraft = (patch: { subject?: string; body?: string }) => {
    setDrafts((prev) => ({
      ...prev,
      [activeKey]: { ...(prev[activeKey] || {}), ...patch } as {
        subject: string
        body: string
      },
    }))
  }

  const insertVariable = (name: string) => {
    const token = `{{${name}}}`
    const target = lastFocusedRef.current === 'subject' ? subjectRef : bodyRef
    const el = target.current
    if (el && typeof el.selectionStart === 'number') {
      const start = el.selectionStart ?? 0
      const end = el.selectionEnd ?? start
      const current =
        lastFocusedRef.current === 'subject'
          ? currentDraft.subject
          : currentDraft.body
      const next = (current || '').slice(0, start) + token + (current || '').slice(end)
      updateDraft(
        lastFocusedRef.current === 'subject'
          ? { subject: next }
          : { body: next }
      )
      requestAnimationFrame(() => {
        try {
          el.focus()
          const pos = start + token.length
          el.setSelectionRange(pos, pos)
        } catch {
          // ignore
        }
      })
    } else {
      if (lastFocusedRef.current === 'subject') {
        updateDraft({ subject: (currentDraft.subject || '') + token })
      } else {
        updateDraft({ body: (currentDraft.body || '') + token })
      }
    }
  }

  const handlePreview = async () => {
    if (!activeKey) return
    setPreviewing(true)
    try {
      const res = await api.post('/api/option/email_templates/preview', {
        key: activeKey,
        subject: currentDraft.subject,
        body: currentDraft.body,
      })
      const { success, message, data } = res.data
      if (!success) {
        toast.error(message || t('Preview failed'))
        return
      }
      setPreviewSubject(data?.subject || '')
      setPreviewBody(data?.body || '')
      setPreviewOpen(true)
    } catch {
      toast.error(t('Preview failed'))
    } finally {
      setPreviewing(false)
    }
  }

  const handleSave = async () => {
    if (!activeTemplate) return
    setSaving(true)
    try {
      const subjectKey = `EmailTemplate.${activeKey}.subject`
      const bodyKey = `EmailTemplate.${activeKey}.body`
      // Persist empty string when value matches built-in default — backend then
      // falls back to the default at send time.
      const subjectValue =
        currentDraft.subject === activeTemplate.default_subject
          ? ''
          : currentDraft.subject || ''
      const bodyValue =
        currentDraft.body === activeTemplate.default_body
          ? ''
          : currentDraft.body || ''

      const [r1, r2] = await Promise.all([
        api.put('/api/option/', { key: subjectKey, value: subjectValue }),
        api.put('/api/option/', { key: bodyKey, value: bodyValue }),
      ])
      if (!r1.data.success) {
        toast.error(r1.data.message)
        return
      }
      if (!r2.data.success) {
        toast.error(r2.data.message)
        return
      }
      toast.success(t('Saved'))
      await fetchTemplates()
    } catch {
      toast.error(t('Save failed'))
    } finally {
      setSaving(false)
    }
  }

  const handleReset = async () => {
    if (!activeTemplate) return
    try {
      const res = await api.post('/api/option/email_templates/reset', {
        key: activeKey,
      })
      if (!res.data.success) {
        toast.error(res.data.message)
        return
      }
      toast.success(t('Restored to defaults'))
      setResetOpen(false)
      await fetchTemplates()
    } catch {
      toast.error(t('Operation failed'))
    }
  }

  return (
    <SettingsSection
      title={t('Email Templates')}
      description={t(
        'Customize subject lines and HTML bodies for outgoing system emails. Use {{name}} placeholders that get replaced at send time.'
      )}
    >
      {loading && templates.length === 0 ? (
        <div className='text-muted-foreground py-8 text-center text-sm'>
          {t('Loading…')}
        </div>
      ) : templates.length === 0 ? (
        <Alert>
          <AlertDescription>
            {t('No email templates available')}
          </AlertDescription>
        </Alert>
      ) : (
        <div className='space-y-5'>
          <div className='flex flex-wrap items-center gap-3'>
            <Label className='text-sm font-medium'>{t('Template')}</Label>
            <Select value={activeKey} onValueChange={setActiveKey}>
              <SelectTrigger className='w-72'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {templates.map((tpl) => (
                  <SelectItem key={tpl.key} value={tpl.key}>
                    {tpl.customized
                      ? `${tpl.name} (${t('Customized')})`
                      : tpl.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {activeTemplate?.customized && (
              <Badge variant='outline'>{t('Customized')}</Badge>
            )}
          </div>

          {activeTemplate && (
            <>
              {activeTemplate.description && (
                <Alert>
                  <AlertDescription>
                    {activeTemplate.description}
                  </AlertDescription>
                </Alert>
              )}

              <div className='space-y-2'>
                <div className='flex items-center justify-between'>
                  <Label className='text-sm font-medium'>
                    {t('Available variables')}
                  </Label>
                  <span className='text-muted-foreground text-xs'>
                    {t('Click a tag to insert at the cursor')}
                  </span>
                </div>
                <div className='flex flex-wrap gap-1.5'>
                  <TooltipProvider delayDuration={300}>
                    {(activeTemplate.variables || []).map((v) => (
                      <Tooltip key={v.name}>
                        <TooltipTrigger asChild>
                          <button
                            type='button'
                            onClick={() => insertVariable(v.name)}
                            className='border-primary/30 bg-primary/5 hover:bg-primary/10 text-primary inline-flex items-center rounded-md border px-2 py-0.5 font-mono text-xs transition-colors'
                          >
                            {`{{${v.name}}}`}
                          </button>
                        </TooltipTrigger>
                        {v.description && (
                          <TooltipContent>{v.description}</TooltipContent>
                        )}
                      </Tooltip>
                    ))}
                  </TooltipProvider>
                </div>
              </div>

              <div className='space-y-2'>
                <Label className='text-sm font-medium'>
                  {t('Email subject')}
                </Label>
                <Input
                  ref={subjectRef}
                  value={currentDraft.subject}
                  onChange={(e) => updateDraft({ subject: e.target.value })}
                  onFocus={() => {
                    lastFocusedRef.current = 'subject'
                  }}
                  placeholder={activeTemplate.default_subject}
                />
              </div>

              <div className='space-y-2'>
                <Label className='text-sm font-medium'>
                  {t('Email body (HTML)')}
                </Label>
                <Textarea
                  ref={bodyRef}
                  value={currentDraft.body}
                  onChange={(e) => updateDraft({ body: e.target.value })}
                  onFocus={() => {
                    lastFocusedRef.current = 'body'
                  }}
                  placeholder={activeTemplate.default_body}
                  rows={16}
                  className='font-mono text-xs'
                />
              </div>

              <div className='flex flex-wrap gap-2'>
                <Button type='button' onClick={handleSave} disabled={saving}>
                  <Save className='mr-1.5 h-3.5 w-3.5' />
                  {t('Save template')}
                </Button>
                <Button
                  type='button'
                  variant='outline'
                  onClick={handlePreview}
                  disabled={previewing}
                >
                  <Eye className='mr-1.5 h-3.5 w-3.5' />
                  {t('Preview')}
                </Button>
                <Button
                  type='button'
                  variant='ghost'
                  className='text-destructive hover:text-destructive'
                  onClick={() => setResetOpen(true)}
                >
                  <RotateCcw className='mr-1.5 h-3.5 w-3.5' />
                  {t('Restore defaults')}
                </Button>
              </div>
            </>
          )}
        </div>
      )}

      {/* Preview dialog */}
      <Dialog open={previewOpen} onOpenChange={setPreviewOpen}>
        <DialogContent className='sm:max-w-3xl'>
          <DialogHeader>
            <DialogTitle>{t('Email preview')}</DialogTitle>
          </DialogHeader>
          <div className='space-y-3'>
            <div className='space-y-1'>
              <Label className='text-muted-foreground text-xs font-normal'>
                {t('Subject')}
              </Label>
              <div className='bg-muted rounded-md px-3 py-2 text-sm'>
                {previewSubject || '(empty)'}
              </div>
            </div>
            <div className='space-y-1'>
              <Label className='text-muted-foreground text-xs font-normal'>
                {t('Body preview')}
              </Label>
              <iframe
                title='email-preview'
                srcDoc={previewBody || ''}
                sandbox=''
                className='bg-background h-[520px] w-full rounded-md border'
              />
            </div>
          </div>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant='outline'>{t('Close')}</Button>
            </DialogClose>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Reset confirm dialog */}
      <Dialog open={resetOpen} onOpenChange={setResetOpen}>
        <DialogContent className='sm:max-w-md'>
          <DialogHeader>
            <DialogTitle>{t('Restore defaults?')}</DialogTitle>
          </DialogHeader>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Both customized subject and body will be cleared and the built-in defaults will be used again.'
            )}
          </p>
          <DialogFooter>
            <Button variant='outline' onClick={() => setResetOpen(false)}>
              {t('Cancel')}
            </Button>
            <Button variant='destructive' onClick={handleReset}>
              {t('Restore defaults')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </SettingsSection>
  )
}
