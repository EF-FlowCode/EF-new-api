import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useStickToBottomContext } from 'use-stick-to-bottom'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import {
  Branch,
  BranchMessages,
  BranchNext,
  BranchPage,
  BranchPrevious,
  BranchSelector,
} from '@/components/ai-elements/branch'
import {
  Conversation,
  ConversationContent,
  ConversationScrollButton,
} from '@/components/ai-elements/conversation'
import { Loader } from '@/components/ai-elements/loader'
import { Message, MessageContent } from '@/components/ai-elements/message'
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
import { MESSAGE_ROLES } from '../constants'
import { getMessageContentStyles } from '../lib/message-styles'
import { parseThinkTags, getVersionText } from '../lib/message-utils'
import type { Message as MessageType, ContentPart } from '../types'
import { MessageActions } from './message-actions'
import { MessageError } from './message-error'

// 操练场内 Streamdown 控件：代码块复制/下载、Mermaid 图表复制/下载/全屏/缩放、
// 表格复制/下载。仅作用于操练场 <Response> 调用点，不改全站共享 Response 默认。
// KaTeX 数学公式由 Streamdown 默认 remark/rehype 插件渲染（无需开关、零新增依赖，
// mermaid@11 / katex@0.16 已是 streamdown 的内置依赖）。
const STREAMDOWN_CONTROLS = {
  code: { copy: true, download: true },
  mermaid: { copy: true, download: true, fullscreen: true, panZoom: true },
  table: { copy: true, download: true },
} as const

interface PlaygroundChatProps {
  messages: MessageType[]
  onCopyMessage?: (message: MessageType) => void
  onRegenerateMessage?: (message: MessageType) => void
  onEditMessage?: (message: MessageType) => void
  onDeleteMessage?: (message: MessageType) => void
  isGenerating?: boolean
  editingKey?: string | null
  onSaveEdit?: (newContent: string) => void
  onCancelEdit?: (open: boolean) => void
  onSaveEditAndSubmit?: (newContent: string) => void
}

interface ChatMessageRowProps {
  message: MessageType
  isLastAssistantMessage: boolean
  isGenerating: boolean
  isEditing: boolean
  editText: string
  setEditText: (v: string) => void
  isEmpty: boolean
  isChanged: boolean
  onCopyMessage?: (message: MessageType) => void
  onRegenerateMessage?: (message: MessageType) => void
  onEditMessage?: (message: MessageType) => void
  onDeleteMessage?: (message: MessageType) => void
  onSaveEdit?: (newContent: string) => void
  onCancelEdit?: (open: boolean) => void
  onSaveEditAndSubmit?: (newContent: string) => void
}

