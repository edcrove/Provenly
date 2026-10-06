/**
 * The command that connects an MCP client (Claude Code as the example) to Provenly with a personal access token
 * (card #62): read-only, limited to the token's projects, and never the person's password.
 */
export function mcpSnippet(origin: string, token = 'YOUR_TOKEN') {
  return `claude mcp add --transport http provenly "${origin}/api/v1/mcp" --header "Authorization: Bearer ${token}"`
}
