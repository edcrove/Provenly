import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'

describe('FE-INT-047 sign-in throttle', () => {
  it('FE-INT-047 after five failed sign-ins the username is locked and the page says for how long', async () => {
    db.session = null
    const { user: u } = renderRoute('/login')
    for (let i = 0; i < 6; i++) {
      await u.clear(await screen.findByLabelText('Username'))
      await u.type(screen.getByLabelText('Username'), 'admin')
      await u.clear(screen.getByLabelText('Password'))
      await u.type(screen.getByLabelText('Password'), 'wrong password')
      await u.click(screen.getByRole('button', { name: /sign in/i }))
      await screen.findByRole('alert')
    }
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'too many failed sign-ins for this username; try again in 15 minutes',
    )
  })
})
