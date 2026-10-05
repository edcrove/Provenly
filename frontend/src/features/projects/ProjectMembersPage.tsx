import { useState, type FormEvent } from 'react'
import { Link, useParams, useSearchParams } from 'react-router'

import type { Member } from '@/api/client'
import { useMemberMutations, useProjectMembers } from '@/api/queries'
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
import { ClassificationSection } from './ClassificationSection'
import { useProjectRole } from './useProjectRole'

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

/** A project's members; maintainers and administrators add, change and remove them. */
export function ProjectMembersPage() {
  const { projectKey = '' } = useParams()
  const [params, setParams] = useSearchParams()
  const page = positiveInt(params.get('page'), 1)
  const members = useProjectMembers(projectKey, page)
  const manage = can(useProjectRole(projectKey), 'maintainer')
  return (
    <div className="grid gap-6">
      <Card>
        <CardHeader>
          <PageTitle title={`${projectKey} members`} />
          <CardTitle as="h1" className="text-xl">
            <span className="font-mono">{projectKey}</span> members
          </CardTitle>
          <CardDescription>
            <Link to="/projects" className="underline">
              All projects
            </Link>
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-6">
          <QueryState query={members} page>
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
      <ClassificationSection projectKey={projectKey} manage={manage} />
      {manage ? <ApiKeysSection projectKey={projectKey} /> : null}
    </div>
  )
}
