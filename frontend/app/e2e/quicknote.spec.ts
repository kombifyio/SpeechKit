import { expect, test } from '@playwright/test'

import { mockBackendRoutes } from './helpers'

/**
 * Smoke tests for the Quick Note surface (quicknote.html → QuickNoteApp).
 *
 * QuickNoteApp is a lightweight note editor with an auto-focusing textarea
 * and a toolbar (Save / Record / Summary / Email). When opened without
 * ?id=N it starts with an empty note. With ?id=N it fetches the note text
 * from /quicknotes/get?id=N.
 */
test.describe('QuickNote surface', () => {
  test.beforeEach(async ({ page }) => {
    await mockBackendRoutes(page)
    await page.goto('/quicknote.html')
  })

  test('renders the note textarea', async ({ page }) => {
    const textarea = page.locator('textarea')
    await expect(textarea).toBeVisible({ timeout: 5_000 })
    await expect(textarea).toHaveAttribute('placeholder', 'Start typing your note...')
  })

  test('textarea receives focus on load', async ({ page }) => {
    const textarea = page.locator('textarea')
    await expect(textarea).toBeFocused({ timeout: 3_000 })
  })

  test('toolbar buttons are visible', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'Save' })).toBeVisible({ timeout: 5_000 })
    await expect(page.getByRole('button', { name: 'Record' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Summary' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Email' })).toBeVisible()
  })

  test('word count updates as user types', async ({ page }) => {
    const textarea = page.locator('textarea')
    await textarea.fill('hello world test')
    await expect(page.getByText('3 words')).toBeVisible({ timeout: 3_000 })
  })

  test('loads existing note text when opened with ?id=N', async ({ page }) => {
    // Override the quicknotes/get mock to return a pre-existing note
    await page.route('**/quicknotes/get**', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ text: 'Pre-existing note content' }),
      }),
    )
    await page.goto('/quicknote.html?id=42')
    const textarea = page.locator('textarea')
    await expect(textarea).toHaveValue('Pre-existing note content', { timeout: 5_000 })
  })

  test('Save button triggers POST to /quicknotes/create for new notes', async ({ page }) => {
    let captured = false
    await page.route('**/quicknotes/create', (route) => {
      captured = true
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ id: 1, message: 'saved' }),
      })
    })

    const textarea = page.locator('textarea')
    await textarea.fill('My new note')
    await page.getByRole('button', { name: 'Save' }).click()
    await page.waitForTimeout(200)
    expect(captured).toBe(true)
  })
})
