import { useState, type FormEvent } from 'react'
import { Link, useParams, useSearchParams } from 'react-router'

import type { Member } from '@/api/client'
import { useMemberMutations, useProject, useProjectMembers } from '@/api/queries'
import { PageTitle } from '@/components/PageTitle'
import { Pagination } from '@/components/Pagination'
import { ErrorAlert, QueryState } from '@/components/QueryState'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { formatDateTime } from '@/lib/format'
import { can, memberRoles, roleDescriptions, type MemberRole } from '@/lib/roles'
import { positiveInt } from '@/lib/status'

import { ApiKeysSection } from './ApiKeysSection'
import { useCurrentProject } from './currentProject'
import { ClassificationSection } from './ClassificationSection'
import { GitHubSection } from './GitHubSection'
import { useProjectRole } from './useProjectRole'
import { WebhooksSection } from './WebhooksSection'

function RoleSelect({
  value,
  onChange,
  label,
}: {
  value: MemberRole
  onChange: (r: MemberRole) => void
  label: string
}) {
  return (
    <NativeSelect aria-label={label} value={value} onChange={(e) => onChange(e.target.value as MemberRole)}>
      {memberRoles.map((r) => (
        <option key={r} value={r}>
          {r}
        </option>
      ))}
    </NativeSelect>
  )
}

function MemberRow({ member, manage, projectKey }: { member: Member; manage: boolean; projectKey: string }) {
  const m = useMemberMutations(projectKey)
  const error = m.set.error ?? m.remove.error
  return (
    <TableRow data-testid={`member-${member.user.username}`}>
      <TableCell className="font-mono">{member.user.username}</TableCell>
      <TableCell>{member.user.displayName}</TableCell>
      <TableCell>
        {manage ? (
          <RoleSelect
            label={`Role of ${member.user.username}`}
            value={member.role}
            onChange={(role) => m.set.mutate({ username: member.user.username, role })}
          />
        ) : (
          member.role
        )}
      </TableCell>
      <TableCell className="whitespace-nowrap">{formatDateTime(member.since)}</TableCell>
      <TableCell>
        {manage ? (
          <Button
            size="sm"
            variant="outline"
            disabled={m.remove.isPending}
            onClick={() => m.remove.mutate(member.user.username)}
          >
            Remove
          </Button>
        ) : null}
        {error ? <ErrorAlert error={error} title="Could not update the member" /> : null}
      </TableCell>
    </TableRow>
  )
}

function AddMember({ projectKey }: { projectKey: string }) {
  const m = useMemberMutations(projectKey)
  const [username, setUsername] = useState('')
  const [role, setRole] = useState<MemberRole>('member')
  const submit = (e: FormEvent) => {
    e.preventDefault()
    m.set.mutate({ username: username.trim(), role }, { onSuccess: () => setUsername('') })
  }
  return (
    <form onSubmit={submit} className="grid gap-3" aria-label="Add member">
      {m.set.error ? <ErrorAlert error={m.set.error} title="Could not add the member" /> : null}
      <div className="flex flex-wrap items-end gap-3">
        <div className="grid gap-2">
          <Label htmlFor="member-username">Username</Label>
          <Input
            id="member-username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            required
          />
        </div>
        <RoleSelect label="Role" value={role} onChange={setRole} />
        <Button type="submit" disabled={m.set.isPending}>
          Add member
        </Button>
      </div>
      <p className="text-muted-foreground text-xs">
        {memberRoles.map((r) => `${r}: ${roleDescriptions[r]}`).join(' · ')}. Administrators can do everything
        in every project.
      </p>
    </form>
  )
}

/** The pages that show what a project holds; opening one makes this the current project. */
const projectPages = [
  { to: '/dashboard', label: 'Dashboard' },
  { to: '/test-runs', label: 'Test Runs' },
  { to: '/test-cases', label: 'Test Cases' },
  { to: '/suites', label: 'Suites' },
  { to: '/requirements', label: 'Requirements' },
  { to: '/issues', label: 'Issues' },
]

