import { useState, type ReactNode } from 'react'

import { useProjects } from '@/api/queries'

import { CurrentProjectContext, loadProject, saveProject } from './currentProject'

export function ProjectProvider({ children }: { children: ReactNode }) {
  const [stored, setStored] = useState(loadProject)
  const known = useProjects().data?.items
  // A remembered key that does not exist here (another database) means every project.
  const project = stored && known && !known.some((p) => p.key === stored) ? '' : stored
  const setProject = (key: string) => {
    saveProject(key)
    setStored(key)
  }
  return <CurrentProjectContext value={{ project, setProject }}>{children}</CurrentProjectContext>
}
