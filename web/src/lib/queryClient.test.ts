// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

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
