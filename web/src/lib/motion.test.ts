import assert from 'node:assert/strict'
import { test } from 'node:test'
import { MOTION_VARIANTS } from './motion'

test('页面入场动画不以零半径 blur 收尾，结束后释放 filter', () => {
  const { animate } = MOTION_VARIANTS.pageEnter
  assert.notEqual(animate.filter, 'blur(0px)')
  assert.notEqual(animate.filter, 'blur(0)')
  assert.match(animate.filter, /^blur\(0\.\d+px\)$/)
  assert.deepEqual(animate.transitionEnd, { filter: 'none' })
})
