import { useState } from 'react'
import { useParams } from 'react-router'

import type { TestCase } from '@/api/client'
import {
  useDeprecateTestCase,
  useReactivateTestCase,
  useTestCase,
  useTestCaseHistory,
  useUpdateTestCase,
} from '@/api/queries'
import { ErrorAlert, QueryState } from '@/components/QueryState'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { PageTitle } from '@/components/PageTitle'
import { NotFoundPage } from '@/app/NotFoundPage'
import { formatDateTime } from '@/lib/format'
import { positiveInt } from '@/lib/status'

import { StepsEditor } from './StepsEditor'
import { TestCaseForm } from './TestCaseForm'
import { TestCaseHistory } from './TestCaseHistory'

function Definition({ tc }: { tc: TestCase }) {
  const [editing, setEditing] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const update = useUpdateTestCase(tc.id)
  const deprecate = useDeprecateTestCase(tc.id)
  const reactivate = useReactivateTestCase(tc.id)
  const history = useTestCaseHistory(tc.id, 1)
  const manualWithResults = tc.status === 'active' && !tc.automated && (history.data?.totalItems ?? 0) > 0

  return (
    <div className="grid gap-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <PageTitle title={`${tc.key} · ${tc.title}`} />
          <h1 className="text-2xl font-semibold">
            <span className="font-mono">{tc.key}</span> · {tc.title}
          </h1>
          <div className="mt-2 flex gap-2">
            <Badge variant={tc.status === 'active' ? 'secondary' : 'outline'}>{tc.status}</Badge>
            <Badge variant="outline">{tc.automated ? 'automated' : 'manual'}</Badge>
          </div>
        </div>
        <div className="flex gap-2">
          {!editing && (
            <Button variant="outline" onClick={() => setEditing(true)}>
              Edit
            </Button>
          )}
          {tc.status === 'active' &&
            (confirming ? (
              <>
                <Button
                  variant="destructive"
                  disabled={deprecate.isPending}
                  onClick={() => deprecate.mutate(undefined, { onSuccess: () => setConfirming(false) })}
                >
                  Confirm deprecation
                </Button>
                <Button variant="ghost" onClick={() => setConfirming(false)}>
                  Keep active
                </Button>
              </>
            ) : (
              <Button variant="outline" onClick={() => setConfirming(true)}>
                Deprecate
              </Button>
            ))}
          {tc.status === 'deprecated' && (
            <Button variant="outline" disabled={reactivate.isPending} onClick={() => reactivate.mutate()}>
              Reactivate
            </Button>
          )}
        </div>
      </div>
      {deprecate.error ? <ErrorAlert error={deprecate.error} title="Could not deprecate" /> : null}
      {reactivate.error ? <ErrorAlert error={reactivate.error} title="Could not reactivate" /> : null}
      {manualWithResults ? (
        <Alert data-testid="manual-with-results">
          <AlertTitle>Receives automated results but is marked manual</AlertTitle>
          <AlertDescription className="flex flex-wrap items-center gap-2">
            Runs do not count it in their expected universe. Mark it as automated so future runs include it.
            <Button
              size="sm"
              variant="outline"
              disabled={update.isPending}
              onClick={() => update.mutate({ automated: true })}
            >
              Mark as automated
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle as="h2">Current definition</CardTitle>
          <CardDescription>
            What this test case says today. Past results were recorded against the TC-ID, not against this
            exact wording: editing content never creates a version or changes how old results are read.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          {editing ? (
            <TestCaseForm
              initial={{
                title: tc.title,
                description: tc.description,
                expectedResult: tc.expectedResult,
                automated: tc.automated,
              }}
              submitLabel="Save changes"
              pending={update.isPending}
              error={update.error}
              onSubmit={(values) => update.mutate(values, { onSuccess: () => setEditing(false) })}
              onCancel={() => setEditing(false)}
            />
          ) : (
            <dl className="grid gap-3 text-sm">
              <div>
                <dt className="font-medium">Description</dt>
                <dd className="text-muted-foreground whitespace-pre-wrap">{tc.description || '—'}</dd>
              </div>
              <div>
                <dt className="font-medium">Expected result</dt>
                <dd className="text-muted-foreground whitespace-pre-wrap">{tc.expectedResult || '—'}</dd>
              </div>
              <div className="text-muted-foreground text-xs">
                Created {formatDateTime(tc.createdAt)} · Updated {formatDateTime(tc.updatedAt)}
                {tc.deprecatedAt ? ` · Deprecated ${formatDateTime(tc.deprecatedAt)}` : ''}
              </div>
            </dl>
          )}
          <div className="grid gap-2">
            <h3 className="font-medium">Steps</h3>
            <StepsEditor testCaseId={tc.id} />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle as="h2">Execution history</CardTitle>
          <CardDescription>
            Observed at execution time: test name, run, branch, commit and execution date as reported by CI;
            Reported is when Provenly received the report.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <TestCaseHistory testCaseId={tc.id} />
        </CardContent>
      </Card>
    </div>
  )
}

export function TestCaseDetailPage() {
  const { testCaseId } = useParams()
  const id = positiveInt(testCaseId, 0)
  const query = useTestCase(id)
  if (!id) return <NotFoundPage />
  return (
    <QueryState page query={query}>
      {(tc) => <Definition key={tc.id} tc={tc} />}
    </QueryState>
  )
}
