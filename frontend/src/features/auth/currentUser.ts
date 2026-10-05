import { useOutletContext } from 'react-router'

import type { User } from '@/api/client'

/** The signed-in user, provided by the Layout to every page it renders. */
export function useCurrentUser(): User {
  return useOutletContext<User>()
}
