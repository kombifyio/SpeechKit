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
    coverage: {
      provider: 'v8',
      reporter: ['text', 'lcov'],
      reportsDirectory: './coverage',
      include: ['src/**/*.{ts,tsx}'],
      exclude: [
        ...configDefaults.coverage.exclude,
        'src/**/*.test.{ts,tsx}',
        'src/**/__tests__/**',
        'src/test/**',
        'src/*-entry.tsx',
        'src/components/agent-audio-visualizer-bar-livekit.tsx',
        'src/components/agent-control-bar.tsx',
        'src/components/agent-disconnect-button.tsx',
        'src/components/agent-track-control.tsx',
        'src/components/agent-track-toggle.tsx',
        'src/components/ai/**',
        'src/components/overlay-control-bar.tsx',
        'src/components/overlay-radial-menu.tsx',
        'src/components/ui/**',
        'src/components/animate-ui/**',
        'src/components/settings/settings-primitives.tsx',
        'src/hooks/use-agent-control-bar.ts',
        'src/hooks/use-auto-close.ts',
        'src/hooks/use-error-polling.ts',
        'src/hooks/use-logs.ts',
      ],
      thresholds: {
        statements: 70,
        functions: 70,
        lines: 70,
        branches: 60,
      },
    },
    // threads pool has startup-timeout issues on Node 25; forks is stable.
    // fileParallelism=false keeps the suite on a single worker in constrained
    // environments (PowerShell build scripts, CI runners).
    pool: 'forks',
    fileParallelism: false,
  },
})
