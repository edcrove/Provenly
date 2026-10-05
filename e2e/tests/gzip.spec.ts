import { gzipSync } from 'node:zlib'

import { apiURL } from '../playwright.config'
import { byProperty, expect, junit, test, uniqueRunId } from '../support/fixtures'

test.describe('Compressed reports (D6)', () => {
  test('[BE-E2E-014] CI sends a gzip report: it is ingested like a plain one; the size limit applies decompressed', async ({ request, provenly }) => {
    const tc = await provenly.createTestCase({ title: 'gzip upload', automated: true })
    const send = (body: Buffer, encoding = 'gzip') =>
      request.post(`${apiURL}/api/v1/ingestion/junit?provider=github&runId=${uniqueRunId()}&runAttempt=1`, {
        headers: { 'Content-Type': 'application/xml', 'Content-Encoding': encoding },
        data: body,
      })
    const res = await send(gzipSync(junit(byProperty('zipped', tc.key))))
    expect(res.status()).toBe(201)
    expect(await res.json()).toMatchObject({ received: 1, persisted: 1, diagnostics: [] })

    const bomb = await send(gzipSync(Buffer.alloc(32 << 20, ' ')))
    expect(bomb.status()).toBe(413)
    expect((await bomb.json()).code).toBe('payload_too_large')
    expect((await send(Buffer.from('not gzip'))).status()).toBe(400)
    expect((await send(Buffer.from(junit()), 'br')).status()).toBe(415)
  })
})
