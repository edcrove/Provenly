import { useState } from 'react'

/** A list's page, back to the first when what the list shows changes (another project or filter). */
export function usePage(scope: string): [number, (page: number) => void] {
  const [state, setState] = useState({ scope, page: 1 })
  const page = state.scope === scope ? state.page : 1
  return [page, (next: number) => setState({ scope, page: next })]
}
