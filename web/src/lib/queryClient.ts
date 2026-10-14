import { QueryClient } from '@tanstack/react-query'
import { ApiError } from './api'

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      gcTime: 5 * 60_000,
      refetchOnWindowFocus: false,
      retry: (failureCount, error) => {
        if (error instanceof ApiError && [400, 401, 403, 404, 413].includes(error.status)) return false
        return failureCount < 2
      },
    },
    mutations: {
      retry: false,
    },
  },
})
