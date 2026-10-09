import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router'

import { useStartManualRun, useSuites } from '@/api/queries'
import { NotAllowed } from '@/components/NotAllowed'
import { PageTitle } from '@/components/PageTitle'
import { ErrorAlert } from '@/components/QueryState'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/select'
import { moreProjectsHint, useProjectChoices } from '@/features/projects/choices'
import { useCurrentProject } from '@/features/projects/currentProject'
import { can } from '@/lib/roles'

/** Starts a manual run: what is being tested, of which project (and suite), and which test cases are expected. */
export function NewManualRunPage() {
  const navigate = useNavigate()
  const start = useStartManualRun()
  const choices = useProjectChoices()
  const all = choices.query
  const projects = choices.items.filter((p) => can(p.myRole, 'member'))
  const { project: current } = useCurrentProject()
  const [chosen, setProject] = useState(current)
  // The select shows only projects the user can write to: send the one it shows (the current project, else the
  // default TC, else the first one), never a hidden default the user cannot write to.
  const project = [chosen, 'TC'].find((k) => projects.some((p) => p.key === k)) ?? projects[0]?.key ?? ''
  const suites = (useSuites(project).data?.items ?? []).filter((s) => s.archivedAt === null)
  const [name, setName] = useState('')
  const [suite, setSuite] = useState('')
  const [scope, setScope] = useState<'manual' | 'all'>('manual')
  const [branch, setBranch] = useState('')
  const submit = (e: FormEvent) => {
    e.preventDefault()
    start.mutate(
      {
        project,
        name: name.trim(),
        scope,
        ...(suite ? { suite } : {}),
        ...(branch.trim() ? { branch: branch.trim() } : {}),
      },
      { onSuccess: (run) => navigate(`/test-runs/${run.id}`) },
    )
  }
  // A viewer everywhere (e.g. following a shared link) gets the reason, not a form whose project list is empty.
  if (all.data && projects.length === 0)
    return (
      <NotAllowed
        title="New manual run"
        reason="Starting manual runs needs the member role (or higher) in a project."
      />
    )
  return (
    <Card className="max-w-2xl">
      <CardHeader>
        <PageTitle title="New manual run" />
        <CardTitle as="h1" className="text-xl">
          New manual run
        </CardTitle>
        <CardDescription>
          A manual run expects the project&apos;s active manual test cases (or every active test case),
          optionally of one suite. You then record each result and complete the run.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={submit} className="grid gap-4" aria-label="New manual run">
          {start.error ? <ErrorAlert error={start.error} title="Could not start the run" /> : null}
          <div className="grid gap-2">
            <Label htmlFor="manual-name">What is being tested</Label>
            <Input
              id="manual-name"
              placeholder="Release 2.4 sign-off"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="manual-project">Project</Label>
            <NativeSelect
              id="manual-project"
              value={project}
              onChange={(e) => {
                setProject(e.target.value)
                setSuite('')
              }}
            >
              {projects.map((p) => (
                <option key={p.key} value={p.key}>
                  {p.key} · {p.name}
                </option>
              ))}
            </NativeSelect>
            {choices.more > 0 ? (
              <p className="text-muted-foreground max-w-xs text-xs">{moreProjectsHint(choices.more)}</p>
            ) : null}
          </div>
          <div className="grid gap-2">
            <Label htmlFor="manual-suite">Suite</Label>
            <NativeSelect id="manual-suite" value={suite} onChange={(e) => setSuite(e.target.value)}>
              <option value="">The whole project</option>
              {suites.map((s) => (
                <option key={s.key} value={s.key}>
                  {s.name}
                </option>
              ))}
            </NativeSelect>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="manual-scope">Expected test cases</Label>
            <NativeSelect
              id="manual-scope"
              value={scope}
              onChange={(e) => setScope(e.target.value as 'manual' | 'all')}
            >
              <option value="manual">Manual test cases (CI runs the automated ones)</option>
              <option value="all">Every active test case</option>
            </NativeSelect>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="manual-branch">Branch or build (optional)</Label>
            <Input id="manual-branch" value={branch} onChange={(e) => setBranch(e.target.value)} />
          </div>
          <div className="flex gap-2">
            <Button type="submit" disabled={start.isPending}>
              Start manual run
            </Button>
            <Button type="button" variant="ghost" onClick={() => navigate('/test-runs')}>
              Cancel
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}
