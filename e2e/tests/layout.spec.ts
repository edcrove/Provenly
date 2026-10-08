import { expect, test } from '../support/fixtures'

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
