import path from 'path'
import { defineConfig, loadEnv } from '@rsbuild/core'
import { pluginReact } from '@rsbuild/plugin-react'
import { tanstackRouter } from '@tanstack/router-plugin/rspack'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'url'
import { ResourceRecoveryPlugin } from './resource-recovery-plugin'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

export default defineConfig(({ envMode }) => {
  const env = loadEnv({ mode: envMode, prefixes: ['VITE_'] })
  const serverUrl =
    process.env.VITE_REACT_APP_SERVER_URL ||
    env.rawPublicVars.VITE_REACT_APP_SERVER_URL ||
    'http://localhost:3000'

  const isProd = envMode === 'production'
  const resourceRecoveryMessages = Object.fromEntries(
    ['en', 'zh', 'fr', 'ja', 'ru', 'vi'].map((locale) => {
      const { translation } = JSON.parse(
        readFileSync(
          path.join(__dirname, `src/i18n/locales/${locale}.json`),
          'utf8'
        )
      )
      const keys = [
        'Resource connection offline',
        'Page loading is taking longer than expected',
        'Page resources could not be loaded',
        'Loading will resume when your connection returns.',
        'Check your connection and try again. Refreshing may discard unsaved changes.',
        'Retry loading',
        'Refresh page',
      ]
      return [
        locale,
        Object.fromEntries(keys.map((key) => [key, translation[key] || key])),
      ]
    })
  )
  const resourceRecoveryScript = readFileSync(
    path.join(__dirname, 'bootstrap/resource-recovery.js'),
    'utf8'
  ).replace(
    '__RESOURCE_RECOVERY_MESSAGES__',
    JSON.stringify(resourceRecoveryMessages).replaceAll('<', '\\u003c')
  )
  const devProxy = Object.fromEntries(
    (['/api', '/v1', '/mj', '/pg'] as const).map((key) => [
      key,
      { target: serverUrl, changeOrigin: true },
    ])
  ) as Record<string, { target: string; changeOrigin: boolean }>

  return {
    plugins: [pluginReact()],
    // Rsbuild 2: replaces deprecated `performance.chunkSplit` (RSPack 2 aligned)
    splitChunks: {
      preset: 'default',
      cacheGroups: {
        'vendor-react': {
          test: /node_modules[\\/](react|react-dom)[\\/]/,
          name: 'vendor-react',
          chunks: 'all',
          priority: 0,
          enforce: true,
        },
        'vendor-radix': {
          test: /node_modules[\\/]@radix-ui[\\/]/,
          name: 'vendor-radix',
          chunks: 'all',
          priority: 0,
          enforce: true,
        },
        'vendor-tanstack': {
          test: /node_modules[\\/]@tanstack[\\/]/,
          name: 'vendor-tanstack',
          chunks: 'all',
          priority: 0,
          enforce: true,
        },
      },
    },
    source: {
      entry: {
        index: './src/main.tsx',
      },
    },
    resolve: {
      alias: {
        '@': path.resolve(__dirname, './src'),
      },
    },
    html: {
      template: './index.html',
      templateParameters: { resourceRecoveryScript },
    },
    server: {
      host: '0.0.0.0',
      proxy: devProxy,
    },
    output: {
      // Production optimizations
      minify: isProd,
      target: 'web',
      distPath: {
        root: 'dist',
      },
      // Rely on Rsbuild default legalComments ("linked" → per-chunk *.LICENSE.txt) in all modes.
      // Do not set "none" in production: that strips minifier-preserved third-party notices and
      // extracted license files, which some distributions require for open-source compliance.
    },
    performance: {
      // Remove console in production
      removeConsole: isProd ? ['log'] : false,
      // Speed up repeated `rsbuild build` (local + CI when node_modules/.cache is preserved).
      // @see https://v2.rsbuild.dev/config/performance/build-cache
      buildCache: {
        cacheDigest: [process.env.VITE_REACT_APP_VERSION],
      },
    },
    tools: {
      rspack: {
        plugins: [
          new ResourceRecoveryPlugin(),
          tanstackRouter({
            target: 'react',
            // Dev: avoid per-route async chunks (reduces white flash on navigation + faster HMR feedback).
            // Prod: keep route-based code splitting.
            autoCodeSplitting: isProd,
          }),
        ],
      },
    },
  }
})
