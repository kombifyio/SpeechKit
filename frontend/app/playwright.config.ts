import { defineConfig, devices } from '@playwright/test'

/**
 * Playwright E2E configuration for SpeechKit frontend surfaces.
 *
 * The frontend communicates with the Go backend exclusively via fetch()
 * calls against a local HTTP server. In E2E tests we start the Vite dev
 * server and intercept those routes via page.route() so tests run without
 * a running Go process.
 *
 * Run:
 *   npx playwright test          # all specs
 *   npx playwright test --ui     # interactive UI mode
 *
 * First-time browser install:
 *   npx playwright install chromium
 */
export default defineConfig({
  testDir: './e2e',
  timeout: 30_000,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: process.env.CI ? 'github' : 'list',

  use: {
    baseURL: 'http://localhost:5174',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },

  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],

  webServer: {
    command: 'npx vite --port 5174',
    url: 'http://localhost:5174/overlay.html',
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
    stdout: 'ignore',
    stderr: 'pipe',
  },
})
