import { QueryClient } from '@tanstack/react-query'

import { ApiError, authMemory, configureUnauthorizedHandler } from '../api/client'
import { queryKeys } from '../api/queries'

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      retry: (failureCount, error) => {
        if (error instanceof ApiError && error.status >= 400 && error.status < 500) return false
        return failureCount < 2
      },
    },
  },
})

configureUnauthorizedHandler(() => {
  authMemory.clear()
  queryClient.removeQueries({ queryKey: queryKeys.session })
  if (window.location.pathname !== '/login') window.location.assign('/login')
})
