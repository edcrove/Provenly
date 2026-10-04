import { afterEach, describe, expect, it, vi } from 'vitest'

import { api, createApiClient } from './client'

afterEach(() => vi.unstubAllGlobals())

describe('createApiClient', () => {
  it('builds typed requests and resolves fetch at call time', async () => {
    const fetchMock = vi.fn(async (req: Request) => {
      expect(req.url).toBe('http://api.test/api/v1/test-cases/153')
      return new Response(JSON.stringify({ id: 153 }), { headers: { 'Content-Type': 'application/json' } })
    })
    const client = createApiClient('http://api.test')
    vi.stubGlobal('fetch', fetchMock)
    const { data } = await client.GET('/api/v1/test-cases/{testCaseId}', {
      params: { path: { testCaseId: 153 } },
    })
    expect(data).toEqual({ id: 153 })
    expect(fetchMock).toHaveBeenCalledOnce()
  })

  it('exposes a default client', () => {
    expect(typeof api.GET).toBe('function')
  })
})
