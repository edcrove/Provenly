import { useState, type ReactNode } from 'react'

import { useProject } from '@/api/queries'

import { CurrentProjectContext, loadProject, saveProject } from './currentProject'

export function ProjectProvider({ children }: { children: ReactNode }) {
  const [stored, setStored] = useState(loadProject)
  // A remembered key that does not exist here (another database, or no longer visible) means every project.
  const remembered = useProject(stored || undefined)
  const project = stored && remembered.isError ? '' : stored
  const setProject = (key: string) => {
    saveProject(key)
    setStored(key)
  }
  return <CurrentProjectContext value={{ project, setProject }}>{children}</CurrentProjectContext>
}
