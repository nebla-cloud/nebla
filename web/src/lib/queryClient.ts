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
