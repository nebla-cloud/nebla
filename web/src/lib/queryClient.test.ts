import { describe, expect, it } from 'vitest'
import { ApiError } from '@/api/errors'
import { shouldRetry } from './queryClient'

describe('shouldRetry', () => {
  it('retries server errors twice', () => {
    const e = new ApiError(502, 'bad_gateway', 'bad gateway')
    expect([0, 1, 2].map((n) => shouldRetry(n, e))).toEqual([true, true, false])
  })

  it('never retries other client errors', () => {
    expect(shouldRetry(0, new ApiError(404, 'not_found', 'not found'))).toBe(false)
  })

  it('retries a rate limit a few times', () => {
    const e = new ApiError(429, 'rate_limit_exceeded', 'too many requests')
    expect([0, 1, 2, 3].map((n) => shouldRetry(n, e))).toEqual([true, true, true, false])
  })
})
