import { describe, expect, it, vi } from 'vitest'

import { ApiError } from '@/lib/problem'

import { keys } from './queries'
import { createQueryClient } from './queryClient'

const reject = (status: number) => () => Promise.reject(new ApiError(status))

describe('createQueryClient', () => {
  it('re-checks who is signed in when any query or mutation answers 401', async () => {
    const client = createQueryClient()
    const invalidate = vi.spyOn(client, 'invalidateQueries')

    await expect(client.fetchQuery({ queryKey: ['test-cases'], queryFn: reject(401) })).rejects.toThrow()
    expect(invalidate).toHaveBeenCalledWith({ queryKey: keys.me })

    invalidate.mockClear()
    await expect(client.fetchQuery({ queryKey: ['test-runs'], queryFn: reject(500) })).rejects.toThrow()
    await expect(client.fetchQuery({ queryKey: keys.me, queryFn: reject(401) })).rejects.toThrow()
    expect(invalidate).not.toHaveBeenCalled()

    const mutation = client.getMutationCache().build(client, { mutationFn: reject(401) })
    await expect(mutation.execute(undefined)).rejects.toThrow()
    expect(invalidate).toHaveBeenCalledWith({ queryKey: keys.me })
  })

  it('does not retry', () => {
    const defaults = createQueryClient().getDefaultOptions()
    expect(defaults.queries?.retry).toBe(false)
    expect(defaults.mutations?.retry).toBe(false)
  })
})