// 单条消息行。memo + 自定义比较器：流式逐 token 时，只有 message 引用变化的那
// 一行（= 正在流式的最后一条）重渲染；历史行 message 引用不变、非编辑、非末条
// 则整体跳过，不再重跑 parseThinkTags / 重建 JSX（消除 O(n·tokens)）。
const ChatMessageRow = memo(
  function ChatMessageRow({
    message,
    isLastAssistantMessage,
    isGenerating,
    isEditing,
    editText,
    setEditText,
    isEmpty,
    isChanged,
    onCopyMessage,
    onRegenerateMessage,
    onEditMessage,
    onDeleteMessage,
    onSaveEdit,
    onCancelEdit,
    onSaveEditAndSubmit,
  }: ChatMessageRowProps) {
    const { versions = [] } = message
    return (
      <Branch defaultBranch={0}>
        <BranchMessages>
          {versions.map((version, versionIndex) => (
            <Message
              className='group flex-row-reverse'
              from={message.from}
              key={`${message.key}-${version.id}-${versionIndex}`}
            >
              <div className='w-full min-w-0 flex-1 basis-full py-1'>
                {isEditing ? (
                  <div className='space-y-2'>
                    <Textarea
                      value={editText}
                      onChange={(e) => setEditText(e.target.value)}
                      className='font-mono text-sm'
                      rows={8}
                    />
                    <div className='flex gap-2'>
                      {/* Save & Submit only makes sense for user messages */}
                      {message.from === MESSAGE_ROLES.USER && (
                        <Button
                          size='sm'
                          onClick={() => onSaveEditAndSubmit?.(editText)}
                          disabled={isEmpty || !isChanged}
                        >
                          Save & Submit
                        </Button>
                      )}
                      <Button
                        size='sm'
                        onClick={() => onSaveEdit?.(editText)}
                        disabled={isEmpty || !isChanged}
                      >
                        Save
                      </Button>
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => onCancelEdit?.(false)}
                      >
                        Cancel
                      </Button>
                    </div>
                  </div>
                ) : (
                  <>
                    {(() => {
                      const isAssistant =
                        message.from === MESSAGE_ROLES.ASSISTANT
                      const hasSources = !!message.sources?.length
                      const showReasoning =
                        isAssistant && !!message.reasoning?.content
                      // 用户多模态消息：content 为 ContentPart[]，
                      // 抽出文本与图片 url 分别渲染。
                      const isMultimodalUser =
                        !isAssistant && Array.isArray(version.content)
                      const userImageUrls: string[] = isMultimodalUser
                        ? (version.content as ContentPart[])
                            .filter(
                              (p) =>
                                p.type === 'image_url' && !!p.image_url?.url
                            )
                            .map((p) => p.image_url!.url)
                        : []
                      const userTextOnly = isMultimodalUser
                        ? getVersionText(version.content)
                        : ''
                      const hasUserPayload =
                        isMultimodalUser &&
                        (userTextOnly.length > 0 || userImageUrls.length > 0)
                      const showLoader =
                        isAssistant &&
                        !message.isReasoningStreaming &&
                        (message.status === 'loading' ||
                          (message.status === 'streaming' && !version.content))
                      const showMessageContent =
                        (message.from === MESSAGE_ROLES.USER ||
                          !message.isReasoningStreaming) &&
                        (isMultimodalUser ? hasUserPayload : !!version.content)

                      // Assistant 可见正文：流式期间用 A1 增量算好的
                      // streamingVisible（不再每 token parseThinkTags 重扫）；
                      // 完成/加载态 content 已是去 <think> 的最终文本，
                      // parseThinkTags 仅在该行变化时跑一次（memo 行）。
                      const displayContent = isAssistant
                        ? message.streamingVisible !== undefined
                          ? message.streamingVisible
                          : parseThinkTags(getVersionText(version.content))
                              .visibleContent
                        : userTextOnly ||
                          (typeof version.content === 'string'
                            ? version.content
                            : '')

                      const actions = (
                        <MessageActions
                          message={message}
                          onCopy={onCopyMessage}
                          onRegenerate={onRegenerateMessage}
                          onEdit={onEditMessage}
                          onDelete={onDeleteMessage}
                          isGenerating={isGenerating}
                          alwaysVisible={isLastAssistantMessage}
                          className='mt-1'
                        />
                      )

                      return (
                        <>
                          {/* Sources */}
                          {hasSources && (
                            <Sources>
                              <SourcesTrigger count={message.sources!.length} />
                              <SourcesContent>
                                {message.sources!.map((source, sourceIndex) => (
                                  <Source
                                    href={source.href}
                                    key={`${message.key}-source-${sourceIndex}`}
                                    title={source.title}
                                  />
                                ))}
                              </SourcesContent>
                            </Sources>
                          )}

                          {/* Reasoning */}
                          {showReasoning && (
                            <Reasoning
                              defaultOpen={true}
                              isStreaming={message.isReasoningStreaming}
                            >
                              <ReasoningTrigger />
                              <ReasoningContent>
                                {message.reasoning!.content}
                              </ReasoningContent>
                            </Reasoning>
                          )}

                          {/* Loader */}
                          {showLoader && (
                            <div className='flex items-center gap-2 py-2'>
                              <Loader />
                              <Shimmer className='text-sm' duration={1}>
                                Responding...
                              </Shimmer>
                            </div>
                          )}

                          {/* Error or Content */}
                          {message.status === 'error' ? (
                            <>
                              <MessageError
                                message={message}
                                className='mb-2'
                              />
                              {actions}
                            </>
                          ) : (
                            showMessageContent && (
                              <>
                                <MessageContent
                                  variant='flat'
                                  className={cn(getMessageContentStyles())}
                                >
                                  {userImageUrls.length > 0 && (
                                    <div className='mb-2 flex flex-wrap gap-2'>
                                      {userImageUrls.map((url, i) => (
                                        <a
                                          href={url}
                                          key={`${message.key}-img-${i}`}
                                          rel='noreferrer'
                                          target='_blank'
                                        >
                                          <img
                                            alt='attachment'
                                            className='h-32 w-32 rounded-md border object-cover'
                                            src={url}
                                          />
                                        </a>
                                      ))}
                                    </div>
                                  )}
                                  {displayContent && (
                                    <Response controls={STREAMDOWN_CONTROLS}>
                                      {displayContent}
                                    </Response>
                                  )}
                                </MessageContent>
                                {actions}
                              </>
                            )
                          )}
                        </>
                      )
                    })()}
                  </>
                )}
              </div>
            </Message>
          ))}
        </BranchMessages>

        {/* Branch selector for multiple versions */}
        {versions.length > 1 && (
          <BranchSelector className='px-0' from={message.from}>
            <BranchPrevious />
            <BranchPage />
            <BranchNext />
          </BranchSelector>
        )}
      </Branch>
    )
  },
  (prev, next) => {
    // 正在编辑的行始终重渲染（反映 editText/按钮态）
    if (next.isEditing || prev.isEditing !== next.isEditing) return false
    // 其余只在这些影响渲染的输入变化时重渲染；
    // 流式逐 token 只有当前流式行的 message 引用会变。
    return (
      prev.message === next.message &&
      prev.isLastAssistantMessage === next.isLastAssistantMessage &&
      prev.isGenerating === next.isGenerating
    )
  }
)

