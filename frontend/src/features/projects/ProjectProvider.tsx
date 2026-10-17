import { useState, type ReactNode } from 'react'

import { useProject } from '@/api/queries'
import { ApiError } from '@/lib/problem'

import { CurrentProjectContext, loadProject, saveProject } from './currentProject'

export function ProjectProvider({ children }: { children: ReactNode }) {
  const [stored, setStored] = useState(loadProject)
  // A remembered key that does not exist here (another database, or no longer visible) means every project; a server
  // or network error does not (it used to widen the scope to every project silently; audit 2026-10-17).
  const remembered = useProject(stored || undefined)
  const gone =
    remembered.isError &&
    remembered.error instanceof ApiError &&
    (remembered.error.status === 404 || remembered.error.status === 403)
  const project = stored && gone ? '' : stored
  const setProject = (key: string) => {
    saveProject(key)
    setStored(key)
  }
  return <CurrentProjectContext value={{ project, setProject }}>{children}</CurrentProjectContext>
}
