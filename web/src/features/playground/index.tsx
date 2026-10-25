import { useCallback, useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getUserModels, getUserGroups } from './api'
import { ContextUsageBar } from './components/context-usage-bar'
import { PlaygroundChat } from './components/playground-chat'
import { PlaygroundInput } from './components/playground-input'
import { PlaygroundSidebar } from './components/playground-sidebar'
import { DEFAULT_GROUP, MESSAGE_STATUS } from './constants'
import { usePlaygroundState, useChatHandler } from './hooks'
import {
  buildMessageContent,
  createUserMessage,
  createLoadingAssistantMessage,
  createMessageVersion,
} from './lib'
import type { Message as MessageType } from './types'

export function Playground() {
  const {
    config,
    parameterEnabled,
    messages,
    models,
    groups,
    sessions,
    activeId,
    updateMessages,
    setModels,
    setGroups,
    updateConfig,
    updateParameterEnabled,
    createSession,
    switchSession,
    deleteSession,
    renameSession,
    systemPrompt,
    setSystemPrompt,
  } = usePlaygroundState()

  const { sendChat, stopGeneration, isGenerating } = useChatHandler({
    config,
    parameterEnabled,
    onMessageUpdate: updateMessages,
    systemPrompt,
  })

  // Edit dialog state
  const [editingMessageKey, setEditingMessageKey] = useState<string | null>(
    null
  )

  // Load models
  const { data: modelsData, isLoading: isLoadingModels } = useQuery({
    queryKey: ['playground-models'],
    queryFn: getUserModels,
  })

  // Load groups
  const { data: groupsData } = useQuery({
    queryKey: ['playground-groups'],
    queryFn: getUserGroups,
  })

  // Update models when data changes
  useEffect(() => {
    if (!modelsData) return

    setModels(modelsData)

    // Set default model if current model is not available
    const isCurrentModelValid = modelsData.some((m) => m.value === config.model)
    if (modelsData.length > 0 && !isCurrentModelValid) {
      updateConfig('model', modelsData[0].value)
    }
  }, [modelsData, config.model, setModels, updateConfig])

  // Update groups when data changes
  useEffect(() => {
    if (!groupsData) return

    // 不再前端虚构 "auto" 分组：后端 distributor 在 playgroundRequest.Group
    // 非空时会做 GroupInUserUsableGroups 权限校验，硬塞一个用户没权限的
    // "auto" 进下拉 + 默认选中，会让首次发送直接 403。
    // 真实可用列表（后端 /api/user/self/groups 返回）原样吐给前端，
    // 用户没选时 group="" 命中后端的"跳过校验、走用户默认 group"分支。
    const visibleGroups = groupsData.filter(
      (group) => group.available !== false || group.value === config.group
    )
    setGroups(visibleGroups)

    // 历史缓存清理：老用户 localStorage 里 group='auto' 之类的值，
    // 如果不在真实可用列表里，回退到空（让后端走默认 group）。
    if (config.group && !groupsData.some((g) => g.value === config.group)) {
      updateConfig('group', DEFAULT_GROUP)
    }
  }, [groupsData, setGroups, config.group, updateConfig])

  const handleSendMessage = (text: string, imageDataUrls: string[] = []) => {
    // 有图就走 ContentPart[]，无图保留原 string，跟历史消息形状一致。
    const content = buildMessageContent(text, imageDataUrls)
    const userMessage = createUserMessage(content)
    const assistantMessage = createLoadingAssistantMessage()

    const newMessages = [...messages, userMessage, assistantMessage]
    updateMessages(newMessages)

    // Send chat request
    sendChat(newMessages)
  }

  const handleCopyMessage = (message: MessageType) => {
    // Copy is handled in MessageActions component
    // eslint-disable-next-line no-console
    console.log('Message copied:', message.key)
  }

  const handleRegenerateMessage = (message: MessageType) => {
    const messageIndex = messages.findIndex((m) => m.key === message.key)
    if (messageIndex === -1) return

    if (message.from === 'assistant') {
      // 重生成 = 在同一条 assistant 消息上追加一个新版本，保留旧回答，
      // 由 BranchSelector 做多版本对比。截断其后的消息；新空版本为
      // 当前版本（getCurrentVersion=last），流式写入它。
      const target = messages[messageIndex]
      const regenerated: MessageType = {
        ...target,
        versions: [...target.versions, createMessageVersion('')],
        reasoning: undefined,
        isReasoningStreaming: false,
        isReasoningComplete: false,
        isContentComplete: false,
        status: MESSAGE_STATUS.LOADING,
        errorCode: null,
        thinkStream: undefined,
        streamingVisible: undefined,
      }
      // UI/流式目标：含新空版本的 assistant 作为最后一条
      const displayMessages = [...messages.slice(0, messageIndex), regenerated]
      updateMessages(displayMessages)
      // payload 上下文：截到该 assistant 之前（不含它自身，避免把旧/空答案带入）
      sendChat(messages.slice(0, messageIndex))
      return
    }

    // 非 assistant（如 user）：保持旧行为——丢弃其后并新起一条回答
    const messagesUpToHere = messages.slice(0, messageIndex)
    const loadingMessage = createLoadingAssistantMessage()
    const newMessages = [...messagesUpToHere, loadingMessage]

    updateMessages(newMessages)
    sendChat(newMessages)
  }

  const handleEditMessage = useCallback((message: MessageType) => {
    setEditingMessageKey(message.key)
  }, [])

  const handleEditOpenChange = useCallback((open: boolean) => {
    if (!open) setEditingMessageKey(null)
  }, [])

  // Apply edit and optionally re-submit from the edited user message
  const applyEdit = useCallback(
    (newContent: string, submit: boolean) => {
      if (!editingMessageKey) return
      const index = messages.findIndex((m) => m.key === editingMessageKey)
      if (index === -1) return

      const updated = messages.map((m) => {
        if (m.key !== editingMessageKey) return m
        const oldContent = m.versions[0]?.content
        // 多模态消息保留原图片，只换文本部分；纯文本消息直接换字符串。
        let nextContent: string | typeof oldContent = newContent
        if (Array.isArray(oldContent)) {
          const images = oldContent.filter((p) => p.type === 'image_url')
          nextContent = newContent
            ? [{ type: 'text' as const, text: newContent }, ...images]
            : images
        }
        return { ...m, versions: [{ ...m.versions[0], content: nextContent }] }
      })

      setEditingMessageKey(null)

      if (!submit || updated[index].from !== 'user') {
        updateMessages(updated)
        return
      }

      const toSubmit = [
        ...updated.slice(0, index + 1),
        createLoadingAssistantMessage(),
      ]
      updateMessages(toSubmit)
      sendChat(toSubmit)
    },
    [editingMessageKey, messages, updateMessages, sendChat]
  )

  const handleDeleteMessage = (message: MessageType) => {
    const newMessages = messages.filter((m) => m.key !== message.key)
    updateMessages(newMessages)
  }

  return (
    <div className='relative flex size-full overflow-hidden'>
      <PlaygroundSidebar
        sessions={sessions}
        activeId={activeId}
        messages={messages}
        systemPrompt={systemPrompt}
        config={config}
        parameterEnabled={parameterEnabled}
        disabled={isGenerating}
        onSwitch={switchSession}
        onCreate={createSession}
        onRename={renameSession}
        onDelete={deleteSession}
        onSystemPromptChange={setSystemPrompt}
        onConfigChange={updateConfig}
        onParameterToggle={updateParameterEnabled}
      />
      <div className='flex min-w-0 flex-1 flex-col overflow-hidden'>
        {/* Full-width scroll container: scrolling works even over side whitespace */}
        <div className='flex flex-1 flex-col overflow-hidden'>
          <PlaygroundChat
            messages={messages}
            onCopyMessage={handleCopyMessage}
            onRegenerateMessage={handleRegenerateMessage}
            onEditMessage={handleEditMessage}
            onDeleteMessage={handleDeleteMessage}
            isGenerating={isGenerating}
            editingKey={editingMessageKey}
            onCancelEdit={handleEditOpenChange}
            onSaveEdit={(newContent) => applyEdit(newContent, false)}
            onSaveEditAndSubmit={(newContent) => applyEdit(newContent, true)}
          />
        </div>

        {/* Input area: center content and constrain to the same container width */}
        <div className='mx-auto w-full max-w-4xl'>
          <ContextUsageBar
            messages={messages}
            disabled={isGenerating}
            onTrim={updateMessages}
          />
          <PlaygroundInput
            disabled={isGenerating}
            groups={groups}
            groupValue={config.group}
            isGenerating={isGenerating}
            isModelLoading={isLoadingModels}
            modelValue={config.model}
            models={models}
            onGroupChange={(value) => updateConfig('group', value)}
            onModelChange={(value) => updateConfig('model', value)}
            onStop={stopGeneration}
            onSubmit={handleSendMessage}
          />
        </div>
      </div>
    </div>
  )
}
