import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { db } from '@/test/mockApi'
import { renderRoute } from '@/test/render'

describe('FE-INT-045 agents (MCP) on the account page', () => {
  it('FE-INT-045 shows how to connect an MCP client with a personal access token, never a password', async () => {
    renderRoute('/account')
    const snippet = await screen.findByTestId('mcp-snippet')
    expect(snippet).toHaveTextContent(
      `claude mcp add --transport http provenly "${window.location.origin}/api/v1/mcp" --header "Authorization: Bearer YOUR_TOKEN"`,
    )
    expect(snippet).not.toHaveTextContent('/auth/login')
    expect(snippet).not.toHaveTextContent('password')
    expect(screen.getByRole('heading', { name: 'Agents (MCP)' })).toBeInTheDocument()
  })
})

describe('FE-INT-057 personal access tokens (card #62)', () => {
  it('FE-INT-057 makes a token over chosen projects, shows it once with its MCP command, lists and revokes it', async () => {
    const { user: u } = renderRoute('/account')
    const card = await screen.findByTestId('tokens')
    expect(await within(card).findByText('No tokens yet.')).toBeInTheDocument()
    expect(within(card).getByRole('button', { name: 'Create token' })).toBeDisabled()
    expect(within(card).getByLabelText('Expires in')).toHaveValue('90')

    await u.type(within(card).getByLabelText('Token name'), 'Claude Desktop')
    await u.click(await within(card).findByRole('checkbox', { name: /^TC/ }))
    await u.selectOptions(within(card).getByLabelText('Expires in'), '30')
    await u.click(within(card).getByRole('button', { name: 'Create token' }))

    const secret = await within(card).findByTestId('token-secret')
    expect(secret.textContent).toMatch(/^pvly_pat_[0-9a-f]{8}_/)
    expect(within(card).getByTestId('token-mcp-snippet')).toHaveTextContent(
      `Authorization: Bearer ${secret.textContent}`,
    )
    expect(db.tokens[0]).toMatchObject({ name: 'Claude Desktop', projects: ['TC'] })
    expect(Date.parse(db.tokens[0].expiresAt) - Date.parse(db.tokens[0].createdAt)).toBe(30 * 86_400_000)
    const row = await within(card).findByTestId(`token-${db.tokens[0].id}`)
    expect(row).toHaveTextContent('Claude Desktop')
    expect(row).toHaveTextContent('TC')
    expect(row).toHaveTextContent('active')
    expect(row).toHaveTextContent('Never')
    expect(row).not.toHaveTextContent(secret.textContent!.slice(20))
    expect(within(card).getByLabelText('Token name')).toHaveValue('')

    // Revoking asks first (Cancel focused); confirming revokes.
    await u.click(within(row).getByRole('button', { name: 'Revoke…' }))
    expect(within(row).getByRole('button', { name: 'Cancel' })).toHaveFocus()
    await u.click(within(row).getByRole('button', { name: 'Revoke' }))
    await waitFor(() => expect(row).toHaveTextContent('revoked'))
    expect(within(row).queryByRole('button', { name: 'Revoke…' })).not.toBeInTheDocument()
  })

  it('FE-INT-057 says why a token cannot be made', async () => {
    const { user: u } = renderRoute('/account')
    const card = await screen.findByTestId('tokens')
    await u.type(within(card).getByLabelText('Token name'), '   ')
    await u.click(await within(card).findByRole('checkbox', { name: /^TC/ }))
    await u.click(within(card).getByRole('button', { name: 'Create token' }))
    expect(await within(card).findByText('Could not create the token')).toBeInTheDocument()
    expect(db.tokens).toHaveLength(0)
  })
})
