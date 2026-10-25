import assert from 'node:assert/strict'
import { readdirSync, readFileSync, statSync } from 'node:fs'
import path from 'node:path'
import { test } from 'node:test'
import { fileURLToPath } from 'node:url'

const SRC = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')

function sourceFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const full = path.join(dir, name)
    if (statSync(full).isDirectory()) return sourceFiles(full)
    return /\.(ts|tsx)$/.test(name) && !/\.test\.tsx?$/.test(name) ? [full] : []
  })
}

// react-icons/si 是单个约 5 MB 的模块，命名导入几个图标也会把整包（约 3400 个图标）
// 打进所在路由的 chunk。品牌图标统一用 @/assets/brand-icons。
test('页面代码不静态导入整包 Simple Icons', () => {
  const offenders = sourceFiles(SRC).filter((file) =>
    /from\s+['"]react-icons\/si['"]/.test(readFileSync(file, 'utf8'))
  )
  assert.deepEqual(
    offenders.map((file) => path.relative(SRC, file)),
    []
  )
})
