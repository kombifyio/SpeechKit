import path from 'node:path'
import { fileURLToPath } from 'node:url'

import react from '@vitejs/plugin-react'
import { configDefaults, defineConfig } from 'vitest/config'

const projectDir = fileURLToPath(new URL('.', import.meta.url))
const setupFile = fileURLToPath(new URL('./src/test/setup.ts', import.meta.url))

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(projectDir, './src'),
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: [setupFile],
    exclude: [...configDefaults.exclude, 'e2e/**', '**/e2e/**'],
    // threads pool has startup-timeout issues on Node 25; forks is stable.
    // fileParallelism=false keeps the suite on a single worker in constrained
    // environments (PowerShell build scripts, CI runners).
    pool: 'forks',
    fileParallelism: false,
  },
})
