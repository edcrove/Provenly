import { useDeferredValue, useState } from 'react'

import { useTestCases } from '@/api/queries'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/select'

/** How many matches a picker lists; more say so and ask for a search. */
const PICKER_SIZE = 50

/**
 * Picks one of a project's test cases, searching on the server by key or title (DEC-78): it lists the first 50 that
 * match, by key, and says how many there are, so a long catalog never silently stops at a first page.
 */
export function TestCasePicker({
  projectKey,
  label,
  action,
  exclude = [],
  activeOnly = false,
  pending = false,
  onPick,
}: {
  projectKey: string
  /** The select's accessible name, e.g. "Test case to link". */
  label: string
  /** The button, e.g. "Link test case". */
  action: string
  /** Test case ids already chosen, left out of the options. */
  exclude?: number[]
  activeOnly?: boolean
  pending?: boolean
  onPick: (testCaseId: number, done: () => void) => void
}) {
  const [text, setText] = useState('')
  const q = useDeferredValue(text.trim())
  const found = useTestCases(1, {
    project: projectKey,
    pageSize: PICKER_SIZE,
    ...(activeOnly ? { status: 'active' as const } : {}),
    ...(q ? { q } : {}),
  })
  const [picked, setPicked] = useState('')
  const items = found.data?.items ?? []
  const options = items.filter((tc) => !exclude.includes(tc.id))
  const total = found.data?.totalItems ?? 0
  return (
    <div className="grid gap-1">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          aria-label={`Search: ${label}`}
          placeholder="Key or title"
          className="h-9 w-48"
          value={text}
          onChange={(e) => {
            setText(e.target.value)
            setPicked('')
          }}
        />
        <NativeSelect aria-label={label} value={picked} onChange={(e) => setPicked(e.target.value)}>
          <option value="">{q && items.length === 0 ? 'No test case matches' : 'Choose a test case…'}</option>
          {options.map((tc) => (
            <option key={tc.id} value={tc.id}>
              {tc.key} · {tc.title}
            </option>
          ))}
        </NativeSelect>
        <Button
          size="sm"
          disabled={!picked || pending}
          onClick={() => onPick(Number(picked), () => setPicked(''))}
        >
          {action}
        </Button>
      </div>
      {total > items.length ? (
        <p className="text-muted-foreground text-xs" data-testid="picker-more">
          Showing {items.length} of {total}, type to search.
        </p>
      ) : null}
    </div>
  )
}
