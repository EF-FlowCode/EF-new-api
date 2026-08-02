import { useState, useCallback, useRef, useEffect, useMemo } from 'react'
import {
  DEFAULT_CONFIG,
  DEFAULT_PARAMETER_ENABLED,
  DEFAULT_SESSION_TITLE,
} from '../constants'
import {
  loadConfig,
  saveConfig,
  loadParameterEnabled,
  saveParameterEnabled,
  loadSessions,
  saveSessions,
  createSession as createSessionLib,
  deriveSessionTitle,
} from '../lib'
import type {
  Message,
  Session,
  PlaygroundConfig,
  ParameterEnabled,
  ModelOption,
  GroupOption,
} from '../types'

// 流式持久化节流窗口：700ms 内的多次增量更新合并成一次写盘。
const SAVE_THROTTLE_MS = 700
const EMPTY_MESSAGES: Message[] = []

interface SessionStore {
  sessions: Session[]
  activeId: string
}

/**
 * Main state management hook for playground
 */
export function usePlaygroundState() {
  // Load initial state from localStorage
  const [config, setConfig] = useState<PlaygroundConfig>(() => {
    const savedConfig = loadConfig()
    return { ...DEFAULT_CONFIG, ...savedConfig }
  })

  const [parameterEnabled, setParameterEnabled] = useState<ParameterEnabled>(
    () => {
      const saved = loadParameterEnabled()
      return { ...DEFAULT_PARAMETER_ENABLED, ...saved }
    }
  )

  // 多会话：单一 store（sessions + activeId），对外仍派生出 messages /
  // updateMessages，使 chat handler / index.tsx 的契约保持不变。
  const [store, setStore] = useState<SessionStore>(() => loadSessions())
  const { sessions, activeId } = store

  const activeSession = useMemo(
    () => sessions.find((s) => s.id === activeId) ?? sessions[0],
    [sessions, activeId]
  )
  const messages = activeSession?.messages ?? EMPTY_MESSAGES

  const [models, setModels] = useState<ModelOption[]>([])
  const [groups, setGroups] = useState<GroupOption[]>([])

  // 流式时 updateMessages 每 token 调一次；同步 saveSessions(JSON.stringify)
  // 会随对话变长产生 O(n²) 临时字符串把渲染进程内存打爆（OOM 主因）。
  // 内存 state 仍每 token 更新（流式 UI 不变），持久化解耦为 700ms 节流；
  // 显式/批量动作（清空、新建、切换、删除、重命名）取消挂起节流并立即落盘。
  const pendingSaveRef = useRef<SessionStore | null>(null)
  const saveTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const scheduleSave = useCallback((next: SessionStore) => {
    pendingSaveRef.current = next
    if (saveTimerRef.current !== null) return
    saveTimerRef.current = setTimeout(() => {
      saveTimerRef.current = null
      const s = pendingSaveRef.current
      pendingSaveRef.current = null
      if (s) saveSessions(s.sessions, s.activeId)
    }, SAVE_THROTTLE_MS)
  }, [])

  const cancelPendingSave = useCallback(() => {
    if (saveTimerRef.current !== null) {
      clearTimeout(saveTimerRef.current)
      saveTimerRef.current = null
    }
    pendingSaveRef.current = null
  }, [])

  // 收尾兜底：卸载 / 关页时把还没落盘的最新状态刷一次。
  useEffect(() => {
    const flush = () => {
      const s = pendingSaveRef.current
      if (s) {
        pendingSaveRef.current = null
        saveSessions(s.sessions, s.activeId)
      }
    }
    window.addEventListener('pagehide', flush)
    return () => {
      window.removeEventListener('pagehide', flush)
      if (saveTimerRef.current !== null) clearTimeout(saveTimerRef.current)
      flush()
    }
  }, [])

  const persistNow = useCallback(
    (next: SessionStore) => {
      cancelPendingSave()
      saveSessions(next.sessions, next.activeId)
    },
    [cancelPendingSave]
  )

  // Update config with automatic save
  const updateConfig = useCallback(
    <K extends keyof PlaygroundConfig>(key: K, value: PlaygroundConfig[K]) => {
      setConfig((prev) => {
        const updated = { ...prev, [key]: value }
        saveConfig(updated)
        return updated
      })
    },
    []
  )

  // Update parameter enabled with automatic save
  const updateParameterEnabled = useCallback(
    (key: keyof ParameterEnabled, value: boolean) => {
      setParameterEnabled((prev) => {
        const updated = { ...prev, [key]: value }
        saveParameterEnabled(updated)
        return updated
      })
    },
    []
  )

  // 更新「当前会话」的 messages。函数式 updater（流式逐 token）→ 节流持久化；
  // 显式数组（清空 / 整体替换）→ 取消挂起节流并立即落盘。契约与旧版一致。
  const updateMessages = useCallback(
    (updater: Message[] | ((prev: Message[]) => Message[])) => {
      setStore((prev) => {
        const cur =
          prev.sessions.find((s) => s.id === prev.activeId) ??
          prev.sessions[0]
        if (!cur) return prev
        const prevMsgs = cur.messages
        const newMsgs =
          typeof updater === 'function' ? updater(prevMsgs) : updater
        const sessions = prev.sessions.map((s) =>
          s.id === cur.id
            ? {
                ...s,
                messages: newMsgs,
                updatedAt: Date.now(),
                // 仅当尚未拥有实质标题时，从首条用户消息派生
                title:
                  s.title === DEFAULT_SESSION_TITLE
                    ? deriveSessionTitle(newMsgs)
                    : s.title,
              }
            : s
        )
        const next: SessionStore = { sessions, activeId: prev.activeId }
        if (typeof updater === 'function') scheduleSave(next)
        else persistNow(next)
        return next
      })
    },
    [scheduleSave, persistNow]
  )

  // 清空「当前会话」消息（保留会话本身）
  const clearMessages = useCallback(() => {
    updateMessages([])
  }, [updateMessages])

  const createSession = useCallback(() => {
    setStore((prev) => {
      const s = createSessionLib([])
      const next: SessionStore = {
        sessions: [...prev.sessions, s],
        activeId: s.id,
      }
      persistNow(next)
      return next
    })
  }, [persistNow])

  const switchSession = useCallback(
    (id: string) => {
      setStore((prev) => {
        if (id === prev.activeId || !prev.sessions.some((s) => s.id === id))
          return prev
        const next: SessionStore = { sessions: prev.sessions, activeId: id }
        persistNow(next)
        return next
      })
    },
    [persistNow]
  )

  const deleteSession = useCallback(
    (id: string) => {
      setStore((prev) => {
        let sessions = prev.sessions.filter((s) => s.id !== id)
        if (sessions.length === 0) sessions = [createSessionLib([])]
        const activeId =
          prev.activeId === id ? sessions[0].id : prev.activeId
        const next: SessionStore = { sessions, activeId }
        persistNow(next)
        return next
      })
    },
    [persistNow]
  )

  const renameSession = useCallback(
    (id: string, title: string) => {
      const t = title.trim()
      if (!t) return
      setStore((prev) => {
        const sessions = prev.sessions.map((s) =>
          s.id === id ? { ...s, title: t, updatedAt: Date.now() } : s
        )
        const next: SessionStore = { sessions, activeId: prev.activeId }
        persistNow(next)
        return next
      })
    },
    [persistNow]
  )

  const setSystemPrompt = useCallback(
    (text: string) => {
      setStore((prev) => {
        const cur =
          prev.sessions.find((s) => s.id === prev.activeId) ??
          prev.sessions[0]
        if (!cur) return prev
        const sessions = prev.sessions.map((s) =>
          s.id === cur.id
            ? { ...s, systemPrompt: text, updatedAt: Date.now() }
            : s
        )
        const next: SessionStore = { sessions, activeId: prev.activeId }
        persistNow(next)
        return next
      })
    },
    [persistNow]
  )

  // Reset config to defaults
  const resetConfig = useCallback(() => {
    setConfig(DEFAULT_CONFIG)
    setParameterEnabled(DEFAULT_PARAMETER_ENABLED)
    saveConfig(DEFAULT_CONFIG)
    saveParameterEnabled(DEFAULT_PARAMETER_ENABLED)
  }, [])

  return {
    // State
    config,
    parameterEnabled,
    messages,
    models,
    groups,
    sessions,
    activeId,
    systemPrompt: activeSession?.systemPrompt ?? '',

    // Setters
    setModels,
    setGroups,

    // Actions
    updateConfig,
    updateParameterEnabled,
    updateMessages,
    clearMessages,
    resetConfig,
    createSession,
    switchSession,
    deleteSession,
    renameSession,
    setSystemPrompt,
  }
}
