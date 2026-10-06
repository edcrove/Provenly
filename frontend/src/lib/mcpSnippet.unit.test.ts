import { describe, expect, it } from 'vitest'

import { mcpSnippet } from './mcpSnippet'

describe('FE-UNIT mcpSnippet', () => {
  it('registers the MCP endpoint with a personal access token, never a password', () => {
    const s = mcpSnippet('https://provenly.example', 'pvly_pat_0000000a_secret')
    expect(s).toBe(
      'claude mcp add --transport http provenly "https://provenly.example/api/v1/mcp" --header "Authorization: Bearer pvly_pat_0000000a_secret"',
    )
    expect(s).not.toContain('password')
    expect(s).not.toContain('/auth/login')
  })

  it('shows a placeholder until a token is made', () => {
    expect(mcpSnippet('http://x')).toContain('Authorization: Bearer YOUR_TOKEN')
  })
})
