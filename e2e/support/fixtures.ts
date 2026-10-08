import { randomUUID } from 'node:crypto'
import { mkdirSync, writeFileSync } from 'node:fs'
import path from 'node:path'

import { test as base, expect, type APIRequestContext, type Page } from '@playwright/test'

import { apiURL } from '../playwright.config'

const coverageDir = path.join(import.meta.dirname, '../coverage/frontend')

export interface TestCase {
  id: number
  key: string
  number: number
  projectKey: string
  title: string
  status: 'active' | 'deprecated'
  automated: boolean
}

/** Chooses a project in the header's switcher ('' for every project). */
export async function pickProject(page: Page, key: string) {
  await page.getByRole('button', { name: /^Current project: / }).click()
  await page
    .getByRole('listbox', { name: 'Projects' })
    .getByRole('option', { name: key ? new RegExp(`^${key} · `) : 'All projects' })
    .click()
}

/** A per-run random suffix: accounts the journeys create never have a password published in this repository. */
const runSecret = randomUUID()

/** The password of an account a journey creates (also when it runs against a deployed, public instance). */
export const secret = (label: string) => `${label} ${runSecret}`

/** Thin client of the public REST API, used for API journeys and for CI simulation. */
export class ProvenlyApi {
  constructor(readonly request: APIRequestContext) {}

  async createTestCase(body: {
    title: string
    automated?: boolean
    expectedResult?: string
    description?: string
    project?: string
    tags?: string[]
    classification?: Record<string, string>
  }) {
    const res = await this.request.post(`${apiURL}/api/v1/test-cases`, { data: body })
    expect(res.status()).toBe(201)
    return (await res.json()) as TestCase
  }

  async createProject(key: string, name: string) {
    const res = await this.request.post(`${apiURL}/api/v1/projects`, { data: { key, name } })
    expect(res.status()).toBe(201)
    return (await res.json()) as { id: number; key: string; name: string }
  }

  /** Deprecates every active automated test case so a journey controls the expected universe. */
  async isolateUniverse() {
    for (;;) {
      const res = await this.request.get(`${apiURL}/api/v1/test-cases?status=active&pageSize=100`)
      const page = (await res.json()) as { items: TestCase[] }
      const automated = page.items.filter((t) => t.automated)
      if (automated.length === 0) return
      for (const tc of automated) {
        expect((await this.request.post(`${apiURL}/api/v1/test-cases/${tc.id}/deprecate`)).status()).toBe(200)
      }
    }
  }

  /** Simulates CI sending one JUnit report for a run attempt. */
  ingest(runId: string, attempt: number, xml: string, extra: Record<string, string> = {}) {
    const params = new URLSearchParams({ provider: 'github', runId, runAttempt: String(attempt), pipeline: 'ci', branch: 'main', commit: 'c0ffee1234', ...extra })
    return this.request.post(`${apiURL}/api/v1/ingestion/junit?${params}`, {
      headers: { 'Content-Type': 'application/xml' },
      data: xml,
    })
  }
}

/** A project key no other execution uses (projects are never deleted). */
export const uniqueProjectKey = () => `P${randomUUID().replace(/-/g, '').slice(0, 8).toUpperCase()}`

/** A unique CI run id per test execution. */
export const uniqueRunId = () => `${Date.now()}${Math.floor(Math.random() * 1000)}`

/** Builds a JUnit XML document from testcase snippets (timestamp as Playwright's JUnit reporter writes it). */
export const junit = (...cases: string[]) =>
  `<?xml version="1.0" encoding="UTF-8"?><testsuites><testsuite name="e2e" timestamp="2026-09-28T10:00:00.000Z">${cases.join('')}</testsuite></testsuites>`

/** A testcase declaring its TC-ID through the tc-id property (primary source). */
export const byProperty = (name: string, ref: string, outcome = '') =>
  `<testcase name="${name}" classname="suite" time="0.42"><properties><property name="tc-id" value="${ref}"/></properties>${outcome}</testcase>`

/** A testcase declaring its TC-ID in its name (fallback). */
export const byName = (name: string, outcome = '') => `<testcase name="${name}" classname="suite" time="1.5">${outcome}</testcase>`

async function collectCoverage(page: Page) {
  const data = await page
    .evaluate(() => (window as unknown as { __coverage__?: unknown }).__coverage__)
    .catch(() => undefined)
  if (data) {
    mkdirSync(coverageDir, { recursive: true })
    writeFileSync(path.join(coverageDir, `${randomUUID()}.json`), JSON.stringify(data))
  }
}

export const test = base.extend<{ provenly: ProvenlyApi }>({
  provenly: async ({ request }, use) => {
    await use(new ProvenlyApi(request))
  },
  // Collects istanbul coverage of the instrumented frontend bundle (evidence only)
  // before every full navigation and at the end of each test.
  page: async ({ page }, use) => {
    const goto = page.goto.bind(page)
    page.goto = async (url, options) => {
      await collectCoverage(page)
      return goto(url, options)
    }
    await use(page)
    await collectCoverage(page)
  },
})

export { expect }
