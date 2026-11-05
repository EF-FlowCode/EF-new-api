import { MESSAGE_ROLES } from '../../constants'
import type { Session } from '../../types'
import {
  getMessageAttachments,
  getMessageContent,
} from '../message/message-utils'

export type SessionExportFormat = 'md' | 'json'

const ROLE_LABELS = {
  [MESSAGE_ROLES.USER]: 'User',
  [MESSAGE_ROLES.ASSISTANT]: 'Assistant',
  [MESSAGE_ROLES.SYSTEM]: 'System',
} as const

function sanitizeFileName(name: string): string {
  const base = (name || 'session').replace(/[\\/:*?"<>|]+/g, '_').trim()
  return (base || 'session').slice(0, 60)
}

export function sessionToMarkdown(session: Session): string {
  const lines: string[] = [`# ${session.title || 'Session'}`, '']
  const systemPrompt = session.systemPrompt?.trim()
  if (systemPrompt) {
    lines.push(
      '> **System**',
      '',
      systemPrompt
        .split('\n')
        .map((line) => `> ${line}`)
        .join('\n'),
      '',
      '---',
      ''
    )
  }

  for (const message of session.messages) {
    const text = getMessageContent(message).trim()
    const attachments = getMessageAttachments(message)
    if (!text && attachments.length === 0) continue

    lines.push(`**${ROLE_LABELS[message.from]}**`, '')
    if (text) lines.push(text, '')
    // 附件内容（data URL、提取文字）体积大，Markdown 里只列文件名；完整内容见 JSON 导出
    for (const attachment of attachments) {
      lines.push(`_[${attachment.name}]_`, '')
    }
    lines.push('---', '')
  }

  return lines.join('\n')
}

export function sessionToJSON(session: Session): string {
  return JSON.stringify(session, null, 2)
}

function download(filename: string, content: string, mime: string): void {
  const blob = new Blob([content], { type: mime })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

export function exportSession(
  session: Session,
  format: SessionExportFormat
): void {
  const base = sanitizeFileName(session.title)
  if (format === 'md') {
    download(`${base}.md`, sessionToMarkdown(session), 'text/markdown')
    return
  }
  download(`${base}.json`, sessionToJSON(session), 'application/json')
}
