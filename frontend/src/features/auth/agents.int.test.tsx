import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { renderRoute } from '@/test/render'

describe('FE-INT-045 agents (MCP) on the account page', () => {
  it('FE-INT-045 shows how to connect an MCP client as the signed-in user', async () => {
    renderRoute('/account')
    const snippet = await screen.findByTestId('mcp-snippet')
    expect(snippet).toHaveTextContent(`${window.location.origin}/api/v1/auth/login`)
    expect(snippet).toHaveTextContent('"username":"admin"')
    expect(snippet).toHaveTextContent(
      `claude mcp add --transport http provenly "${window.location.origin}/api/v1/mcp"`,
    )
    expect(screen.getByRole('heading', { name: 'Agents (MCP)' })).toBeInTheDocument()
  })
})