interface MessageListProps {
  messages: MessageType[]
  isGenerating: boolean
  editingKey?: string | null
  editText: string
  setEditText: (v: string) => void
  isEmpty: boolean
  isChanged: boolean
  onCopyMessage?: (message: MessageType) => void
  onRegenerateMessage?: (message: MessageType) => void
  onEditMessage?: (message: MessageType) => void
  onDeleteMessage?: (message: MessageType) => void
  onSaveEdit?: (newContent: string) => void
  onCancelEdit?: (open: boolean) => void
  onSaveEditAndSubmit?: (newContent: string) => void
}

type RegisterFn = (el: Element, cb: (inView: boolean) => void) => () => void

// 离屏剔除行：在视口缓冲区外时把整行内容卸载、用一个等高占位块顶住，保留
// 滚动高度（stick-to-bottom 的贴底/自动滚动完全不受影响），离屏的 Streamdown
// 等重组件不挂载。forceShow（最后一条/流式）恒渲染。
const CulledRow = memo(function CulledRow({
  register,
  forceShow,
  rowProps,
}: {
  register: RegisterFn
  forceShow: boolean
  rowProps: ChatMessageRowProps
}) {
  const wrapperRef = useRef<HTMLDivElement | null>(null)
  const [inView, setInView] = useState(true)
  // 用 state 存测量高度（render 里只能读 state/props，不能读 ref.current）
  const [measuredHeight, setMeasuredHeight] = useState<number | undefined>(
    undefined
  )

  // 注册到列表共享的 IntersectionObserver
  useEffect(() => {
    const el = wrapperRef.current
    if (!el || forceShow) return
    return register(el, setInView)
  }, [register, forceShow])

  const show = forceShow || inView || measuredHeight === undefined

  // 显示时持续记录高度（用于剔除后占位）
  useEffect(() => {
    if (!show) return
    const el = wrapperRef.current
    if (!el) return
    const ro = new ResizeObserver(() => {
      const h = el.offsetHeight
      if (h > 0) setMeasuredHeight((prev) => (prev === h ? prev : h))
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [show])

  return (
    <div
      ref={wrapperRef}
      data-msg-key={rowProps.message.key}
      style={show ? undefined : { height: measuredHeight }}
      aria-hidden={show ? undefined : true}
    >
      {show ? <ChatMessageRow {...rowProps} /> : null}
    </div>
  )
})

// 消息列表：建立一个挂在 stick-to-bottom 滚动容器上的共享
// IntersectionObserver（大 rootMargin 缓冲，滚动不闪占位），逐条交给 CulledRow。
function MessageList(props: MessageListProps) {
  const { scrollRef } = useStickToBottomContext()
  const cbMapRef = useRef<Map<Element, (v: boolean) => void>>(new Map())
  const observerRef = useRef<IntersectionObserver | null>(null)

  useEffect(() => {
    const root = scrollRef.current ?? null
    const obs = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          const cb = cbMapRef.current.get(e.target)
          if (cb) cb(e.isIntersecting)
        }
      },
      { root, rootMargin: '1200px 0px 1200px 0px' }
    )
    observerRef.current = obs
    // 重新注册已有节点（root 变化时）
    for (const el of cbMapRef.current.keys()) obs.observe(el)
    return () => obs.disconnect()
  }, [scrollRef])

  const register = useCallback<RegisterFn>((el, cb) => {
    cbMapRef.current.set(el, cb)
    observerRef.current?.observe(el)
    return () => {
      observerRef.current?.unobserve(el)
      cbMapRef.current.delete(el)
    }
  }, [])

  const {
    messages,
    isGenerating,
    editingKey,
    editText,
    setEditText,
    isEmpty,
    isChanged,
    onCopyMessage,
    onRegenerateMessage,
    onEditMessage,
    onDeleteMessage,
    onSaveEdit,
    onCancelEdit,
    onSaveEditAndSubmit,
  } = props

  return (
    <div className='mx-auto w-full max-w-4xl px-4 py-4'>
      {messages.map((message, messageIndex) => {
        const isLast = messageIndex === messages.length - 1
        const isLastAssistantMessage =
          isLast && message.from === MESSAGE_ROLES.ASSISTANT
        const rowProps: ChatMessageRowProps = {
          message,
          isLastAssistantMessage,
          isGenerating,
          isEditing: editingKey === message.key,
          editText,
          setEditText,
          isEmpty,
          isChanged,
          onCopyMessage,
          onRegenerateMessage,
          onEditMessage,
          onDeleteMessage,
          onSaveEdit,
          onCancelEdit,
          onSaveEditAndSubmit,
        }
        return (
          <CulledRow
            key={message.key}
            register={register}
            forceShow={isLast}
            rowProps={rowProps}
          />
        )
      })}
    </div>
  )
}

