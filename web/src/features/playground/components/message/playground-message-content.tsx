import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import {
  CodeBlock,
  CodeBlockCopyButton,
} from '@/components/ai-elements/code-block'
import { Loader } from '@/components/ai-elements/loader'
import { MessageContent } from '@/components/ai-elements/message'
import {
  Reasoning,
  ReasoningContent,
  ReasoningTrigger,
} from '@/components/ai-elements/reasoning'
import { Response } from '@/components/ai-elements/response'
import { Shimmer } from '@/components/ai-elements/shimmer'
import {
  Source,
  Sources,
  SourcesContent,
  SourcesTrigger,
} from '@/components/ai-elements/sources'
import { MESSAGE_STATUS } from '../../constants'
import {
  getMessageAlignmentClass,
  getMessageContentState,
  isErrorMessage,
  type MessageAlignment,
} from '../../lib'
import { getMessageContentStyles } from '../../lib/message/message-styles'
import type { Message } from '../../types'
import { MessageAttachments } from './message-attachments'
import { MessageError } from './message-error'
import { MessageMetadata } from './message-metadata'

type PlaygroundMessageContentProps = {
  actions: ReactNode
  alignment: MessageAlignment
  errorActions?: ReactNode
  isSourceVisible?: boolean
  message: Message
  versionContent: string
  versionSwitcher?: ReactNode
}

export function PlaygroundMessageContent({
  actions,
  alignment,
  errorActions,
  isSourceVisible = false,
  message,
  versionContent,
  versionSwitcher,
}: PlaygroundMessageContentProps) {
  const { t } = useTranslation()
  const {
    attachments,
    displayContent,
    hasReasoning,
    hasSources,
    reasoningContent,
    showLoader,
    showMessageContent,
    sources,
  } = getMessageContentState(message, versionContent)
  const isError = isErrorMessage(message)
  const isMessageFinal =
    message.status !== MESSAGE_STATUS.LOADING &&
    message.status !== MESSAGE_STATUS.STREAMING

  return (
    <div
      className={cn(
        'flex w-full min-w-0 flex-col',
        getMessageAlignmentClass(alignment)
      )}
    >
      {hasSources && (
        <Sources>
          <SourcesTrigger count={sources.length} />
          <SourcesContent>
            {sources.map((source) => (
              <Source
                href={source.href}
                key={`${source.href}-${source.title}`}
                title={source.title}
              />
            ))}
          </SourcesContent>
        </Sources>
      )}

      {hasReasoning && (
        <Reasoning
          defaultOpen
          duration={message.reasoning?.duration}
          isStreaming={message.isReasoningStreaming}
        >
          <ReasoningTrigger />
          <ReasoningContent>{reasoningContent}</ReasoningContent>
        </Reasoning>
      )}

      {showLoader && (
        <div className='flex items-center gap-2 py-2'>
          <Loader />
          <Shimmer className='text-sm' duration={1}>
            {t('Responding...')}
          </Shimmer>
        </div>
      )}

      {isError && (
        <>
          <MessageError message={message} className='mb-2' />
          <MessageMetadata alignment={alignment} message={message} />
          {versionSwitcher && <div className='mt-1.5'>{versionSwitcher}</div>}
          {errorActions}
        </>
      )}

      {!isError && showMessageContent && (
        <>
          <MessageAttachments
            attachments={attachments}
            className={displayContent ? 'mb-2' : undefined}
          />
          {!displayContent ? null : isSourceVisible ? (
            <CodeBlock
              code={versionContent}
              className='my-0 group-[.is-assistant]:w-full group-[.is-assistant]:max-w-[78ch]'
              collapsedLines={24}
              defaultCollapsed={false}
              language='markdown'
              maxExpandedLines={48}
              showLineNumbers
              showToolbar
              title={t('Raw response')}
            >
              <CodeBlockCopyButton />
            </CodeBlock>
          ) : (
            <MessageContent
              variant='flat'
              className={cn(getMessageContentStyles())}
            >
              <Response final={isMessageFinal}>{displayContent}</Response>
            </MessageContent>
          )}
          <MessageMetadata alignment={alignment} message={message} />
          <div className='mt-1.5 flex flex-wrap items-center gap-1'>
            {versionSwitcher}
            {actions}
          </div>
        </>
      )}
    </div>
  )
}
