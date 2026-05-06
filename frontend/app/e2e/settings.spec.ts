import { expect, test } from '@playwright/test'

import { mockBackendRoutes } from './helpers'

/**
 * Smoke tests for the Settings surface (settings.html → SettingsApp).
 *
 * SettingsApp loads settings state + model profiles on mount, then renders
 * a tabbed layout. We verify: page renders, the seven tabs are present, and
 * navigating tabs shows the correct content without errors.
 */
test.describe('Settings surface', () => {
  test.beforeEach(async ({ page }) => {
    await mockBackendRoutes(page)
    await page.goto('/settings.html')
  })

  test('renders the Settings heading', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible({ timeout: 5_000 })
  })

  test('shows all seven navigation tabs', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'General', exact: true })).toBeVisible({ timeout: 5_000 })
    await expect(page.getByRole('button', { name: 'Integrations', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'SpeechKit Server', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Storage & Data', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Transcribe Mode', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Assist Mode', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Voice Agent Mode', exact: true })).toBeVisible()
  })

  test('General tab is active by default', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'General' })).toBeVisible({ timeout: 5_000 })
    await expect(page.getByText('Startup')).toBeVisible()
    await expect(page.getByText('Microphone', { exact: true })).toBeVisible()
    await expect(page.getByText('Overlay', { exact: true })).toBeVisible()
    await expect(page.getByLabel('Audio retention')).toBeHidden()
  })

  test('navigating to Integrations tab renders provider setup', async ({ page }) => {
    await page.getByRole('button', { name: 'Integrations', exact: true }).click()
    await expect(page.getByText('Optional providers appear here')).toBeVisible({ timeout: 5_000 })
  })

  test('navigating to SpeechKit Server tab renders server target and mode source', async ({ page }) => {
    await page.getByRole('button', { name: 'SpeechKit Server', exact: true }).click()
    await expect(page.getByText('Server Target')).toBeVisible({ timeout: 5_000 })
    await expect(page.getByText('Mode Source')).toBeVisible({ timeout: 5_000 })
  })

  test('navigating to Storage & Data tab renders storage settings', async ({ page }) => {
    await page.getByRole('button', { name: 'Storage & Data', exact: true }).click()
    await expect(page.getByLabel('Audio retention')).toBeVisible()
  })

  test('navigating to Transcribe tab renders provider section', async ({ page }) => {
    await page.getByRole('button', { name: 'Transcribe Mode' }).click()
    // The modality panel heading shows 'Speech-to-Text'
    await expect(page.getByText('Speech-to-Text')).toBeVisible({ timeout: 5_000 })
  })

  test('navigating to Assist tab renders provider section', async ({ page }) => {
    await page.getByRole('button', { name: 'Assist Mode', exact: true }).click()
    await expect(page.getByText('Assist LLM')).toBeVisible({ timeout: 5_000 })
  })

  test('navigating to Voice Agent tab renders provider section', async ({ page }) => {
    await page.getByRole('button', { name: 'Voice Agent Mode', exact: true }).click()
    // The Voice Agent tab heading appears inside the modality panel
    await expect(page.locator('text=Voice Agent').first()).toBeVisible({ timeout: 5_000 })
  })

  test('no unhandled console errors on load', async ({ page }) => {
    const errors: string[] = []
    page.on('console', (msg) => {
      if (msg.type() === 'error') errors.push(msg.text())
    })
    // Re-navigate to capture errors during the full load cycle
    await page.goto('/settings.html')
    await page.getByRole('heading', { name: 'Settings' }).waitFor({ timeout: 5_000 })
    // Filter out expected non-fatal noise: React DevTools hint, Vite HMR,
    // and 404s from unmocked action endpoints (we only care about JS errors)
    const fatal = errors.filter(
      (e) =>
        !e.includes('Download the React DevTools') &&
        !e.includes('[vite]') &&
        !e.includes('[HMR]') &&
        !e.includes('404') &&
        !e.includes('Failed to load resource'),
    )
    expect(fatal).toHaveLength(0)
  })
})
