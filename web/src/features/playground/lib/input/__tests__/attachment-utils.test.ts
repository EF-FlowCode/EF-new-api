import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { formatMessageForAPI } from '../../message/message-utils'
import {
  AttachmentError,
  buildMessageAttachments,
  getAttachmentType,
  isSupportedAttachmentFile,
} from '../attachment-utils'

function textDataUrl(text: string, mediaType = 'text/plain'): string {
  const bytes = new TextEncoder().encode(text)
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return `data:${mediaType};base64,${btoa(binary)}`
}

describe('playground attachments', () => {
  test('classifies files by MIME type and falls back to the extension', () => {
    assert.equal(getAttachmentType({ mediaType: 'image/png' }), 'image')
    assert.equal(getAttachmentType({ filename: 'a.PDF', mediaType: '' }), 'pdf')
    assert.equal(getAttachmentType({ filename: 'report.docx' }), 'docx')
    assert.equal(
      getAttachmentType({ filename: 'notes.md', mediaType: '' }),
      'text'
    )
    assert.equal(getAttachmentType({ filename: 'main.go' }), 'text')
    assert.equal(getAttachmentType({ filename: 'old.doc' }), null)
    assert.ok(
      !isSupportedAttachmentFile({ name: 'a.zip', type: 'application/zip' })
    )
  })

  test('keeps images and PDFs as data URLs and decodes text files', async () => {
    const attachments = await buildMessageAttachments([
      {
        filename: 'a.png',
        mediaType: 'image/png',
        url: 'data:image/png;base64,AA',
      },
      {
        filename: 'a.pdf',
        mediaType: 'application/pdf',
        url: 'data:application/pdf;base64,JVBE',
      },
      { filename: '说明.md', mediaType: '', url: textDataUrl('# 标题\n内容') },
    ])

    assert.deepEqual(attachments, [
      {
        kind: 'image',
        name: 'a.png',
        mediaType: 'image/png',
        url: 'data:image/png;base64,AA',
      },
      {
        kind: 'file',
        name: 'a.pdf',
        mediaType: 'application/pdf',
        url: 'data:application/pdf;base64,JVBE',
      },
      {
        kind: 'text',
        name: '说明.md',
        mediaType: 'text/plain',
        text: '# 标题\n内容',
      },
    ])
  })

  test('rejects unsupported and empty files with the file name', async () => {
    await assert.rejects(
      buildMessageAttachments([
        { filename: 'a.zip', mediaType: 'application/zip', url: 'data:,' },
      ]),
      (error: unknown) =>
        error instanceof AttachmentError && error.filename === 'a.zip'
    )
    await assert.rejects(
      buildMessageAttachments([
        { filename: 'empty.txt', url: textDataUrl('   ') },
      ]),
      (error: unknown) =>
        error instanceof AttachmentError &&
        error.message === 'No text found in file'
    )
  })

  test('formats user attachments as OpenAI content parts', () => {
    const message = formatMessageForAPI({
      key: 'u1',
      from: 'user',
      versions: [{ id: 'v', content: 'Summarize' }],
      attachments: [
        {
          kind: 'image',
          name: 'a.png',
          mediaType: 'image/png',
          url: 'data:image/png;base64,AA',
        },
        {
          kind: 'file',
          name: 'a.pdf',
          mediaType: 'application/pdf',
          url: 'data:application/pdf;base64,JVBE',
        },
        {
          kind: 'text',
          name: 'notes.txt',
          mediaType: 'text/plain',
          text: 'hello',
        },
        // 本地历史里数据已丢弃的附件不再发送
        { kind: 'file', name: 'gone.pdf', mediaType: 'application/pdf' },
      ],
    })

    assert.deepEqual(message, {
      role: 'user',
      content: [
        { type: 'text', text: 'Summarize' },
        { type: 'image_url', image_url: { url: 'data:image/png;base64,AA' } },
        {
          type: 'file',
          file: {
            filename: 'a.pdf',
            file_data: 'data:application/pdf;base64,JVBE',
          },
        },
        { type: 'text', text: '[File: notes.txt]\nhello' },
      ],
    })
  })

  test('plain text messages stay a string', () => {
    assert.deepEqual(
      formatMessageForAPI({
        key: 'u',
        from: 'user',
        versions: [{ id: 'v', content: 'hi' }],
      }),
      { role: 'user', content: 'hi' }
    )
  })
})
