import { apiURL } from '../playwright.config'
import { byProperty, expect, junit, pickProject, test, uniqueProjectKey, uniqueRunId } from '../support/fixtures'

const failure = '<failure message="boom">trace</failure>'
const skipped = '<skipped/>'

test.describe('Issues and verification (DEC-8)', () => {
  test('[BE-E2E-019] issue verification through the public API follows the issue state and the latest conclusive results', async ({ request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Issues API')
    const login = await provenly.createTestCase({ title: 'login', project: key, automated: true })
    const base = `${apiURL}/api/v1/projects/${key}/issues`
    const report = (...cases: string[]) =>
      request.post(`${apiURL}/api/v1/ingestion/junit?project=${key}&provider=github&runId=${uniqueRunId()}&runAttempt=1`, {
        headers: { 'Content-Type': 'application/xml' },
        data: junit(...cases),
      })
    const verification = async (id: number) => (await (await request.get(`${base}/${id}`)).json()).verification

    const native = await request.post(base, { data: { title: 'Sign-in loops' } })
    expect(native.status()).toBe(201)
    expect(await native.json()).toMatchObject({ externalId: 'I-1', state: 'open', verification: { status: 'unlinked' } })
    const imported = await request.post(`${base}/import`, {
      data: { provider: 'jira', items: [{ externalId: 'AUTH-9', title: 'Login rejects valid passwords', state: 'open', providerStatus: 'In progress' }] },
    })
    expect(await imported.json()).toEqual({ created: 1, updated: 0 })
    const jira = (await (await request.get(`${base}?state=open`)).json()).items.find((i: { externalId: string }) => i.externalId === 'AUTH-9')
    expect((await request.put(`${base}/${jira.id}/test-cases`, { data: { testCaseIds: [login.id] } })).status()).toBe(200)
    expect((await verification(jira.id)).status).toBe('unverified')

    const failed = await report(byProperty('login', login.key, failure))
    const failedRun = (await failed.json()).testRun.id
    expect(await verification(jira.id)).toMatchObject({ status: 'known_issue', testCases: [{ evidence: 'failed', evidenceRunId: failedRun }] })
    await report(byProperty('login', login.key, skipped))
    expect(await verification(jira.id)).toMatchObject({ status: 'known_issue', testCases: [{ evidenceRunId: failedRun, latestInconclusive: true }] })

    const closed = await request.post(`${base}/import`, { data: { provider: 'jira', items: [{ externalId: 'AUTH-9', title: 'Login rejects valid passwords', state: 'closed' }] } })
    expect(await closed.json()).toEqual({ created: 0, updated: 1 })
    expect((await verification(jira.id)).status).toBe('reopen')
    await report(byProperty('login', login.key))
    expect((await verification(jira.id)).status).toBe('validated_fixed')
    const reopened = await request.patch(`${base}/${jira.id}`, { data: { state: 'open' } })
    expect(await reopened.json()).toMatchObject({ closedAt: null, verification: { status: 'not_reproducible' } })

    expect((await (await request.get(`${base}?testCase=${login.id}`)).json()).items).toHaveLength(1)
    expect((await request.get(`${base}?state=done`)).status()).toBe(400)
    expect((await request.post(`${base}/import`, { data: { provider: 'jira', items: [{ externalId: 'X-1', title: 'x' }] } })).status()).toBe(400)
    expect((await request.post(base, { data: { provider: 'jira', externalId: 'AUTH-9', title: 'dup' } })).status()).toBe(409)
    expect((await request.get(`${base}/999999999`)).status()).toBe(404)
  })

  test('[FE-E2E-021] a member reports an issue, links the failing test, sees the run flag it as known, closes it and sees it validated after the fix', async ({ page, request, provenly }) => {
    const key = uniqueProjectKey()
    await provenly.createProject(key, 'Issues UI')
    const login = await provenly.createTestCase({ title: 'Sign in works', project: key, automated: true })
    const report = async (outcome: string) => {
      const res = await request.post(`${apiURL}/api/v1/ingestion/junit?project=${key}&provider=github&runId=${uniqueRunId()}&runAttempt=1`, {
        headers: { 'Content-Type': 'application/xml' },
        data: junit(byProperty('sign in', login.key, outcome)),
      })
      expect(res.status()).toBe(201)
      return (await res.json()).testRun.id as number
    }
    const run = await report(failure)

    await page.goto(`/test-runs/${run}`)
    await expect(page.getByTestId('run-new-failures')).toContainText(login.key)
    await pickProject(page, key)
    await page.goto('/issues')
    await page.getByLabel('Title').fill('Sign-in rejects valid passwords')
    await page.getByRole('button', { name: 'Add issue' }).click()
    await page.getByRole('link', { name: 'I-1' }).click()
    await page.getByLabel('Test case to link', { exact: true }).selectOption({ label: `${login.key} · Sign in works` })
    await page.getByRole('button', { name: 'Link test case' }).click()
    await expect(page.getByTestId('verification-badge')).toHaveText('Known issue')
    await expect(page.getByTestId(`reproducing-${login.id}`)).toContainText(`run #${run}`)

    await page.goto(`/test-runs/${run}`)
    await expect(page.getByTestId('run-known-issues')).toContainText(`Known issue: ${login.key} fails with I-1`)
    await page.getByRole('link', { name: 'I-1' }).click()
    await page.getByRole('button', { name: 'Close issue' }).click()
    await expect(page.getByTestId('verification-badge')).toHaveText('Reopen candidate')
    await report('')
    await page.reload()
    await expect(page.getByTestId('verification-badge')).toHaveText('Validated fixed')
    await page.getByRole('link', { name: login.key }).click()
    await expect(page.getByTestId('linked-issues')).toContainText('Validated fixed')
    await pickProject(page, '')
  })
})