/**
 * A project's settings: what it holds (links to its pages), its members, classification and, for maintainers, CI API
 * keys, webhooks and GitHub. Maintainers and administrators change them; everyone else reads members and classification.
 */
export function ProjectMembersPage() {
  const { projectKey = '' } = useParams()
  const [params, setParams] = useSearchParams()
  const page = positiveInt(params.get('page'), 1)
  const members = useProjectMembers(projectKey, page)
  const manage = can(useProjectRole(projectKey), 'maintainer')
  const { setProject } = useCurrentProject()
  const project = useProject(projectKey).data
  const sections = [
    { id: 'members', label: 'Members' },
    { id: 'classification', label: 'Classification' },
    ...(manage
      ? [
          { id: 'api-keys', label: 'CI API keys' },
          { id: 'webhooks', label: 'Webhooks' },
          { id: 'github', label: 'GitHub' },
        ]
      : []),
  ]
  return (
    <div className="grid gap-6">
      <div className="grid gap-2">
        <PageTitle title={`${projectKey} · settings`} />
        <h1 className="text-2xl font-semibold">
          <span className="font-mono">{projectKey}</span>
          {project ? ` · ${project.name}` : ''} — Settings
        </h1>
        <p className="text-muted-foreground max-w-3xl text-sm">
          A project owns its test cases (numbered {projectKey}-1, {projectKey}-2…), runs, suites,
          requirements, issues, members and integrations. Its key never changes.{' '}
          <Link to="/projects" className="underline">
            All projects
          </Link>
        </p>
        <nav aria-label="In this project" className="flex flex-wrap gap-x-4 gap-y-1 text-sm">
          {projectPages.map((l) => (
            <Link key={l.to} to={l.to} className="underline" onClick={() => setProject(projectKey)}>
              {l.label}
            </Link>
          ))}
        </nav>
        <nav
          aria-label="On this page"
          className="text-muted-foreground flex flex-wrap gap-x-4 gap-y-1 text-sm"
        >
          {sections.map((s) => (
            <a key={s.id} href={`#${s.id}`} className="hover:text-foreground">
              {s.label}
            </a>
          ))}
        </nav>
      </div>
      <Card id="members" className="scroll-mt-4">
        <CardHeader>
          <CardTitle as="h2" className="text-lg">
            Members
          </CardTitle>
          <CardDescription>Who works in this project and with which role.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-6">
          <QueryState query={members}>
            {(data) => (
              <>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Username</TableHead>
                      <TableHead>Name</TableHead>
                      <TableHead>Role</TableHead>
                      <TableHead>Since</TableHead>
                      <TableHead />
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {data.items.length === 0 && (
                      <TableRow>
                        <TableCell colSpan={5} className="text-muted-foreground">
                          No members yet. Administrators work in every project without being members.
                        </TableCell>
                      </TableRow>
                    )}
                    {data.items.map((m) => (
                      <MemberRow key={m.user.id} member={m} manage={manage} projectKey={projectKey} />
                    ))}
                  </TableBody>
                </Table>
                <Pagination
                  page={data.page}
                  totalPages={data.totalPages}
                  totalItems={data.totalItems}
                  onPageChange={(p, replace) => setParams({ page: String(p) }, { replace })}
                />
              </>
            )}
          </QueryState>
          {manage ? <AddMember projectKey={projectKey} /> : null}
        </CardContent>
      </Card>
      <section id="classification" className="scroll-mt-4">
        <ClassificationSection projectKey={projectKey} manage={manage} />
      </section>
      {manage ? (
        <>
          <section id="api-keys" className="scroll-mt-4">
            <ApiKeysSection projectKey={projectKey} />
          </section>
          <section id="webhooks" className="scroll-mt-4">
            <WebhooksSection projectKey={projectKey} />
          </section>
          <section id="github" className="scroll-mt-4">
            <GitHubSection projectKey={projectKey} />
          </section>
        </>
      ) : null}
    </div>
  )
}
