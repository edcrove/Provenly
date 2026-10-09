import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'

const at = '2026-10-05T10:00:00Z'

/** A viewer of every project they can see (as a shared link would bring them to a write form). */
function signInAsViewer() {
  db.users.push({ ...db.users[0], id: 2, username: 'vera', isAdmin: false })
  db.members.push({ projectId: 1, userId: 2, role: 'viewer', since: at })
  db.session = 2
}

describe('FE-INT-058 forms a role cannot use', () => {
  it.each([
    [
      '/test-cases/new',
      'New test case',
      'Creating test cases needs the member role (or higher) in a project.',
    ],
    [
      '/test-runs/manual',
      'New manual run',
      'Starting manual runs needs the member role (or higher) in a project.',
    ],
  ])(
    'FE-INT-058 a viewer opening %s by URL is told why instead of getting a form',
    async (path, title, reason) => {
      signInAsViewer()
      renderRoute(path)
      expect(await screen.findByText('Not allowed')).toBeInTheDocument()
      expect(screen.getByRole('heading', { name: title })).toBeInTheDocument()
      expect(screen.getByText(reason)).toBeInTheDocument()
      expect(
        screen.queryByRole('button', { name: /Create test case|Start manual run/ }),
      ).not.toBeInTheDocument()
      expect(screen.queryByLabelText('Project')).not.toBeInTheDocument()
    },
  )
})
