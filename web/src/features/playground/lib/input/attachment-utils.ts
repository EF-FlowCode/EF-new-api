import { MAX_ATTACHMENT_TEXT_CHARS } from '../../constants'
import type { MessageAttachment } from '../../types'

export type AttachmentFileInput = {
  filename?: string
  mediaType?: string
  url?: string
}

type AttachmentType = 'image' | 'pdf' | 'docx' | 'text'

const DOCX_MEDIA_TYPE =
  'application/vnd.openxmlformats-officedocument.wordprocessingml.document'

// 纯文本类文件：浏览器常给空 MIME 或各种 text/*，以扩展名为准
const TEXT_EXTENSIONS = [
  'txt',
  'md',
  'markdown',
  'csv',
  'tsv',
  'json',
  'jsonl',
  'xml',
  'yaml',
  'yml',
  'toml',
  'ini',
  'log',
  'html',
  'htm',
  'sql',
  'js',
  'jsx',
  'ts',
  'tsx',
  'py',
  'go',
  'rs',
  'java',
  'kt',
  'swift',
  'c',
  'h',
  'cpp',
  'hpp',
  'cs',
  'php',
  'rb',
  'sh',
  'ps1',
  'css',
  'scss',
  'vue',
]

/** Value for the file input's `accept` attribute. */
export const ATTACHMENT_ACCEPT = [
  'image/*',
  'application/pdf',
  '.pdf',
  DOCX_MEDIA_TYPE,
  '.docx',
  'text/*',
  ...TEXT_EXTENSIONS.map((extension) => `.${extension}`),
].join(',')

function getExtension(filename = ''): string {
  const index = filename.lastIndexOf('.')
  return index === -1 ? '' : filename.slice(index + 1).toLowerCase()
}

export function getAttachmentType(
  file: AttachmentFileInput
): AttachmentType | null {
  const mediaType = file.mediaType?.toLowerCase() ?? ''
  const extension = getExtension(file.filename)

  if (mediaType.startsWith('image/')) return 'image'
  if (mediaType === 'application/pdf' || extension === 'pdf') return 'pdf'
  if (mediaType === DOCX_MEDIA_TYPE || extension === 'docx') return 'docx'
  if (mediaType.startsWith('text/') || TEXT_EXTENSIONS.includes(extension)) {
    return 'text'
  }
  return null
}

/**
 * Whether a picked / pasted / dropped file can be attached.
 */
export function isSupportedAttachmentFile(file: {
  name: string
  type: string
}): boolean {
  return (
    getAttachmentType({ filename: file.name, mediaType: file.type }) !== null
  )
}

function dataUrlToBytes(url: string): Uint8Array {
  const commaIndex = url.indexOf(',')
  const meta = url.slice(0, commaIndex)
  const data = url.slice(commaIndex + 1)
  if (!meta.endsWith(';base64')) {
    return new TextEncoder().encode(decodeURIComponent(data))
  }

  const binary = atob(data)
  const bytes = new Uint8Array(binary.length)
  for (let index = 0; index < binary.length; index++) {
    bytes[index] = binary.charCodeAt(index)
  }
  return bytes
}

function limitText(text: string): string {
  if (text.length <= MAX_ATTACHMENT_TEXT_CHARS) return text
  return `${text.slice(0, MAX_ATTACHMENT_TEXT_CHARS)}\n\n[...]`
}

async function extractDocxText(bytes: Uint8Array): Promise<string> {
  // mammoth 体积较大，只在第一次上传 Word 时加载
  const mammoth = await import('mammoth')
  const arrayBuffer = bytes.buffer.slice(
    bytes.byteOffset,
    bytes.byteOffset + bytes.byteLength
  ) as ArrayBuffer
  const result = await mammoth.extractRawText({ arrayBuffer })
  return result.value
}

export class AttachmentError extends Error {
  readonly filename: string

  constructor(message: string, filename: string) {
    super(message)
    this.name = 'AttachmentError'
    this.filename = filename
  }
}

/**
 * Turn PromptInput files (already data URLs) into message attachments.
 * Images and PDFs keep their data URL; Word and text files become text.
 */
export async function buildMessageAttachments(
  files: AttachmentFileInput[]
): Promise<MessageAttachment[]> {
  const attachments: MessageAttachment[] = []

  for (const file of files) {
    const name = file.filename || 'file'
    const url = file.url
    const type = getAttachmentType(file)
    if (!url || !type) {
      throw new AttachmentError('Unsupported file type', name)
    }

    if (type === 'image' || type === 'pdf') {
      attachments.push({
        kind: type === 'image' ? 'image' : 'file',
        name,
        mediaType: type === 'pdf' ? 'application/pdf' : (file.mediaType ?? ''),
        url,
      })
      continue
    }

    let text: string
    try {
      const bytes = dataUrlToBytes(url)
      text =
        type === 'docx'
          ? await extractDocxText(bytes)
          : new TextDecoder().decode(bytes)
    } catch {
      throw new AttachmentError('Failed to read file', name)
    }

    if (!text.trim()) {
      throw new AttachmentError('No text found in file', name)
    }

    attachments.push({
      kind: 'text',
      name,
      mediaType: file.mediaType || 'text/plain',
      text: limitText(text),
    })
  }

  return attachments
}
