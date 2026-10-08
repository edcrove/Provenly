import { useState } from 'react'
import { useNavigate } from 'react-router'

import { useCreateTestCase, useDimensions, useProjects } from '@/api/queries'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/select'
import { NotAllowed } from '@/components/NotAllowed'
import { PageTitle } from '@/components/PageTitle'
import { useCurrentProject } from '@/features/projects/currentProject'
import { can } from '@/lib/roles'
import { createBody } from '@/lib/taxonomy'

import { TestCaseForm } from './TestCaseForm'

export function NewTestCasePage() {
  const navigate = useNavigate()
  const create = useCreateTestCase()
  const all = useProjects()
  const projects = (all.data?.items ?? []).filter((p) => can(p.myRole, 'member'))
  const { project: current } = useCurrentProject()
  const [chosen, setProject] = useState(current)
  // The select shows only projects the user can write to: send the one it shows (the current project, else the
  // default TC, else the first one), never a hidden default the user cannot write to.
  const project = [chosen, 'TC'].find((k) => projects.some((p) => p.key === k)) ?? projects[0]?.key ?? ''
  const dimensions = useDimensions(project).data?.items ?? []
  // A viewer everywhere (e.g. following a shared link) gets the reason, not a form whose project list is empty.
  if (all.data && projects.length === 0)
    return (
      <NotAllowed
        title="New test case"
        reason="Creating test cases needs the member role (or higher) in a project."
      />
    )
  return (
    <Card className="max-w-2xl">
      <CardHeader>
        <PageTitle title="New test case" />
        <CardTitle as="h1" className="text-xl">
          New test case
        </CardTitle>
        <CardDescription>
          The TC-ID (project key and number, e.g. {project}-12) is assigned by Provenly and never changes or
          gets reused.
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4">
        <div className="grid gap-2">
          <Label htmlFor="tc-project">Project</Label>
          <NativeSelect id="tc-project" value={project} onChange={(e) => setProject(e.target.value)}>
            {projects.map((p) => (
              <option key={p.key} value={p.key}>
                {p.key} · {p.name}
              </option>
            ))}
          </NativeSelect>
        </div>
        <TestCaseForm
          dimensions={dimensions}
          submitLabel="Create test case"
          pending={create.isPending}
          error={create.error}
          onSubmit={(values) =>
            // Only dimensions of the chosen project: the form keeps what was picked before switching projects.
            create.mutate(
              { ...createBody(values, dimensions), project },
              { onSuccess: (tc) => navigate(`/test-cases/${tc.id}`) },
            )
          }
          onCancel={() => navigate('/test-cases')}
        />
      </CardContent>
    </Card>
  )
}
