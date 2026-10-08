import { useState, type FormEvent } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/select'
import { executionStatuses, hasRunFilters, modeLabels, modes, type RunFilterParams } from '@/lib/runFilters'

/** Narrows the runs list by branch, execution status, mode and a range of days; the state lives in the URL. */
export function RunFilters({
  value,
  onChange,
}: {
  value: RunFilterParams
  onChange: (next: Partial<Record<keyof RunFilterParams, string>>) => void
}) {
  const [branch, setBranch] = useState(value.branch)
  const submit = (e: FormEvent) => {
    e.preventDefault()
    onChange({ branch: branch.trim() })
  }
  return (
    <div className="flex flex-wrap items-center gap-2" data-testid="run-filters">
      <form onSubmit={submit} role="search" aria-label="Filter by branch">
        <Input
          aria-label="Branch"
          placeholder="Branch (Enter)"
          className="w-40"
          value={branch}
          onChange={(e) => setBranch(e.target.value)}
        />
      </form>
      <NativeSelect
        aria-label="Execution"
        value={value.executionStatus ?? ''}
        onChange={(e) => onChange({ executionStatus: e.target.value })}
      >
        <option value="">Any execution</option>
        {executionStatuses.map((s) => (
          <option key={s} value={s}>
            {s}
          </option>
        ))}
      </NativeSelect>
      <NativeSelect
        aria-label="Mode"
        value={value.mode ?? ''}
        onChange={(e) => onChange({ mode: e.target.value })}
      >
        <option value="">Any mode</option>
        {modes.map((m) => (
          <option key={m} value={m}>
            {modeLabels[m]}
          </option>
        ))}
      </NativeSelect>
      <label className="text-muted-foreground flex items-center gap-1 text-sm">
        From
        <Input
          type="date"
          aria-label="From"
          className="w-40"
          value={value.from}
          max={value.to || undefined}
          onChange={(e) => onChange({ from: e.target.value })}
        />
      </label>
      <label className="text-muted-foreground flex items-center gap-1 text-sm">
        To
        <Input
          type="date"
          aria-label="To"
          className="w-40"
          value={value.to}
          min={value.from || undefined}
          onChange={(e) => onChange({ to: e.target.value })}
        />
      </label>
      {hasRunFilters(value) ? (
        <Button
          size="sm"
          variant="ghost"
          onClick={() => {
            setBranch('')
            onChange({ branch: '', executionStatus: '', mode: '', from: '', to: '' })
          }}
        >
          Clear filters
        </Button>
      ) : null}
    </div>
  )
}
