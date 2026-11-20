import { QueryClient } from '@tanstack/vue-query'
import { ApiError } from '@/api/errors'

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5_000,
      retry: (count, err) => !(err instanceof ApiError && err.status < 500) && count < 2,
    },
  },
})
