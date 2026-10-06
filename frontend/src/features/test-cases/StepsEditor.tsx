import { ArrowDown, ArrowUp, Pencil, Trash2 } from 'lucide-react'
import { useState, type FormEvent } from 'react'

import type { TestStep } from '@/api/client'
import { useStepMutations, useTestSteps } from '@/api/queries'
import { InlineConfirm } from '@/components/InlineConfirm'
import { ErrorAlert, QueryState } from '@/components/QueryState'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { moveId } from '@/lib/steps'

interface StepFields {
  action: string
  expectedResult: string
}

function StepForm({
  initial,
  submitLabel,
  onSubmit,
  onCancel,
}: {
  initial: StepFields
  submitLabel: string
  /** `clear` empties the form; call it once the server accepted the step, so a rejected one keeps what was typed. */
  onSubmit: (fields: StepFields, clear: () => void) => void
  onCancel?: () => void
}) {
  const [fields, setFields] = useState(initial)
  const submit = (e: FormEvent) => {
    e.preventDefault()
    onSubmit(fields, () => setFields({ action: '', expectedResult: '' }))
  }
  return (
    <form onSubmit={submit} className="grid gap-2 md:grid-cols-[1fr_1fr_auto]" aria-label={submitLabel}>
      <Input
        aria-label="Step action"
        placeholder="Action"
        value={fields.action}
        required
        onChange={(e) => setFields({ ...fields, action: e.target.value })}
      />
      <Input
        aria-label="Step expected result"
        placeholder="Expected result (optional)"
        value={fields.expectedResult}
        onChange={(e) => setFields({ ...fields, expectedResult: e.target.value })}
      />
      <div className="flex gap-2">
        <Button type="submit" size="sm">
          {submitLabel}
        </Button>
        {onCancel ? (
          <Button type="button" size="sm" variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
        ) : null}
      </div>
    </form>
  )
}

/** Optional ordered steps of a test case: add, edit, delete and reorder. */
export function StepsEditor({ testCaseId, readOnly = false }: { testCaseId: number; readOnly?: boolean }) {
  const query = useTestSteps(testCaseId)
  const m = useStepMutations(testCaseId)
  const [editing, setEditing] = useState<number | null>(null)
  // Only the outcome of the latest action is shown: an older failure must not hide (or outlive) a newer one.
  const [last, setLast] = useState<keyof typeof m | null>(null)
  const error = last ? m[last].error : null
  const track = (key: keyof typeof m) => {
    m[key].reset()
    setLast(key)
  }

  const move = (steps: TestStep[], index: number, delta: -1 | 1) => {
    track('reorder')
    m.reorder.mutate(
      moveId(
        steps.map((s) => s.id),
        index,
        delta,
      ),
    )
  }

  return (
    <div className="grid gap-3">
      {error ? <ErrorAlert error={error} title="Could not update the steps" /> : null}
      <QueryState query={query}>
        {(data) =>
          data.items.length === 0 ? (
            <p className="text-muted-foreground text-sm">No steps. A test case is valid without steps.</p>
          ) : (
            <ol className="grid gap-2" aria-label="Steps">
              {data.items.map((step, index) => (
                <li key={step.id} className="rounded-md border p-2 text-sm">
                  {editing === step.id ? (
                    <StepForm
                      initial={{ action: step.action, expectedResult: step.expectedResult }}
                      submitLabel="Save step"
                      onCancel={() => setEditing(null)}
                      onSubmit={(fields) => {
                        track('update')
                        m.update.mutate({ stepId: step.id, ...fields }, { onSuccess: () => setEditing(null) })
                      }}
                    />
                  ) : (
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0">
                        <span className="font-medium">
                          {step.position}. {step.action}
                        </span>
                        {step.expectedResult ? (
                          <p className="text-muted-foreground">Expected: {step.expectedResult}</p>
                        ) : null}
                      </div>
                      {readOnly ? null : (
                        <div className="flex gap-1">
                          <Button
                            size="icon"
                            variant="ghost"
                            aria-label={`Move step ${step.position} up`}
                            disabled={index === 0}
                            onClick={() => move(data.items, index, -1)}
                          >
                            <ArrowUp />
                          </Button>
                          <Button
                            size="icon"
                            variant="ghost"
                            aria-label={`Move step ${step.position} down`}
                            disabled={index === data.items.length - 1}
                            onClick={() => move(data.items, index, 1)}
                          >
                            <ArrowDown />
                          </Button>
                          <Button
                            size="icon"
                            variant="ghost"
                            aria-label={`Edit step ${step.position}`}
                            onClick={() => setEditing(step.id)}
                          >
                            <Pencil />
                          </Button>
                          <InlineConfirm
                            label={`Confirm deleting step ${step.position}`}
                            question={`Delete step ${step.position}?`}
                            confirmLabel="Delete"
                            pending={m.remove.isPending}
                            onConfirm={(close) => {
                              track('remove')
                              m.remove.mutate(step.id, { onSettled: close })
                            }}
                            trigger={(open) => (
                              <Button
                                size="icon"
                                variant="ghost"
                                aria-label={`Delete step ${step.position}`}
                                onClick={open}
                              >
                                <Trash2 />
                              </Button>
                            )}
                          />
                        </div>
                      )}
                    </div>
                  )}
                </li>
              ))}
            </ol>
          )
        }
      </QueryState>
      {readOnly ? null : (
        <StepForm
          initial={{ action: '', expectedResult: '' }}
          submitLabel="Add step"
          onSubmit={(f, clear) => {
            track('create')
            m.create.mutate(f, { onSuccess: clear })
          }}
        />
      )}
    </div>
  )
}
