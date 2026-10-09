import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent, { type UserEvent } from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router'

import { createQueryClient } from '@/api/queryClient'
import { routes } from '@/app/routes'

/** Renders the real route tree at path, against the MSW-mocked API. */
export function renderRoute(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  const queryClient = createQueryClient()
  const user = userEvent.setup()
  const view = render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
  return { ...view, router, user }
}

/** The header's project switcher (its name says the current project). */
export const projectSwitcher = () => screen.getByRole('button', { name: /^Current project: / })

/** Chooses a project in the header's switcher ('' for every project). */
export async function pickProject(user: UserEvent, key: string) {
  await user.click(projectSwitcher())
  await user.click(
    await screen.findByRole('option', { name: key ? new RegExp(`^${key} · `) : 'All projects' }),
  )
}
