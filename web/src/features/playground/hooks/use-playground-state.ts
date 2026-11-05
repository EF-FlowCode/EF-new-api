import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { DEFAULT_CONFIG, DEFAULT_PARAMETER_ENABLED } from '../constants'
import {
  addSession,
  applyMessageStateUpdate,
  createSessionStore,
  getActiveSession,
  getInitialParameterEnabled,
  getInitialPlaygroundConfig,
  loadSessions,
  removeSession,
  renameSession as renameSessionInStore,
  saveConfig,
  saveParameterEnabled,
  saveSessions,
  setActiveSystemPrompt,
  switchSession as switchSessionInStore,
  updateActiveSessionMessages,
  type MessageStateUpdater,
  type SessionStore,
} from '../lib'
import type {
  Message,
  PlaygroundConfig,
  ParameterEnabled,
  ModelOption,
  GroupOption,
} from '../types'

const SESSION_SAVE_DEBOUNCE_MS = 500
const EMPTY_MESSAGES: Message[] = []

/**
 * Main state management hook for playground
 */
export function usePlaygroundState() {
  const { t } = useTranslation()
  // Load initial state from localStorage
  const [config, setConfig] = useState<PlaygroundConfig>(
    getInitialPlaygroundConfig
  )

  const [parameterEnabled, setParameterEnabled] = useState<ParameterEnabled>(
    getInitialParameterEnabled
  )

  const [store, setStore] = useState<SessionStore>(createSessionStore)
  const [isLoadingMessages, setIsLoadingMessages] = useState(true)
  const saveTimerRef = useRef<number | null>(null)
  const latestStoreRef = useRef<SessionStore>(store)
  const hasLoadedSessionsRef = useRef(false)
  const fileDropNoticeShownRef = useRef(false)

  const [models, setModels] = useState<ModelOption[]>([])
  const [groups, setGroups] = useState<GroupOption[]>([])

  const activeSession = useMemo(() => getActiveSession(store), [store])
  const messages = activeSession?.messages ?? EMPTY_MESSAGES

  const writeSessions = useCallback(
    (storeToSave: SessionStore) => {
      const result = saveSessions(storeToSave)
      if (result === 'saved-without-files' && !fileDropNoticeShownRef.current) {
        fileDropNoticeShownRef.current = true
        toast.warning(
          t(
            'Browser storage is full; images and PDFs were not saved to local history.'
          )
        )
      }
    },
    [t]
  )

  // Streaming updates arrive many times per second; persistence is debounced.
  // Explicit actions (switch, create, delete, rename) flush immediately.
  const persistSessions = useCallback(
    (storeToSave: SessionStore, immediate = false) => {
      latestStoreRef.current = storeToSave

      if (!hasLoadedSessionsRef.current) {
        return
      }

      if (saveTimerRef.current !== null) {
        window.clearTimeout(saveTimerRef.current)
        saveTimerRef.current = null
      }

      if (immediate) {
        writeSessions(storeToSave)
        return
      }

      saveTimerRef.current = window.setTimeout(() => {
        saveTimerRef.current = null
        writeSessions(latestStoreRef.current)
      }, SESSION_SAVE_DEBOUNCE_MS)
    },
    [writeSessions]
  )

  useEffect(() => {
    let cancelled = false

    window.setTimeout(() => {
      const loadedStore = loadSessions()
      if (cancelled) {
        return
      }

      latestStoreRef.current = loadedStore
      hasLoadedSessionsRef.current = true
      setStore(loadedStore)
      setIsLoadingMessages(false)
    }, 0)

    return () => {
      cancelled = true
    }
  }, [])

  // Flush pending changes when the page is hidden or the playground unmounts.
  useEffect(() => {
    const flush = () => {
      if (saveTimerRef.current !== null) {
        window.clearTimeout(saveTimerRef.current)
        saveTimerRef.current = null
        saveSessions(latestStoreRef.current)
      }
    }
    window.addEventListener('pagehide', flush)
    return () => {
      window.removeEventListener('pagehide', flush)
      flush()
    }
  }, [])

  const updateStore = useCallback(
    (
      updater: (previousStore: SessionStore) => SessionStore,
      immediate = false
    ) => {
      setStore((previousStore) => {
        const nextStore = updater(previousStore)
        if (nextStore !== previousStore) {
          persistSessions(nextStore, immediate)
        }
        return nextStore
      })
    },
    [persistSessions]
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

  // Update the active session's messages
  const updateMessages = useCallback(
    (updater: MessageStateUpdater) => {
      updateStore((previousStore) => {
        const previousMessages =
          getActiveSession(previousStore)?.messages ?? EMPTY_MESSAGES
        return updateActiveSessionMessages(
          previousStore,
          applyMessageStateUpdate(previousMessages, updater)
        )
      })
    },
    [updateStore]
  )

  // Clear the active session's messages (the session itself is kept)
  const clearMessages = useCallback(() => {
    updateStore(
      (previousStore) => updateActiveSessionMessages(previousStore, []),
      true
    )
  }, [updateStore])

  const createSession = useCallback(() => {
    updateStore(addSession, true)
  }, [updateStore])

  const switchSession = useCallback(
    (id: string) => {
      updateStore(
        (previousStore) => switchSessionInStore(previousStore, id),
        true
      )
    },
    [updateStore]
  )

  const deleteSession = useCallback(
    (id: string) => {
      updateStore((previousStore) => removeSession(previousStore, id), true)
    },
    [updateStore]
  )

  const renameSession = useCallback(
    (id: string, title: string) => {
      updateStore(
        (previousStore) => renameSessionInStore(previousStore, id, title),
        true
      )
    },
    [updateStore]
  )

  const setSystemPrompt = useCallback(
    (systemPrompt: string) => {
      updateStore((previousStore) =>
        setActiveSystemPrompt(previousStore, systemPrompt)
      )
    },
    [updateStore]
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
    isLoadingMessages,
    models,
    groups,
    sessions: store.sessions,
    activeSessionId: activeSession?.id ?? store.activeId,
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
