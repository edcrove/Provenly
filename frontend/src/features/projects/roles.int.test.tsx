import { screen, waitFor, within } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import type { MemberRole } from '@/lib/roles'
import { invitation, project, user } from '@/test/fixtures'
import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'
import { server } from '@/test/server'

const at = '2026-10-05T10:00:00Z'
/** Signs in as Ana, a non-admin with the given role in the default project TC. */
const signInAs = (role: MemberRole) => {
  db.users.push(user({ id: 2, username: 'ana', displayName: 'Ana Pérez', isAdmin: false }))
  db.members.push({ projectId: 1, userId: 2, role, since: at })
  db.session = 2
}

describe('FE-INT-030 what each role can do', () => {
  it('FE-INT-030 a viewer reads but sees no editing actions', async () => {
    signInAs('viewer')
    db.projects.push(project({ id: 2, key: 'CHK', name: 'Checkout' }))
    const { user: u, router } = renderRoute('/test-cases')
    expect(await screen.findByText('Login works')).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByRole('link', { name: 'New test case' })).not.toBeInTheDocument())

    await router.navigate('/test-cases/153')
    expect(await screen.findByRole('heading', { name: /TC-153/ })).toBeInTheDocument()
    expect(await screen.findByText(/Open the login page/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Edit' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Deprecate' })).not.toBeInTheDocument()
    expect(screen.queryByRole('form', { name: 'Add step' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Delete step/ })).not.toBeInTheDocument()

    await u.click(screen.getByRole('link', { name: 'Projects' }))
    const row = await screen.findByTestId('project-TC')
    expect(within(row).getByTestId('my-role')).toHaveTextContent('viewer')
    expect(within(row).queryByRole('button', { name: 'Rename' })).not.toBeInTheDocument()
    expect(screen.queryByTestId('project-CHK')).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'New project' })).not.toBeInTheDocument()
  })

  it('FE-INT-030 a member edits and creates; deprecation stays with maintainers', async () => {
    signInAs('member')
    db.projects.push(project({ id: 2, key: 'CHK', name: 'Checkout' }))
    db.members.push({ projectId: 2, userId: 2, role: 'viewer', since: at })
    const { router } = renderRoute('/test-cases/153')
    expect(await screen.findByRole('button', { name: 'Edit' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Deprecate' })).not.toBeInTheDocument()
    expect(await screen.findByRole('form', { name: 'Add step' })).toBeInTheDocument()

    await router.navigate('/test-cases/new')
    const select = await screen.findByLabelText('Project')
    await waitFor(() => expect(within(select).getAllByRole('option')).toHaveLength(1))
    expect(within(select).getByRole('option')).toHaveTextContent('TC')
  })

  it('FE-INT-030 a maintainer deprecates and renames', async () => {
    signInAs('maintainer')
    const { router } = renderRoute('/test-cases/153')
    expect(await screen.findByRole('button', { name: 'Deprecate' })).toBeInTheDocument()
    await router.navigate('/projects')
    expect(
      await within(await screen.findByTestId('project-TC')).findByRole('button', { name: 'Rename' }),
    ).toBeInTheDocument()
  })

  it('FE-INT-030 a manual test case outside a run universe offers "Mark as automated" only to members', async () => {
    signInAs('viewer')
    db.testCases[1] = { ...db.testCases[1], automated: false }
    db.summaries[7] = { ...db.summaries[7], outsideUniverse: 1, outsideUniverseTestCaseIds: [154] }
    renderRoute('/test-runs/7')
    const item = await screen.findByTestId('outside-154')
    expect(await within(item).findByText('Logout works')).toBeInTheDocument()
    expect(within(item).queryByRole('button', { name: 'Mark as automated' })).not.toBeInTheDocument()
  })
})

describe('FE-INT-030 manual test cases with results', () => {
  it('FE-INT-030 viewers are told about the manual test case but cannot mark it automated', async () => {
    signInAs('viewer')
    db.testCases[0] = { ...db.testCases[0], automated: false }
    renderRoute('/test-cases/153')
    expect(await screen.findByTestId('manual-with-results')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Mark as automated' })).not.toBeInTheDocument()
  })
})

describe('FE-INT-031 project members', () => {
  it('FE-INT-031 an administrator adds, changes and removes members', async () => {
    db.users.push(user({ id: 2, username: 'ana', displayName: 'Ana Pérez', isAdmin: false }))
    const { user: u } = renderRoute('/projects')
    await u.click(within(await screen.findByTestId('project-TC')).getByRole('link', { name: 'Members' }))
    expect(await screen.findByText(/No members yet/)).toBeInTheDocument()

    await u.type(screen.getByLabelText('Username'), 'nobody')
    await u.click(screen.getByRole('button', { name: 'Add member' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('user nobody not found')

    await u.clear(screen.getByLabelText('Username'))
    await u.type(screen.getByLabelText('Username'), 'ana')
    await u.selectOptions(screen.getByRole('combobox', { name: 'Role' }), 'viewer')
    await u.click(screen.getByRole('button', { name: 'Add member' }))
    const row = await screen.findByTestId('member-ana')
    expect(within(row).getByRole('combobox', { name: 'Role of ana' })).toHaveValue('viewer')
    expect(screen.getByLabelText('Username')).toHaveValue('')

    await u.selectOptions(within(row).getByRole('combobox', { name: 'Role of ana' }), 'maintainer')
    await waitFor(() => expect(db.members[0].role).toBe('maintainer'))
    await u.click(within(row).getByRole('button', { name: 'Remove' }))
    expect(await screen.findByText(/No members yet/)).toBeInTheDocument()
    expect(db.members).toHaveLength(0)
  })

  it('FE-INT-031 viewers see the members without controls; failures are reported', async () => {
    signInAs('viewer')
    renderRoute('/projects/TC')
    const row = await screen.findByTestId('member-ana')
    expect(row).toHaveTextContent('viewer')
    expect(within(row).queryByRole('combobox')).not.toBeInTheDocument()
    expect(screen.queryByRole('form', { name: 'Add member' })).not.toBeInTheDocument()
    expect(document.title).toBe('TC members · Provenly')
  })

  it('FE-INT-031 a failed change on a row is shown there', async () => {
    db.users.push(user({ id: 2, username: 'ana', isAdmin: false }))
    db.members.push({ projectId: 1, userId: 2, role: 'member', since: at })
    const { user: u } = renderRoute('/projects/TC')
    const row = await screen.findByTestId('member-ana')
    server.use(
      http.delete('*/api/v1/projects/:key/members/:username', () =>
        HttpResponse.json(
          {
            type: 'about:blank',
            title: 'Forbidden',
            status: 403,
            code: 'forbidden',
            detail: 'no longer allowed',
          },
          { status: 403, headers: { 'Content-Type': 'application/problem+json' } },
        ),
      ),
    )
    await u.click(within(row).getByRole('button', { name: 'Remove' }))
    expect(await within(row).findByText('Could not update the member')).toBeInTheDocument()
  })

  it('FE-INT-031 lists many members by page and reports an unknown project', async () => {
    for (let i = 0; i < 21; i++) {
      db.users.push(user({ id: 100 + i, username: `user${String(i).padStart(2, '0')}`, isAdmin: false }))
      db.members.push({ projectId: 1, userId: 100 + i, role: 'viewer', since: at })
    }
    const { user: u, router } = renderRoute('/projects/TC')
    await screen.findByTestId('member-user00')
    await u.click(screen.getByRole('button', { name: /next/i }))
    expect(await screen.findByTestId('member-user20')).toBeInTheDocument()
    expect(router.state.location.search).toBe('?page=2')
    await router.navigate('/projects/NOPE')
    expect(await screen.findByRole('alert')).toHaveTextContent('project NOPE not found')
  })
})

describe('FE-INT-032 invitations into a project', () => {
  it('FE-INT-032 an invitation can make the new account a project member', async () => {
    db.projects.push(project({ id: 2, key: 'CHK', name: 'Checkout' }))
    db.invitations.push(invitation({ id: 80, projectId: 2, projectRole: 'member' }))
    db.invitations.push(invitation({ id: 81, projectId: 99, projectRole: 'viewer' }))
    const { user: u } = renderRoute('/users')
    expect(
      within(await screen.findByTestId('invitation-80')).getByTestId('invitation-grant'),
    ).toHaveTextContent('CHK · member')
    expect(within(screen.getByTestId('invitation-81')).getByTestId('invitation-grant')).toHaveTextContent(
      '? · viewer',
    )
    expect(screen.getByLabelText('As')).toBeDisabled()
    await u.selectOptions(screen.getByLabelText('Joins project (optional)'), 'CHK')
    await u.selectOptions(screen.getByLabelText('As'), 'viewer')
    await u.click(screen.getByRole('button', { name: 'Create invitation link' }))
    await screen.findByTestId('invitation-link')
    expect(db.invitations.at(-1)).toMatchObject({ projectId: 2, projectRole: 'viewer' })
    expect(screen.getByLabelText('Joins project (optional)')).toHaveValue('')
    expect(
      within(screen.getByTestId(`invitation-${db.invitations.at(-1)!.id}`)).getByTestId('invitation-grant'),
    ).toHaveTextContent('CHK · viewer')
  })
})
