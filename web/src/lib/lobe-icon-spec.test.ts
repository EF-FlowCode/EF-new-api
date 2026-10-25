import assert from 'node:assert/strict'
import { existsSync, readFileSync } from 'node:fs'
import path from 'node:path'
import { test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { LOBE_ICON_VARIANTS, parseLobeIconSpec } from './lobe-icon-spec'

const known = new Set(['OpenAI', 'Claude', 'Gemma', 'LobeHub'])
const isKnown = (id: string) => known.has(id)

test('基础名与变体', () => {
  assert.deepEqual(parseLobeIconSpec('OpenAI', 20, isKnown), {
    baseKey: 'OpenAI',
    variant: 'Mono',
    props: { size: 20 },
  })
  assert.deepEqual(parseLobeIconSpec(' Claude.Color ', 16, isKnown), {
    baseKey: 'Claude',
    variant: 'Color',
    props: { size: 16 },
  })
  assert.equal(
    parseLobeIconSpec('Gemma.Simple', 24, isKnown)?.variant,
    'Simple'
  )
  assert.equal(
    parseLobeIconSpec('LobeHub.Morden', 24, isKnown)?.variant,
    'Morden'
  )
})

test('链式属性与 size 覆盖', () => {
  assert.deepEqual(
    parseLobeIconSpec(
      `Claude.Avatar.type={'platform'}.size={32}.shape="square".active`,
      20,
      isKnown
    ),
    {
      baseKey: 'Claude',
      variant: 'Avatar',
      props: { type: 'platform', size: 32, shape: 'square', active: true },
    }
  )
  // 第二段小写开头时是属性，不是变体
  assert.deepEqual(parseLobeIconSpec('OpenAI.flag=false', 14, isKnown), {
    baseKey: 'OpenAI',
    variant: 'Mono',
    props: { flag: false, size: 14 },
  })
})

test('未知变体回退到基础图标，未知图标返回 null', () => {
  const spec = parseLobeIconSpec('OpenAI.Unknown.role="img"', 20, isKnown)
  assert.equal(spec?.variant, 'Mono')
  assert.deepEqual(spec?.props, { role: 'img', size: 20 })
  assert.equal(parseLobeIconSpec('NotAnInstalledIcon', 20, isKnown), null)
  assert.equal(parseLobeIconSpec('', 20, isKnown), null)
})

test('变体白名单与已安装 @lobehub/icons 的目录结构一致', () => {
  const pkg = path.resolve(
    path.dirname(fileURLToPath(import.meta.url)),
    '../../node_modules/@lobehub/icons/es'
  )
  const toc = readFileSync(path.join(pkg, 'toc.js'), 'utf8')
  assert.match(toc, /export default toc/)
  for (const id of ['OpenAI', 'Claude']) {
    assert.ok(existsSync(path.join(pkg, id, 'components', 'Mono.js')))
  }
  assert.ok(existsSync(path.join(pkg, 'Claude', 'components', 'Color.js')))
  assert.ok(existsSync(path.join(pkg, 'Gemma', 'components', 'Simple.js')))
  assert.ok(existsSync(path.join(pkg, 'LobeHub', 'components', 'Morden.js')))
  assert.ok(LOBE_ICON_VARIANTS.includes('Mono'))
})
