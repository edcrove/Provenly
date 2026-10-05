import { useState, type FormEvent } from 'react'

import type { Dimension } from '@/api/client'
import { useDimensionMutations, useDimensions } from '@/api/queries'
import { ErrorAlert, QueryState } from '@/components/QueryState'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'

/** Key and name inputs that add a dimension or a value. */
function AddForm({
  label,
  placeholder,
  pending,
  onAdd,
}: {
  label: string
  placeholder: string
  pending: boolean
  onAdd: (key: string, name: string, done: () => void) => void
}) {
  const [key, setKey] = useState('')
  const [name, setName] = useState('')
  const submit = (e: FormEvent) => {
    e.preventDefault()
    onAdd(key.trim(), name.trim(), () => {
      setKey('')
      setName('')
    })
  }
  return (
    <form onSubmit={submit} aria-label={label} className="flex flex-wrap items-center gap-2">
      <Input
        aria-label={`${label}: key`}
        placeholder={placeholder}
        className="w-36 font-mono"
        value={key}
        onChange={(e) => setKey(e.target.value)}
        required
      />
      <Input
        aria-label={`${label}: name`}
        placeholder="Name"
        className="w-40"
        value={name}
        onChange={(e) => setName(e.target.value)}
        required
      />
      <Button type="submit" size="sm" variant="outline" disabled={pending}>
        {label}
      </Button>
    </form>
  )
}

function DimensionRow({
  dimension,
  projectKey,
  manage,
}: {
  dimension: Dimension
  projectKey: string
  manage: boolean
}) {
  const m = useDimensionMutations(projectKey)
  const archived = dimension.archivedAt !== null
  const error = m.update.error ?? m.createValue.error ?? m.updateValue.error
  return (
    <li className="grid gap-2 border-b pb-3 last:border-0" data-testid={`dimension-${dimension.key}`}>
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium">{dimension.name}</span>
        <span className="text-muted-foreground font-mono text-xs">{dimension.key}</span>
        {dimension.builtIn ? <Badge variant="outline">built-in</Badge> : null}
        {archived ? <Badge variant="outline">archived</Badge> : null}
        {manage ? (
          <Button
            size="sm"
            variant="ghost"
            disabled={m.update.isPending}
            onClick={() => m.update.mutate({ dimensionKey: dimension.key, archived: !archived })}
          >
            {archived ? 'Restore' : 'Archive'}
          </Button>
        ) : null}
      </div>
      <div className="flex flex-wrap items-center gap-1">
        {dimension.values.length === 0 ? (
          <span className="text-muted-foreground text-sm">No values yet.</span>
        ) : null}
        {dimension.values.map((v) => {
          const off = v.archivedAt !== null
          return manage ? (
            <Button
              key={v.key}
              size="sm"
              variant={off ? 'ghost' : 'secondary'}
              className={off ? 'line-through' : undefined}
              title={off ? `Restore ${v.name}` : `Archive ${v.name}`}
              aria-label={`${off ? 'Restore' : 'Archive'} ${dimension.name}: ${v.name}`}
              disabled={m.updateValue.isPending}
              onClick={() =>
                m.updateValue.mutate({ dimensionKey: dimension.key, valueKey: v.key, archived: !off })
              }
            >
              {v.name}
            </Button>
          ) : (
            <Badge
              key={v.key}
              variant={off ? 'outline' : 'secondary'}
              className={off ? 'line-through' : undefined}
            >
              {v.name}
            </Badge>
          )
        })}
      </div>
      {manage && !archived ? (
        <AddForm
          label={`Add value to ${dimension.name}`}
          placeholder="value-key"
          pending={m.createValue.isPending}
          onAdd={(key, name, done) =>
            m.createValue.mutate({ dimensionKey: dimension.key, key, name }, { onSuccess: done })
          }
        />
      ) : null}
      {error ? <ErrorAlert error={error} title={`Could not update ${dimension.name}`} /> : null}
    </li>
  )
}

/** A project's classification dimensions; maintainers add dimensions and values and archive them (never delete). */
export function ClassificationSection({ projectKey, manage }: { projectKey: string; manage: boolean }) {
  const dimensions = useDimensions(projectKey)
  const m = useDimensionMutations(projectKey)
  return (
    <Card>
      <CardHeader>
        <CardTitle as="h2">Classification</CardTitle>
        <CardDescription>
          Dimensions classify test cases with controlled values (one per dimension); tags are free labels on
          each test case. Values and dimensions are archived, never deleted, so reports keep their meaning.
          {manage ? ' Click a value to archive or restore it.' : ''}
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4">
        <QueryState query={dimensions}>
          {(data) => (
            <ul className="grid gap-3">
              {data.items.map((d) => (
                <DimensionRow key={d.key} dimension={d} projectKey={projectKey} manage={manage} />
              ))}
            </ul>
          )}
        </QueryState>
        {manage ? (
          <div className="grid gap-2">
            {m.create.error ? (
              <ErrorAlert error={m.create.error} title="Could not add the dimension" />
            ) : null}
            <AddForm
              label="Add dimension"
              placeholder="dimension-key"
              pending={m.create.isPending}
              onAdd={(key, name, done) => m.create.mutate({ key, name }, { onSuccess: done })}
            />
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}
