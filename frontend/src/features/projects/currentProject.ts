import { createContext, useContext } from 'react'

const STORAGE_KEY = 'provenly.project'

/** The project the lists are narrowed to; '' means every project. */
export interface CurrentProject {
  project: string
  setProject: (key: string) => void
}

/** Outside a ProjectProvider: every project, and choosing one does nothing. */
export const noProvider: CurrentProject = { project: '', setProject: () => {} }

export const CurrentProjectContext = createContext<CurrentProject>(noProvider)

export function useCurrentProject(): CurrentProject {
  return useContext(CurrentProjectContext)
}

/** Remembered choice of this browser (storage may be unavailable: then nothing is remembered). */
export function loadProject(): string {
  try {
    return globalThis.localStorage?.getItem(STORAGE_KEY) ?? ''
  } catch {
    return ''
  }
}

export function saveProject(key: string) {
  try {
    if (key) globalThis.localStorage?.setItem(STORAGE_KEY, key)
    else globalThis.localStorage?.removeItem(STORAGE_KEY)
  } catch {
    // Not remembered; the choice still applies to this page.
  }
}

/** The text of a project everywhere the current one is named. */
export const projectLabel = (p: { key: string; name: string }) => `${p.key} · ${p.name}`
