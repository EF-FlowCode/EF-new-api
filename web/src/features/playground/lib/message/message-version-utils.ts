import { MESSAGE_ROLES, MESSAGE_STATUS } from '../../constants'
import type { Message, MessageAnswerState, MessageVersion } from '../../types'
import { createMessageVersion, getActiveVersionIndex } from './message-utils'

// Fields that describe one answer. The active version's state lives on the
// message itself; inactive versions keep theirs in `version.state`.
const ANSWER_STATE_KEYS = [
  'createdAt',
  'startedAt',
  'completedAt',
  'durationMs',
  'sources',
  'reasoning',
  'isReasoningStreaming',
  'isReasoningComplete',
  'isContentComplete',
  'status',
  'errorCode',
] as const satisfies readonly (keyof MessageAnswerState)[]

function pickAnswerState(message: Message): MessageAnswerState {
  const state: MessageAnswerState = {}
  for (const key of ANSWER_STATE_KEYS) {
    if (message[key] !== undefined) {
      Object.assign(state, { [key]: message[key] })
    }
  }
  return state
}

function withoutAnswerState(message: Message): Message {
  const next = { ...message }
  for (const key of ANSWER_STATE_KEYS) {
    delete next[key]
  }
  return next
}

function stripVersionState(version: MessageVersion): MessageVersion {
  if (!version.state) return version
  const { state: _state, ...rest } = version
  return rest
}

/**
 * A failed or empty answer is replaced rather than kept as a version.
 */
function isWorthKeeping(message: Message, version: MessageVersion): boolean {
  return (
    message.status !== MESSAGE_STATUS.ERROR && version.content.trim() !== ''
  )
}

/**
 * Start a new answer on an assistant message, keeping earlier answers as
 * versions the user can switch back to.
 */
export function appendRegeneratedVersion(
  message: Message,
  startedAt: number = Date.now()
): Message {
  const activeIndex = getActiveVersionIndex(message)
  const active = message.versions[activeIndex]
  const archived = message.versions.map((version, index) =>
    index === activeIndex
      ? { ...version, state: pickAnswerState(message) }
      : version
  )
  const kept =
    active && isWorthKeeping(message, active)
      ? archived
      : archived.filter((_, index) => index !== activeIndex)
  const versions = [...kept, createMessageVersion('')]

  return {
    ...withoutAnswerState(message),
    versions,
    activeVersion: versions.length - 1,
    createdAt: startedAt,
    startedAt,
    isReasoningComplete: false,
    isContentComplete: false,
    isReasoningStreaming: false,
    status: MESSAGE_STATUS.LOADING,
  }
}

/**
 * Show another version of an assistant message.
 */
export function switchMessageVersion(message: Message, index: number): Message {
  const activeIndex = getActiveVersionIndex(message)
  if (index === activeIndex || index < 0 || index >= message.versions.length) {
    return message
  }

  const target = message.versions[index]
  const versions = message.versions.map((version, versionIndex) => {
    if (versionIndex === activeIndex) {
      return { ...version, state: pickAnswerState(message) }
    }
    if (versionIndex === index) return stripVersionState(version)
    return version
  })

  return {
    ...withoutAnswerState(message),
    ...target.state,
    versions,
    activeVersion: index,
  }
}

export function canSwitchVersions(message: Message): boolean {
  return message.from === MESSAGE_ROLES.ASSISTANT && message.versions.length > 1
}
