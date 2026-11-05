import assert from 'node:assert/strict'
import { afterEach, beforeEach, describe, test } from 'node:test'
import {
  beginRun,
  classifyValue,
  createFadeState,
  endRun,
  FADE_DURATION_MS,
  FADE_HYDRATION_THRESHOLD,
  FADE_STAGGER_MAX_MS,
  FADE_STAGGER_MS,
  splitWords,
  stageRun,
} from '../response-fade'

const realNow = Object.getOwnPropertyDescriptor(performance, 'now')

function mockNow(value: number) {
  Object.defineProperty(performance, 'now', {
    configurable: true,
    value: () => value,
  })
}

function restoreNow() {
  if (realNow) {
    Object.defineProperty(performance, 'now', realNow)
  } else {
    delete (performance as { now?: unknown }).now
  }
}

function assertMatchObject(
  actual: object | undefined,
  expected: Record<string, unknown>
) {
  assert.ok(actual)
  for (const [key, value] of Object.entries(expected)) {
    assert.deepEqual((actual as Record<string, unknown>)[key], value, key)
  }
}

describe('splitWords', () => {
  test('round-trips ASCII words with trailing whitespace', () => {
    const value = 'Hello world,  stream.\n'
    assert.equal(splitWords(value).join(''), value)
  })

  test('keeps leading whitespace as its own part', () => {
    assert.deepEqual(splitWords('  hi'), ['  ', 'hi'])
  })

  test('segments CJK without spaces via Intl.Segmenter', () => {
    const value = '你好世界'
    const parts = splitWords(value)
    assert.equal(parts.join(''), value)
    assert.ok(parts.length > 1)
  })
})

describe('classifyValue', () => {
  beforeEach(() => {
    mockNow(1000)
  })

  afterEach(() => {
    restoreNow()
  })

  test('animates only newly appended words with capped stagger', () => {
    const state = createFadeState()
    const first = beginRun(state)
    const firstSegments = classifyValue(first, 'one two ')
    endRun(first)

    assert.equal(firstSegments.filter((s) => s.animated).length, 2)
    assert.equal(firstSegments[0]?.delay, 0)
    assert.equal(firstSegments[1]?.delay, FADE_STAGGER_MS)

    mockNow(1000 + FADE_DURATION_MS + FADE_STAGGER_MS + 1)
    const second = beginRun(state)
    const secondSegments = classifyValue(second, 'one two three four')
    endRun(second)

    const animated = secondSegments.filter((s) => s.animated)
    assert.deepEqual(
      animated.map((s) => s.value.trim()),
      ['three', 'four']
    )
    assert.equal(animated[0]?.delay, 0)
    assert.equal(animated[1]?.delay, FADE_STAGGER_MS)
  })

  test('caps stagger delay at FADE_STAGGER_MAX_MS', () => {
    const state = createFadeState()
    const run = beginRun(state)
    const words = Array.from({ length: 20 }, (_, i) => `w${i}`).join(' ')
    const segments = classifyValue(run, words)
    endRun(run)

    const delays = segments.filter((s) => s.animated).map((s) => s.delay)
    assert.equal(Math.max(...delays), FADE_STAGGER_MAX_MS)
  })

  test('replays identical delay while still inside the animation window', () => {
    const state = createFadeState()
    const first = beginRun(state)
    classifyValue(first, 'hello ')
    endRun(first)

    mockNow(1000 + FADE_DURATION_MS / 2)
    const second = beginRun(state)
    const segments = classifyValue(second, 'hello world')
    endRun(second)

    assertMatchObject(segments[0], {
      animated: true,
      delay: 0,
      start: 0,
      value: 'hello ',
    })
    assertMatchObject(segments[1], {
      animated: true,
      start: 6,
      value: 'world',
    })
  })

  test('keeps the same start offset when the head word grows', () => {
    const state = createFadeState()
    const first = beginRun(state)
    const head = classifyValue(first, 'hel')
    endRun(first)
    assert.equal(head[0]?.start, 0)

    mockNow(1050)
    const second = beginRun(state)
    const grown = classifyValue(second, 'hello')
    endRun(second)

    assert.equal(grown[0]?.start, 0)
    assert.equal(grown[0]?.animated, true)
    assert.equal(grown[0]?.value, 'hello')
  })

  test('does not animate whitespace-only parts', () => {
    const state = createFadeState()
    const run = beginRun(state)
    const segments = classifyValue(run, '  \n')
    endRun(run)

    assert.equal(
      segments.every((s) => !s.animated),
      true
    )
  })

  test('suppresses animation on the hydration baseline', () => {
    const state = createFadeState()
    const longText = 'a'.repeat(FADE_HYDRATION_THRESHOLD + 1)
    const run = beginRun(state, true)
    const segments = classifyValue(run, longText)
    endRun(run)

    assert.equal(
      segments.every((s) => !s.animated),
      true
    )
    assert.equal(state.prevCount, longText.length)
  })

  test('stops replaying animation after the window expires', () => {
    const state = createFadeState()
    const first = beginRun(state)
    classifyValue(first, 'done ')
    endRun(first)

    mockNow(1000 + FADE_DURATION_MS + 1)
    const second = beginRun(state)
    const segments = classifyValue(second, 'done next')
    endRun(second)

    assertMatchObject(segments[0], { animated: false, value: 'done ' })
    assertMatchObject(segments[1], { animated: true, value: 'next' })
    assert.equal(state.active.has(0), false)
  })

  test('abandoned staged runs leave committed state untouched', () => {
    const state = createFadeState()
    const first = beginRun(state)
    classifyValue(first, 'keep ')
    endRun(first)
    assert.equal(state.prevCount, 5)

    const abandoned = beginRun(state)
    classifyValue(abandoned, 'keep extra')
    stageRun(abandoned)
    // Never commit — simulate React discarding the render
    state.pending = null

    assert.equal(state.prevCount, 5)
    assert.equal(state.active.size, 1)
  })
})
