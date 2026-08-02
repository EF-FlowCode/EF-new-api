// 一次性脚本：仅重新生成 src/routeTree.gen.ts，不跑完整 rsbuild build
// 用法（在 web 目录下）：bun scripts/regen-routes.mjs
import { Generator, getConfig } from '@tanstack/router-generator'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const root = path.resolve(here, '..')

const config = getConfig(
  {
    target: 'react',
    routesDirectory: path.join(root, 'src/routes'),
    generatedRouteTree: path.join(root, 'src/routeTree.gen.ts'),
  },
  root,
)

const gen = new Generator({ config, root })
await gen.run()
console.log('routeTree.gen.ts regenerated')
