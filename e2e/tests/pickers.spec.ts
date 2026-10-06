import { apiURL } from '../playwright.config'
import { expect, test, uniqueProjectKey, type ProvenlyApi, type TestCase } from '../support/fixtures'

/** n test cases titled "Checkout step i", created ten at a time (so their numbers need not follow i), by number. */
async function manyCases(provenly: ProvenlyApi, project: string, n: number) {
  const out: TestCase[] = []
  for (let i = 0; i < n; i += 10) {
    const batch = Array.from({ length: Math.min(10, n - i) }, (_, j) =>
      provenly.createTestCase({ title: `Checkout step ${i + j}`, project, automated: true }),
    )
    out.push(...(await Promise.all(batch)))
  }
  return out.sort((a, b) => a.number - b.number)
}

test.describe('Long lists (DEC-78)', () => {
  test('[BE-E2E-029] catalog lists are paged and test cases are searched by key or title through the public API', async ({ request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Long lists API')
    const cases = await manyCases(provenly, key, 105)
    const base = `${apiURL}/api/v1/projects/${key}/requirements`
    for (let i = 0; i < 3; i++) expect((await request.post(base, { data: { title: `Requirement ${i}` } })).status()).toBe(201)

    const page = await (await request.get(`${base}?page=2&pageSize=2`)).json()
    expect(page).toMatchObject({ page: 2, pageSize: 2, totalItems: 3, totalPages: 2, coverageCounts: { uncovered: 3 } })
    expect(page.items).toHaveLength(1)
    expect((await request.get(`${base}?pageSize=101`)).status()).toBe(400)

    const target = cases[100]
    const byKey = await (await request.get(`${apiURL}/api/v1/test-cases?project=${key}&q=${target.key.toLowerCase()}`)).json()
    expect(byKey.items.map((t: TestCase) => t.key)).toEqual([target.key])
    const byTitle = await (await request.get(`${apiURL}/api/v1/test-cases?project=${key}&q=STEP%20104`)).json()
    expect(byTitle.items.map((t: TestCase) => t.title)).toEqual(['Checkout step 104'])
    expect((await request.get(`${apiURL}/api/v1/test-cases?project=${key}&q=%20`)).status()).toBe(400)
  })

  test('[FE-E2E-027] with more than 100 test cases the pickers search the server, say how many match and show keys, never ids', async ({ page, request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Long lists UI')
    const cases = await manyCases(provenly, key, 105)
    const created = await request.post(`${apiURL}/api/v1/projects/${key}/requirements`, { data: { title: 'Checkout' } })
    expect(created.status()).toBe(201)
    const requirement = await created.json()

    await page.goto(`/requirements/${key}/${requirement.id}`)
    await expect(page.getByTestId('picker-more')).toHaveText('Showing 50 of 105, type to search.')
    const last = cases[104]
    const titled = (i: number) => cases.find((c) => c.title === `Checkout step ${i}`)!
    await page.getByLabel('Search: Test case to link').fill(last.key.toLowerCase())
    await expect(page.getByLabel('Test case to link', { exact: true }).getByRole('option')).toHaveCount(2)
    await expect(page.getByTestId('picker-more')).toHaveCount(0)
    await page.getByLabel('Test case to link', { exact: true }).selectOption({ label: `${last.key} · ${last.title}` })
    await page.getByRole('button', { name: 'Link test case' }).click()
    await expect(page.getByTestId(`covering-${last.id}`)).toContainText(last.key)
    await expect(page.getByText(`#${last.id}`)).toHaveCount(0)

    // The suite picker searches by title as well.
    expect((await request.post(`${apiURL}/api/v1/projects/${key}/suites`, { data: { key: 'release', name: 'Release', kind: 'static', testCaseIds: [] } })).status()).toBe(201)
    await page.goto(`/suites/${key}/release`)
    await page.getByLabel('Search: Test case to add').fill('step 101')
    await page.getByLabel('Test case to add', { exact: true }).selectOption({ label: `${titled(101).key} · Checkout step 101` })
    await page.getByRole('button', { name: 'Add to suite' }).click()
    await expect(page.getByTestId(`suite-case-${titled(101).key}`)).toBeVisible()
  })
})
