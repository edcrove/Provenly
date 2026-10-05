import { useState } from 'react'
import { useNavigate } from 'react-router'

import { useCreateTestCase, useProjects } from '@/api/queries'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/select'
import { PageTitle } from '@/components/PageTitle'
import { useCurrentProject } from '@/features/projects/currentProject'
import { can } from '@/lib/roles'

import { TestCaseForm } from './TestCaseForm'

export function NewTestCasePage() {
  const navigate = useNavigate()
  const create = useCreateTestCase()
  const projects = useProjects()
  const { project: current } = useCurrentProject()
  const [project, setProject] = useState(current || 'TC')
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
            {(projects.data?.items ?? [])
              .filter((p) => can(p.myRole, 'member'))
              .map((p) => (
                <option key={p.key} value={p.key}>
                  {p.key} · {p.name}
                </option>
              ))}
          </NativeSelect>
        </div>
        <TestCaseForm
          submitLabel="Create test case"
          pending={create.isPending}
          error={create.error}
          onSubmit={(values) =>
            create.mutate({ ...values, project }, { onSuccess: (tc) => navigate(`/test-cases/${tc.id}`) })
          }
          onCancel={() => navigate('/test-cases')}
        />
      </CardContent>
    </Card>
  )
}