export function PlaygroundChat({
  messages,
  onCopyMessage,
  onRegenerateMessage,
  onEditMessage,
  onDeleteMessage,
  isGenerating = false,
  editingKey,
  onSaveEdit,
  onCancelEdit,
  onSaveEditAndSubmit,
}: PlaygroundChatProps) {
  const [editText, setEditText] = useState('')
  const [originalText, setOriginalText] = useState('')

  useEffect(() => {
    if (!editingKey) return
    const message = messages.find((m) => m.key === editingKey)
    // 多模态消息 (ContentPart[]) 编辑只取文本部分；图片保持原样不进 textarea。
    const content = getVersionText(message?.versions?.[0]?.content ?? '')
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setEditText(content)

    setOriginalText(content)
  }, [editingKey, messages])

  const isEmpty = useMemo(() => !editText.trim(), [editText])
  const isChanged = useMemo(
    () => editText !== originalText,
    [editText, originalText]
  )
  return (
    <Conversation>
      {/* Remove outer padding; apply padding to inner centered container to align with input */}
      <ConversationContent className='p-0'>
        <MessageList
          messages={messages}
          isGenerating={isGenerating}
          editingKey={editingKey}
          editText={editText}
          setEditText={setEditText}
          isEmpty={isEmpty}
          isChanged={isChanged}
          onCopyMessage={onCopyMessage}
          onRegenerateMessage={onRegenerateMessage}
          onEditMessage={onEditMessage}
          onDeleteMessage={onDeleteMessage}
          onSaveEdit={onSaveEdit}
          onCancelEdit={onCancelEdit}
          onSaveEditAndSubmit={onSaveEditAndSubmit}
        />
      </ConversationContent>
      <ConversationScrollButton />
    </Conversation>
  )
}
