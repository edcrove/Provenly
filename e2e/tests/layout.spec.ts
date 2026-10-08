import { byProperty, expect, junit, pickProject, test, uniqueRunId } from '../support/fixtures'

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

test.describe('A project and its parts (deployed audit)', () => {
  test('[FE-E2E-031] a project opens from the projects page; its settings link to its pages and sections', async ({ page }) => {
    await page.goto('/projects')
    await page.getByTestId('project-TC').getByRole('link', { name: 'Default' }).click()
    await expect(page).toHaveURL(/\/dashboard$/)
    await expect(page.getByRole('button', { name: 'Current project: TC · Default' })).toBeVisible()

    await page.goto('/projects')
    await page.getByTestId('project-TC').getByRole('link', { name: 'Settings' }).click()
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('TC · Default — Settings')
    await page.getByRole('navigation', { name: 'On this page' }).getByRole('link', { name: 'Webhooks' }).click()
    await expect(page).toHaveURL(/\/projects\/TC#webhooks$/)
    await page.getByText('How CI reports runs here').click()
    await expect(page.getByTestId('ci-snippet')).toContainText('/api/v1/ingestion/junit?project=TC')
    await page.getByRole('navigation', { name: 'In this project' }).getByRole('link', { name: 'Issues' }).click()
    await expect(page).toHaveURL(/\/issues$/)
  })
})

test.describe('Phone tables (deployed audit)', () => {
  test.use({ viewport: { width: 390, height: 844 } })

  test('[FE-E2E-033] on a phone a wide table scrolls inside its card, keeps its first column and says so', async ({ page, provenly }) => {
    const tc = await provenly.createTestCase({ title: 'phone table', automated: true })
    const res = await provenly.ingest(uniqueRunId(), 1, junit(byProperty('phone table', tc.key, '<failure message="Expected 200 but got 500 from the payments service"/>')))
    const runId = ((await res.json()) as { testRun: { id: number } }).testRun.id
    await page.goto(`/test-runs/${runId}`)
    await expect(page.getByRole('table', { name: 'Results' })).toBeVisible()
    // Every table wider than its card scrolls inside it, keeps its first column and says so; the others say nothing.
    const tables = await page.evaluate(() =>
      [...document.querySelectorAll('[data-slot="table-container"]')].map((c) => ({
        overflows: c.scrollWidth > c.clientWidth + 1,
        hint: c.parentElement!.querySelector('[data-testid="table-scroll-hint"]')?.textContent ?? null,
        firstColumn: getComputedStyle(c.querySelector('tr > *')!).position,
      })),
    )
    expect(tables.some((t) => t.overflows)).toBe(true)
    for (const t of tables) {
      expect(t.hint).toBe(t.overflows ? 'Scroll sideways for more columns →' : null)
      expect(t.firstColumn).toBe(t.overflows ? 'sticky' : 'static')
    }
    // The page itself never scrolls sideways: only the table does.
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  })
})
