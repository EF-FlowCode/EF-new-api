import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'
import { useTranslation } from 'react-i18next'
import { api } from '@/lib/api'

// ----------------------------------------------------------------------------
// Types
// ----------------------------------------------------------------------------

export type TicketAttachmentInfo = {
  id: number
  file_name: string
  mime_type: string
  size: number
  sha256?: string
  previewable?: boolean
  created_time?: number
  // Local-only flag used while a file is uploading.
  uploading?: boolean
}

export type TicketAttachmentConfig = {
  enabled: boolean
  maxSize: number
  maxCount: number
  exts: string[]
  accept: string
}

// ----------------------------------------------------------------------------
// Helpers preserve the established attachment validation rules.
// ----------------------------------------------------------------------------

const DEFAULT_EXTS =
  'jpg,jpeg,png,gif,webp,bmp,json,xml,txt,log,md,csv,pdf'.split(',')
const DEFAULT_MAX_SIZE = 50 * 1024 * 1024
const DEFAULT_MAX_COUNT = 5

const MIME_TO_EXT: Record<string, string> = {
  'image/png': 'png',
  'image/jpeg': 'jpg',
  'image/jpg': 'jpg',
  'image/gif': 'gif',
  'image/webp': 'webp',
  'image/bmp': 'bmp',
  'application/pdf': 'pdf',
  'application/json': 'json',
  'application/xml': 'xml',
  'text/plain': 'txt',
  'text/markdown': 'md',
  'text/csv': 'csv',
}

function extFromMime(mime: string | undefined): string {
  if (!mime) return ''
  return MIME_TO_EXT[mime.toLowerCase().split(';')[0].trim()] || ''
}

function ensureFileName(file: File): string {
  const orig = (file.name || '').trim()
  if (orig && orig !== 'image.png' && orig !== 'blob') return orig
  const ext = extFromMime(file.type) || 'bin'
  const ts = new Date().toISOString().replace(/[-:]/g, '').replace(/\..+$/, '')
  return `pasted-${ts}.${ext}`
}

// Cache /api/status response (idempotent across hooks)
let cachedConfig: TicketAttachmentConfig | null = null
let configInflight: Promise<TicketAttachmentConfig> | null = null

async function fetchConfig(): Promise<TicketAttachmentConfig> {
  if (cachedConfig) return cachedConfig
  if (configInflight) return configInflight
  configInflight = (async () => {
    const fallback: TicketAttachmentConfig = {
      enabled: true,
      maxSize: DEFAULT_MAX_SIZE,
      maxCount: DEFAULT_MAX_COUNT,
      exts: DEFAULT_EXTS,
      accept: DEFAULT_EXTS.map((e) => `.${e}`).join(','),
    }
    try {
      const res = await api.get('/api/status')
      const data = res.data?.data || {}
      const enabled =
        data.ticket_attachment_enabled === undefined
          ? true
          : Boolean(data.ticket_attachment_enabled)
      const maxSize = Number(data.ticket_attachment_max_size) || DEFAULT_MAX_SIZE
      const maxCount =
        Number(data.ticket_attachment_max_count) || DEFAULT_MAX_COUNT
      const extsRaw =
        typeof data.ticket_attachment_allowed_exts === 'string'
          ? data.ticket_attachment_allowed_exts
          : DEFAULT_EXTS.join(',')
      const exts = extsRaw
        .split(',')
        .map((s: string) => s.trim().toLowerCase())
        .filter(Boolean)
      cachedConfig = {
        enabled,
        maxSize,
        maxCount,
        exts,
        accept: exts.map((e: string) => `.${e}`).join(','),
      }
    } catch {
      cachedConfig = fallback
    } finally {
      configInflight = null
    }
    return cachedConfig!
  })()
  return configInflight
}

// ----------------------------------------------------------------------------
// Hook
// ----------------------------------------------------------------------------

export type UseTicketAttachmentsResult = {
  config: TicketAttachmentConfig
  configLoading: boolean
  attachments: TicketAttachmentInfo[]
  uploading: boolean
  /** Open the OS file picker. */
  openFilePicker: () => void
  /** Programmatically add files (e.g. from clipboard paste). */
  addFiles: (files: File[]) => Promise<void>
  /** Remove a single attachment (also deletes from server if it has an id). */
  removeAttachment: (idOrLocalKey: number | string) => Promise<void>
  /** Reset local state (does not delete from server). */
  reset: () => void
  /** Discard all pending attachments and free server storage. */
  discardAll: () => Promise<void>
  /** Paste handler attachable to onPasteCapture. */
  handlePaste: (event: React.ClipboardEvent) => void
  /** Hidden file input ref for the picker. */
  fileInputRef: React.RefObject<HTMLInputElement | null>
  /** Native onChange for the hidden file input. */
  onFileInputChange: (event: React.ChangeEvent<HTMLInputElement>) => void
}

const PLACEHOLDER_FALLBACK_CONFIG: TicketAttachmentConfig = {
  enabled: true,
  maxSize: DEFAULT_MAX_SIZE,
  maxCount: DEFAULT_MAX_COUNT,
  exts: DEFAULT_EXTS,
  accept: DEFAULT_EXTS.map((e) => `.${e}`).join(','),
}

