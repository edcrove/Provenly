import { describe, expect, it } from 'vitest'

import { mcpSnippet } from './mcpSnippet'

describe('FE-UNIT mcpSnippet', () => {
  it('signs in as the user and registers the MCP endpoint with the session token', () => {
    const s = mcpSnippet('https://provenly.example', 'ana')
    expect(s).toContain(`curl -fsS -X POST "https://provenly.example/api/v1/auth/login"`)
    expect(s).toContain(`-d '{"username":"ana","password":"YOUR_PASSWORD"}'`)
    expect(s).toContain(
      'claude mcp add --transport http provenly "https://provenly.example/api/v1/mcp" --header "Authorization: Bearer $TOKEN"',
    )
  })

  it("quotes usernames for the shell (a ' cannot end the argument)", () => {
    expect(mcpSnippet('http://x', "o'neil")).toContain(
      `-d '{"username":"o'\\''neil","password":"YOUR_PASSWORD"}'`,
    )
  })
})
