import type { KnipConfig } from 'knip';

const config: KnipConfig = {
  entry: [
    'src/main.tsx',
    'src/**/*.test.{ts,tsx}',
    'postcss.config.mjs',
    'public/sw.js',
  ],
  ignore: [
    'src/components/ui/**',
    'src/env.d.ts',
    'src/tanstack-table.d.ts',
    'src/i18n/static-keys.ts',
  ],
  ignoreDependencies: ['@tailwindcss/postcss', 'embla-carousel-react'],
};

export default config;
