/**
 * The commands that connect an MCP client (Claude Code as the example) to Provenly as this user: sign in for a
 * session token, then register the HTTP endpoint with it. The password is typed by the user, never filled in.
 */
export function mcpSnippet(origin: string, username: string) {
  const login = JSON.stringify({ username, password: 'YOUR_PASSWORD' })
  return [
    `TOKEN=$(curl -fsS -X POST "${origin}/api/v1/auth/login" -H "Content-Type: application/json" \\`,
    `  -d '${login.replaceAll("'", "'\\''")}' | jq -r .token)`,
    `claude mcp add --transport http provenly "${origin}/api/v1/mcp" --header "Authorization: Bearer $TOKEN"`,
  ].join('\n')
}
