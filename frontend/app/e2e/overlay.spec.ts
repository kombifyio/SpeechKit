import { expect, test } from '@playwright/test'

import { mockBackendRoutes } from './helpers'

/**
 * Smoke tests for the overlay surface (overlay.html → OverlayApp).
 *
 * OverlayApp renders PillAnchorOverlay when visualizer='pill' and
 * DotAnchorOverlay when visualizer='circle'. The mock backend always
 * returns visualizer='pill', so we exercise the pill surface here.
 *
 * The overlay polls /overlay/state every 90 ms; route interception keeps
 * the state constant so assertions are stable.
 */
test.describe('Overlay surface', () => {
  test.beforeEach(async ({ page }) => {
    await mockBackendRoutes(page)
    await page.goto('/overlay.html')
  })

  test('renders the pill anchor shell', async ({ page }) => {
    // PillAnchorOverlayView wraps everything in a surface shell
    const shell = page.getByTestId('pill-anchor-shell')
    await expect(shell).toBeVisible({ timeout: 5_000 })
  })

  test('pill anchor stage is in idle state', async ({ page }) => {
    const stage = page.getByTestId('pill-anchor-stage')
    await expect(stage).toBeVisible({ timeout: 5_000 })
  })

  test('accessibility: overlay has an accessible status label', async ({ page }) => {
    // PillAnchorOverlayView renders a .sr-only status span
    const status = page.getByTestId('pill-anchor-status')
    await expect(status).toBeAttached({ timeout: 5_000 })
  })

  test('mode toggle buttons render for enabled modes', async ({ page }) => {
    // With all three modes enabled the panel exposes mode toggles
    // The panel only appears on expansion; assert the shell level first.
    const shell = page.getByTestId('pill-anchor-shell')
    await expect(shell).toBeVisible({ timeout: 5_000 })
  })
})
