import { expect, pickProject, test } from '../support/fixtures'

test.describe('Phone layout (deployed audit)', () => {
  test.use({ viewport: { width: 390, height: 844 } })

  test('[FE-E2E-029] at phone width the forms fit the screen: no field runs past the right edge', async ({ page }) => {
    for (const path of ['/test-runs/manual', '/test-cases/new']) {
      await page.goto(path)
      const form = page.locator('main form').first()
      await expect(form).toBeVisible()
      // Every field ends inside its card (and the card inside the screen).
      const overflow = await page.evaluate(() =>
        [...document.querySelectorAll('main input, main select, main textarea')]
          .map((el) => {
            const card = el.closest('[data-slot="card"]')!.getBoundingClientRect()
            const box = el.getBoundingClientRect()
            return { id: el.id || el.getAttribute('aria-label'), right: Math.round(box.right), limit: Math.round(Math.min(card.right, document.documentElement.clientWidth)) }
          })
          .filter((f) => f.right > f.limit),
      )
      expect(overflow, path).toEqual([])
    }
  })
})

test.describe('Navigation (deployed audit)', () => {
  test('[FE-E2E-030] the header puts the project first, then its pages in working order, with Settings once one is chosen', async ({ page }) => {
    await page.goto('/test-runs')
    const pages = page.getByRole('navigation', { name: 'Project pages' })
    await pickProject(page, '')
    await expect(pages.getByRole('link')).toHaveText(['Dashboard', 'Test Runs', 'Test Cases', 'Suites', 'Requirements', 'Issues'])
    await expect(page.getByTestId('scope-label')).toHaveText('All projects')

    // The switcher finds a project by typing.
    await page.getByRole('button', { name: /^Current project: / }).click()
    await page.getByRole('combobox', { name: 'Find a project' }).fill('default')
    await page.keyboard.press('Enter')
    await expect(page.getByRole('button', { name: 'Current project: TC · Default' })).toBeVisible()
    await expect(page.getByTestId('scope-label')).toHaveText('TC · Default')
    await pages.getByRole('link', { name: 'Settings' }).click()
    await expect(page).toHaveURL(/\/projects\/TC$/)
  })

  test('[FE-E2E-030] on a phone the header is one row and the pages are behind Menu', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 812 })
    await page.goto('/dashboard')
    const header = page.getByRole('banner')
    await expect(page.getByRole('button', { name: 'Menu' })).toBeVisible()
    expect((await header.boundingBox())!.height).toBeLessThanOrEqual(57)
    await page.getByRole('button', { name: 'Menu' }).click()
    await page.locator('#main-menu').getByRole('link', { name: 'Test Runs' }).click()
    await expect(page).toHaveURL(/\/test-runs$/)
    await expect(page.locator('#main-menu')).toHaveCount(0)
  })
})
