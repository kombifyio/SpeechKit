import path from 'node:path'
import { fileURLToPath } from 'node:url'

import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

const projectDir = fileURLToPath(new URL('.', import.meta.url))
const setupFile = fileURLToPath(new URL('./src/test/setup.ts', import.meta.url))

// https://vite.dev/config/
export default defineConfig(() => {
  const isVitest = process.env.VITEST === 'true'

  return {
    plugins: [react(), ...(isVitest ? [] : [tailwindcss()])],
    resolve: {
      alias: {
        '@': path.resolve(projectDir, './src'),
      },
    },
    build: {
      outDir: '../../internal/frontendassets/dist',
      emptyOutDir: true,
      rollupOptions: {
        input: {
          overlay: path.resolve(projectDir, 'overlay.html'),
          pillAnchor: path.resolve(projectDir, 'pill-anchor.html'),
          pillPanel: path.resolve(projectDir, 'pill-panel.html'),
          dotAnchor: path.resolve(projectDir, 'dot-anchor.html'),
          dotRadial: path.resolve(projectDir, 'dot-radial.html'),
          assistBubble: path.resolve(projectDir, 'assist-bubble.html'),
          settings: path.resolve(projectDir, 'settings.html'),
          dashboard: path.resolve(projectDir, 'dashboard.html'),
          quicknote: path.resolve(projectDir, 'quicknote.html'),
          quickcapture: path.resolve(projectDir, 'quickcapture.html'),
          voiceagentPrompter: path.resolve(projectDir, 'voiceagent-prompter.html'),
        },
      },
    },
    test: {
      environment: 'jsdom',
      globals: true,
      setupFiles: [setupFile],
    },
  }
})
