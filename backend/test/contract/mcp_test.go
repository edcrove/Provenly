//go:build contract

package contract

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMCP: an agent initializes, lists the tools and calls them as the signed-in user; what the user cannot see is a
// tool error, not data.
func TestMCP(t *testing.T) {
	s := fresh(t)
	admin := api(t, s, 1<<20)
	e := anon(t, s, 1<<20)
	tc := admin.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "Pay by card", "automated": true}).Expect().Status(http.StatusCreated).JSON().Object()
	id := int64(tc.Value("id").Number().Raw())
	ingest(admin, "mcp", 1, report(id)).Expect().Status(http.StatusCreated)
	init := admin.POST("/api/v1/mcp").WithJSON(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "contract", "version": "1"}}}).
		Expect().Status(http.StatusOK).JSON().Object()
	init.HasValue("id", 1).Value("result").Object().HasValue("protocolVersion", "2025-06-18").Value("serverInfo").Object().HasValue("name", "provenly")
	admin.POST("/api/v1/mcp").WithJSON(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}).Expect().Status(http.StatusAccepted).NoContent()
	admin.POST("/api/v1/mcp").WithJSON(map[string]any{"jsonrpc": "2.0", "id": "l", "method": "tools/list"}).Expect().Status(http.StatusOK).
		JSON().Object().Value("result").Object().Value("tools").Array().Length().IsEqual(13)

	call := func(name string, args map[string]any) map[string]any {
		body := admin.POST("/api/v1/mcp").WithJSON(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}).
			Expect().Status(http.StatusOK).Body().Raw()
		var out struct {
			Result map[string]any `json:"result"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &out))
		return out.Result
	}
	got := call("get_test_case", map[string]any{"testCaseId": id})
	assert.Equal(t, false, got["isError"])
	assert.Equal(t, "Pay by card", got["structuredContent"].(map[string]any)["title"])
	runs := call("list_test_runs", map[string]any{"project": "TC"})
	assert.EqualValues(t, 1, runs["structuredContent"].(map[string]any)["totalItems"])
	summary := call("get_test_run_summary", map[string]any{"testRunId": 1})
	assert.Equal(t, false, summary["isError"])
	missing := call("get_test_case", map[string]any{"testCaseId": 987654})
	assert.Equal(t, true, missing["isError"])
	assert.True(t, strings.HasPrefix(missing["content"].([]any)[0].(map[string]any)["text"].(string), "HTTP 404:"))
	admin.POST("/api/v1/mcp").WithJSON(map[string]any{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "get_test_case", "arguments": map[string]any{}}}).
		Expect().Status(http.StatusOK).JSON().Object().Value("error").Object().HasValue("code", -32602)
	admin.POST("/api/v1/mcp").WithText(`{}`).Expect().Status(http.StatusUnsupportedMediaType)
	admin.POST("/api/v1/mcp").WithHeader("Content-Type", "application/json").WithText(`{"pad":"` + strings.Repeat("x", 1<<20) + `"}`).
		Expect().Status(http.StatusRequestEntityTooLarge)

	// A user without access to the project sees nothing through the agent either.
	inv := admin.POST("/api/v1/invitations").WithJSON(map[string]any{}).Expect().Status(http.StatusCreated).JSON().Object()
	ana := as(e, e.POST("/api/v1/invitations/accept").WithJSON(map[string]any{"token": inv.Value("token").String().Raw(), "username": "ana",
		"displayName": "Ana", "password": "ana's password"}).Expect().Status(http.StatusCreated).JSON().Object().Value("token").String().Raw())
	hidden := ana.POST("/api/v1/mcp").WithJSON(map[string]any{"jsonrpc": "2.0", "id": 4, "method": "tools/call",
		"params": map[string]any{"name": "get_test_case", "arguments": map[string]any{"testCaseId": id}}}).Expect().Status(http.StatusOK).JSON().Object().Value("result").Object()
	hidden.HasValue("isError", true)
	hidden.Value("content").Array().Value(0).Object().Value("text").String().HasPrefix("HTTP 404")
}
