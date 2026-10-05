import { MutationCache, QueryCache, QueryClient } from '@tanstack/react-query'

import { ApiError } from '@/lib/problem'

import { keys } from './queries'

/** The app's query client: a 401 anywhere (an expired session) re-checks who is signed in, which sends to sign-in. */
export function createQueryClient(): QueryClient {
  const onError = (error: unknown) => {
    if (error instanceof ApiError && error.status === 401)
      void client.invalidateQueries({ queryKey: keys.me })
  }
  const client: QueryClient = new QueryClient({
    // The "who is signed in" query itself answering 401 is the signed-out state, not an expiry.
    queryCache: new QueryCache({
      onError: (error, query) => query.queryKey[0] !== keys.me[0] && onError(error),
    }),
    mutationCache: new MutationCache({ onError }),
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return client
}
