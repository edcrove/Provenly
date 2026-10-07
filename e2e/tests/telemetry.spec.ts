import { apiURL } from '../playwright.config'
import { byProperty, expect, junit, test, uniqueRunId } from '../support/fixtures'

test.describe('OpenTelemetry (DEC-11)', () => {
  test('[BE-E2E-023] every response names its trace and continues the caller\'s traceparent, ingestion included', async ({ request, provenly }) => {
    const traceId = '4bf92f3577b34da6a3ce929d0e0e4736'
    // An API route (not /healthz) so the journey also runs through a deployed UI origin that proxies /api.
    const listed = await request.get(`${apiURL}/api/v1/projects`, { headers: { traceparent: `00-${traceId}-00f067aa0ba902b7-01` } })
    expect(listed.headers()['x-trace-id']).toBe(traceId)

    const tc = await provenly.createTestCase({ title: 'traced', automated: true })
    const otherTrace = '0af7651916cd43dd8448eb211c80319c'
    const report = await request.post(`${apiURL}/api/v1/ingestion/junit?provider=github&runId=${uniqueRunId()}&runAttempt=1`, {
      headers: { 'Content-Type': 'application/xml', traceparent: `00-${otherTrace}-b7ad6b7169203331-01` },
      data: junit(byProperty('traced', tc.key)),
    })
    expect(report.status()).toBe(201)
    expect(report.headers()['x-trace-id']).toBe(otherTrace)

    const fresh = await request.get(`${apiURL}/api/v1/test-cases`)
    expect(fresh.headers()['x-trace-id']).toMatch(/^[0-9a-f]{32}$/)
    expect(fresh.headers()['x-trace-id']).not.toBe(traceId)
  })
})