export function useTicketAttachments(): UseTicketAttachmentsResult {
  const { t } = useTranslation()
  const [config, setConfig] = useState<TicketAttachmentConfig>(
    cachedConfig ?? PLACEHOLDER_FALLBACK_CONFIG
  )
  const [configLoading, setConfigLoading] = useState<boolean>(
    cachedConfig === null
  )
  const [attachments, setAttachments] = useState<TicketAttachmentInfo[]>([])
  const [uploading, setUploading] = useState(false)
  const fileInputRef = useRef<HTMLInputElement | null>(null)

  // Load attachment limits once.
  useEffect(() => {
    let active = true
    if (cachedConfig !== null) return
    fetchConfig().then((cfg) => {
      if (!active) return
      setConfig(cfg)
      setConfigLoading(false)
    })
    return () => {
      active = false
    }
  }, [])

  const validateFile = useCallback(
    (file: File, currentCount: number): string | null => {
      if (!config.enabled) return t('Attachment uploads are disabled')
      if (currentCount >= config.maxCount) {
        return t('At most {{n}} attachments allowed', { n: config.maxCount })
      }
      if (file.size > config.maxSize) {
        return t('Each attachment must not exceed {{mb}} MB', {
          mb: Math.floor(config.maxSize / 1024 / 1024),
        })
      }
      const fileName = file.name || ''
      const dot = fileName.lastIndexOf('.')
      const ext = dot >= 0 ? fileName.slice(dot + 1).toLowerCase() : ''
      if (ext === 'svg') return t('SVG attachments are disabled for safety')
      if (config.exts.length && !config.exts.includes(ext)) {
        return t('Unsupported attachment type')
      }
      return null
    },
    [config, t]
  )

  const addFiles = useCallback(
    async (incoming: File[]) => {
      if (incoming.length === 0) return
      const accepted: File[] = []
      let count = attachments.length
      for (const f of incoming) {
        const err = validateFile(f, count)
        if (err) {
          toast.warning(err)
          continue
        }
        accepted.push(f)
        count++
      }
      if (accepted.length === 0) return

      setUploading(true)
      try {
        for (const file of accepted) {
          const form = new FormData()
          form.append('file', file)
          try {
            const res = await api.post('/api/ticket/attachment', form, {
              headers: { 'Content-Type': 'multipart/form-data' },
            })
            if (!res.data?.success) {
              toast.error(res.data?.message || t('Attachment upload failed'))
              continue
            }
            const data: TicketAttachmentInfo = res.data.data
            setAttachments((prev) => [...prev, data])
          } catch {
            toast.error(t('Attachment upload failed'))
          }
        }
      } finally {
        setUploading(false)
      }
    },
    [attachments.length, validateFile, t]
  )

  const removeAttachment = useCallback(
    async (id: number | string) => {
      const numericId = typeof id === 'number' ? id : Number(id)
      const target = attachments.find((a) => a.id === numericId)
      if (target?.id) {
        try {
          await api.delete(`/api/ticket/attachment/${target.id}`)
        } catch {
          /* ignore — backend cleanup task will reap orphans */
        }
      }
      setAttachments((prev) => prev.filter((a) => a.id !== numericId))
    },
    [attachments]
  )

  const reset = useCallback(() => {
    setAttachments([])
  }, [])

  const discardAll = useCallback(async () => {
    const ids = attachments.map((a) => a.id).filter(Boolean)
    setAttachments([])
    for (const id of ids) {
      try {
        await api.delete(`/api/ticket/attachment/${id}`)
      } catch {
        /* ignore */
      }
    }
  }, [attachments])

  const handlePaste = useCallback(
    (event: React.ClipboardEvent) => {
      if (!config.enabled) return
      const cb = event.clipboardData
      if (!cb) return
      const files: File[] = []
      for (let i = 0; i < cb.items.length; i++) {
        const item = cb.items[i]
        if (item.kind !== 'file') continue
        const f = item.getAsFile()
        if (!f) continue
        const name = ensureFileName(f)
        try {
          files.push(
            new File([f], name, { type: f.type, lastModified: f.lastModified })
          )
        } catch {
          files.push(f)
        }
      }
      if (files.length === 0) return
      event.preventDefault()
      addFiles(files)
    },
    [addFiles, config.enabled]
  )

  const openFilePicker = useCallback(() => {
    fileInputRef.current?.click()
  }, [])

  const onFileInputChange = useCallback(
    (event: React.ChangeEvent<HTMLInputElement>) => {
      const files = event.target.files
      if (!files || files.length === 0) return
      addFiles(Array.from(files))
      // Reset so that picking the same file twice still triggers onChange.
      event.target.value = ''
    },
    [addFiles]
  )

  return useMemo(
    () => ({
      config,
      configLoading,
      attachments,
      uploading,
      openFilePicker,
      addFiles,
      removeAttachment,
      reset,
      discardAll,
      handlePaste,
      fileInputRef,
      onFileInputChange,
    }),
    [
      config,
      configLoading,
      attachments,
      uploading,
      openFilePicker,
      addFiles,
      removeAttachment,
      reset,
      discardAll,
      handlePaste,
      onFileInputChange,
    ]
  )
}
