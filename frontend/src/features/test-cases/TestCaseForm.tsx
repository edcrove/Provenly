import { useState, type FormEvent } from 'react'

import type { Dimension } from '@/api/client'
import { ErrorAlert } from '@/components/QueryState'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { ApiError } from '@/lib/problem'
import { selectableValues } from '@/lib/taxonomy'

export interface TestCaseFormValues {
  title: string
  description: string
  expectedResult: string
  automated: boolean
  /** Comma or space separated, as typed. */
  tags: string
  /** Dimension key → value key; '' leaves the dimension unset. */
  classification: Record<string, string>
}

interface Props {
  initial?: TestCaseFormValues
  /** The project's dimensions: one select per active dimension (or one the test case already uses). */
  dimensions?: Dimension[]
  submitLabel: string
  pending: boolean
  error: unknown
  onSubmit: (values: TestCaseFormValues) => void
  onCancel?: () => void
}

const empty: TestCaseFormValues = {
  title: '',
  description: '',
  expectedResult: '',
  automated: false,
  tags: '',
  classification: {},
}

/** Validation message of one field, linked to its input through aria-describedby. */
function FieldError({ id, message }: { id: string; message?: string }) {
  return message ? (
    <p id={id} className="text-destructive text-sm">
      {message}
    </p>
  ) : null
}

/** Form for the editable content of a test case. The TC-ID is never editable. */
export function TestCaseForm({
  initial = empty,
  dimensions = [],
  submitLabel,
  pending,
  error,
  onSubmit,
  onCancel,
}: Props) {
  const [values, setValues] = useState(initial)
  const set = <K extends keyof TestCaseFormValues>(key: K, value: TestCaseFormValues[K]) =>
    setValues((v) => ({ ...v, [key]: value }))

  const fieldErrors = error instanceof ApiError ? error.fieldErrors : {}
  const invalid = (field: string) =>
    fieldErrors[field] ? { 'aria-invalid': true, 'aria-describedby': `tc-${field}-error` } : {}

  const submit = (e: FormEvent) => {
    e.preventDefault()
    onSubmit(values)
  }

  return (
    <form onSubmit={submit} className="grid gap-4">
      {error ? <ErrorAlert error={error} title="Could not save the test case" /> : null}
      <div className="grid gap-2">
        <Label htmlFor="tc-title">Title</Label>
        <Input
          id="tc-title"
          value={values.title}
          onChange={(e) => set('title', e.target.value)}
          required
          maxLength={200}
          {...invalid('title')}
        />
        <FieldError id="tc-title-error" message={fieldErrors.title && `Title ${fieldErrors.title}`} />
      </div>
      <div className="grid gap-2">
        <Label htmlFor="tc-description">Description</Label>
        <Textarea
          id="tc-description"
          value={values.description}
          onChange={(e) => set('description', e.target.value)}
          {...invalid('description')}
        />
        <FieldError
          id="tc-description-error"
          message={fieldErrors.description && `Description ${fieldErrors.description}`}
        />
      </div>
      <div className="grid gap-2">
        <Label htmlFor="tc-expected">Expected result</Label>
        <Textarea
          id="tc-expected"
          value={values.expectedResult}
          onChange={(e) => set('expectedResult', e.target.value)}
          {...invalid('expectedResult')}
        />
        <FieldError
          id="tc-expectedResult-error"
          message={fieldErrors.expectedResult && `Expected result ${fieldErrors.expectedResult}`}
        />
      </div>
      <Label htmlFor="tc-automated">
        <Checkbox
          id="tc-automated"
          checked={values.automated}
          onChange={(e) => set('automated', e.target.checked)}
        />
        Automated (an automated test declares this TC-ID)
      </Label>
      <div className="grid gap-2">
        <Label htmlFor="tc-tags">Tags</Label>
        <Input
          id="tc-tags"
          value={values.tags}
          onChange={(e) => set('tags', e.target.value)}
          placeholder="smoke, checkout"
          {...invalid('tags')}
        />
        <FieldError id="tc-tags-error" message={fieldErrors.tags && `Tags: ${fieldErrors.tags}`} />
      </div>
      {dimensions.length > 0 ? (
        <fieldset className="grid gap-3 sm:grid-cols-2">
          <legend className="mb-2 text-sm font-medium">Classification</legend>
          {dimensions
            .filter((d) => d.archivedAt === null || values.classification[d.key])
            .map((d) => {
              const current = values.classification[d.key] ?? ''
              const field = `classification.${d.key}`
              return (
                <div key={d.key} className="grid gap-1">
                  <Label htmlFor={`tc-dim-${d.key}`}>{d.name}</Label>
                  <NativeSelect
                    id={`tc-dim-${d.key}`}
                    value={current}
                    onChange={(e) =>
                      set('classification', { ...values.classification, [d.key]: e.target.value })
                    }
                    {...invalid(field)}
                  >
                    <option value="">—</option>
                    {selectableValues(d, current).map((v) => (
                      <option key={v.key} value={v.key}>
                        {v.name}
                      </option>
                    ))}
                  </NativeSelect>
                  <FieldError id={`tc-${field}-error`} message={fieldErrors[field]} />
                </div>
              )
            })}
        </fieldset>
      ) : null}
      <div className="flex gap-2">
        <Button type="submit" disabled={pending}>
          {submitLabel}
        </Button>
        {onCancel ? (
          <Button type="button" variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
        ) : null}
      </div>
    </form>
  )
}
