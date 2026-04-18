import { expect, test } from '@playwright/test'

import { mockBackendRoutes } from './helpers'

/**
 * Smoke tests for the Settings surface (settings.html → SettingsApp).
 *
 * SettingsApp loads settings state + model profiles on mount, then renders
 * a tabbed layout. We verify: page renders, the four tabs are present, and
 * navigating tabs shows the correct content without errors.
 */
test.describe('Settings surface', () => {
  test.beforeEach(async ({ page }) => {
    await mockBackendRoutes(page)
    await page.goto('/settings.html')
  })

  test('renders the Settings heading', async ({ page }) => {
    await expect(page.getByText('Settings')).toBeVisible({ timeout: 5_000 })
  })

  test('shows all four navigation tabs', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'General', exact: true })).toBeVisible({ timeout: 5_000 })
    await expect(page.getByRole('button', { name: 'Transcribe', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Assist', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Voice Agent', exact: true })).toBeVisible()
  })

  test('General tab is active by default', async ({ page }) => {
    // The General tab renders hotkey fields — spot-check one
    await expect(page.getByRole('button', { name: 'General' })).toBeVisible({ timeout: 5_000 })
    // Audio retention label is unique to General tab
    await expect(page.getByLabel('Audio retention')).toBeVisible()
  })

  test('navigating to Transcribe tab renders provider section', async ({ page }) => {
    await page.getByRole('button', { name: 'Transcribe' }).click()
    // The modality panel heading shows 'Speech-to-Text'
    await expect(page.getByText('Speech-to-Text')).toBeVisible({ timeout: 5_000 })
  })

  test('navigating to Assist tab renders provider section', async ({ page }) => {
    await page.getByRole('button', { name: 'Assist', exact: true }).click()
    await expect(page.getByText('Assist LLM')).toBeVisible({ timeout: 5_000 })
  })

  test('navigating to Voice Agent tab renders provider section', async ({ page }) => {
    await page.getByRole('button', { name: 'Voice Agent', exact: true }).click()
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
    await page.getByText('Settings').waitFor({ timeout: 5_000 })
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
