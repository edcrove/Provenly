import { describe, expect, it } from 'vitest'

import { ApiError, errorMessage, unwrap } from './problem'

const response = (status: number) => new Response(null, { status })

describe('ApiError', () => {
  it('uses the problem detail, then title, then a generic message', () => {
    const base = { type: 'about:blank', status: 400, code: 'validation_error' as const }
    expect(new ApiError(400, { ...base, title: 'Bad Request', detail: 'title is required' }).message).toBe(
      'title is required',
    )
    expect(new ApiError(400, { ...base, title: 'Bad Request' }).message).toBe('Bad Request')
    expect(new ApiError(502).message).toBe('Request failed with status 502')
  })

  it('exposes field errors', () => {
    const err = new ApiError(400, {
      type: 'about:blank',
      title: 'Bad Request',
      status: 400,
      code: 'validation_error',
      errors: [{ field: 'title', message: 'is required' }],
    })
    expect(err.fieldErrors).toEqual({ title: 'is required' })
    expect(new ApiError(500).fieldErrors).toEqual({})
    expect(err.name).toBe('ApiError')
    expect(err.status).toBe(400)
  })
})

describe('unwrap', () => {
  it('returns data of successful responses', () => {
    expect(unwrap({ data: { id: 1 }, response: response(200) })).toEqual({ id: 1 })
    expect(unwrap({ data: undefined, response: response(204) })).toBeUndefined()
  })

  it('throws ApiError with the problem body', () => {
    const problem = {
      type: 'about:blank',
      title: 'Not Found',
      status: 404,
      code: 'not_found' as const,
      detail: 'gone',
    }
    expect(() => unwrap({ error: problem, response: response(404) })).toThrow(new ApiError(404, problem))
    try {
      unwrap({ error: problem, response: response(404) })
    } catch (e) {
      expect((e as ApiError).problem).toEqual(problem)
    }
  })

  it('throws ApiError without problem for non-problem errors or non-ok responses', () => {
    expect(() => unwrap({ error: 'plain text', response: response(500) })).toThrow(
      'Request failed with status 500',
    )
    expect(() => unwrap({ error: null, response: response(500) })).toThrow('Request failed with status 500')
    expect(() => unwrap({ data: {}, response: response(503) })).toThrow('Request failed with status 503')
  })
})

describe('errorMessage', () => {
  it('formats API, generic and unknown errors', () => {
    const err = new ApiError(400, {
      type: 'about:blank',
      title: 'Bad Request',
      status: 400,
      code: 'validation_error',
      detail: 'request validation failed',
      errors: [
        { field: 'title', message: 'is required' },
        { field: 'expectedResult', message: 'too long' },
      ],
    })
    expect(errorMessage(err)).toBe('request validation failed (title: is required; expectedResult: too long)')
    expect(errorMessage(new ApiError(404))).toBe('Request failed with status 404')
    expect(errorMessage(new Error('network down'))).toBe('network down')
    expect(errorMessage('weird')).toBe('Unexpected error')
  })
})
