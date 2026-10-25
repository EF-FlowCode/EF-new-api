import type { Session } from '../types'
import { getCurrentVersion, getVersionText } from './message-utils'

function sanitizeFileName(name: string): string {
  const base = (name || 'session').replace(/[\\/:*?"<>|]+/g, '_').trim()
  return (base || 'session').slice(0, 60)
}

export function sessionToMarkdown(session: Session): string {
  const lines: string[] = [`# ${session.title || 'Session'}`, '']
  if (session.systemPrompt?.trim()) {
    lines.push('> **System**', '')
    lines.push(
      session.systemPrompt
        .trim()
        .split('\n')
        .map((l) => `> ${l}`)
        .join('\n'),
      '',
      '---',
      ''
    )
  }
  for (const m of session.messages) {
    const role =
      m.from === 'user'
        ? 'User'
        : m.from === 'assistant'
          ? 'Assistant'
          : 'System'
    const text = getVersionText(getCurrentVersion(m).content).trim()
    if (!text) continue
    lines.push(`**${role}**`, '', text, '', '---', '')
  }
  return lines.join('\n')
}

export function sessionToJSON(session: Session): string {
  return JSON.stringify(session, null, 2)
}

function download(filename: string, content: string, mime: string): void {
  if (typeof window === 'undefined') return
  const blob = new Blob([content], { type: mime })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

export function exportSession(session: Session, format: 'md' | 'json'): void {
  const base = sanitizeFileName(session.title)
  if (format === 'md') {
    download(`${base}.md`, sessionToMarkdown(session), 'text/markdown')
  } else {
    download(`${base}.json`, sessionToJSON(session), 'application/json')
  }
}
