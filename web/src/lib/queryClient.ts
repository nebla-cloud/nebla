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

import { QueryClient } from '@tanstack/vue-query'
import { ApiError } from '@/api/errors'

// Client errors are final, except a rate limit, which clears in a few seconds.
export function shouldRetry(count: number, err: unknown): boolean {
  if (err instanceof ApiError && err.status === 429) return count < 3
  return !(err instanceof ApiError && err.status < 500) && count < 2
}

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5_000,
      retry: shouldRetry,
    },
  },
})
