package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAPI answers like the REST API and records what the tools asked.
type fakeAPI struct {
	got    *http.Request
	status int
	body   string
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.got = r
	w.Header().Set("Content-Type", "application/json")
	if f.status != 0 {
		w.WriteHeader(f.status)
	}
	_, _ = w.Write([]byte(f.body))
}

func server(api *fakeAPI) http.Handler {
	h := NewHandler("1.2.3")
	h.Bind(api)
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

func post(t *testing.T, h http.Handler, body string, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type rpc struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  map[string]any  `json:"result"`
	Error   *rpcError       `json:"error"`
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) rpc {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out rpc
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Equal(t, "2.0", out.JSONRPC)
	return out
}

func TestInitialize(t *testing.T) {
	h := server(&fakeAPI{})
	out := decode(t, post(t, h, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`))
	assert.Equal(t, json.RawMessage("1"), out.ID)
	assert.Equal(t, "2025-03-26", out.Result["protocolVersion"], "a supported older revision is negotiated")
	assert.Equal(t, map[string]any{"name": "provenly", "title": "Provenly", "version": "1.2.3"}, out.Result["serverInfo"])
	assert.Equal(t, map[string]any{"tools": map[string]any{"listChanged": false}}, out.Result["capabilities"])
	assert.Contains(t, out.Result["instructions"], "read-only")

	out = decode(t, post(t, h, `{"jsonrpc":"2.0","id":"a","method":"initialize","params":{"protocolVersion":"1999-01-01"}}`))
	assert.Equal(t, json.RawMessage(`"a"`), out.ID)
	assert.Equal(t, ProtocolVersion, out.Result["protocolVersion"], "an unknown revision gets the server's")
	out = decode(t, post(t, h, `{"jsonrpc":"2.0","id":2,"method":"ping"}`))
	assert.Equal(t, map[string]any{}, out.Result)
}

func TestMessages(t *testing.T) {
	h := server(&fakeAPI{})
	// Notifications and responses are accepted without a body.
	for _, body := range []string{`{"jsonrpc":"2.0","method":"notifications/initialized"}`, `{"jsonrpc":"2.0","id":null,"method":"x"}`, `{"jsonrpc":"2.0","result":{}}`} {
		rec := post(t, h, body)
		assert.Equal(t, http.StatusAccepted, rec.Code, body)
		assert.Empty(t, rec.Body.String())
	}
	cases := map[string]int{
		`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`:                                       codeInvalidRequest,
		`{"jsonrpc":"2.0","id":1`:                                                          codeParse,
		`{"jsonrpc":"1.0","id":1,"method":"ping"}`:                                         codeInvalidRequest,
		`{"jsonrpc":"2.0","id":1}`:                                                         codeInvalidRequest,
		`{"jsonrpc":"2.0","id":1,"method":"resources/list"}`:                               codeMethodNotFound,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":[]}`:                       codeInvalidParams,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"drop_database"}}`: codeInvalidParams,
	}
	for body, code := range cases {
		out := decode(t, post(t, h, body))
		require.NotNil(t, out.Error, body)
		assert.Equal(t, code, out.Error.Code, body)
		assert.Nil(t, out.Result, body)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
	rec = post(t, h, `{"jsonrpc":"2.0","id":1,"method":"ping","pad":"`+strings.Repeat("x", maxBody)+`"}`)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestToolsList(t *testing.T) {
	out := decode(t, post(t, server(&fakeAPI{}), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	list := out.Result["tools"].([]any)
	require.Len(t, list, len(tools))
	names := map[string]map[string]any{}
	for _, raw := range list {
		tl := raw.(map[string]any)
		names[tl["name"].(string)] = tl
		assert.Equal(t, map[string]any{"readOnlyHint": true, "openWorldHint": false}, tl["annotations"])
		assert.NotEmpty(t, tl["description"])
	}
	schema := names["get_test_case_history"]["inputSchema"].(map[string]any)
	assert.Equal(t, []any{"testCaseId"}, schema["required"])
	assert.Equal(t, false, schema["additionalProperties"])
	assert.Equal(t, map[string]any{"type": "integer", "minimum": 1.0, "description": testCaseID.description}, schema["properties"].(map[string]any)["testCaseId"])
	assert.Equal(t, []any{}, names["list_projects"]["inputSchema"].(map[string]any)["required"])
}

func call(t *testing.T, h http.Handler, name, args string, headers ...string) map[string]any {
	t.Helper()
	out := decode(t, post(t, h, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"`+name+`","arguments":`+args+`}}`, headers...))
	require.Nil(t, out.Error)
	return out.Result
}

func TestToolsCall(t *testing.T) {
	api := &fakeAPI{body: `{"id":12,"key":"CHK-12"}`}
	h := server(api)
	res := call(t, h, "get_test_case", `{"testCaseId":12}`, "Authorization", "Bearer s3cret", "Cookie", "provenly_session=c")
	assert.Equal(t, "/api/v1/test-cases/12", api.got.URL.String())
	assert.Equal(t, http.MethodGet, api.got.Method)
	assert.Equal(t, "Bearer s3cret", api.got.Header.Get("Authorization"), "the tool runs as the caller")
	assert.Equal(t, "provenly_session=c", api.got.Header.Get("Cookie"))
	assert.Equal(t, false, res["isError"])
	assert.Equal(t, map[string]any{"id": 12.0, "key": "CHK-12"}, res["structuredContent"])
	assert.Equal(t, []any{map[string]any{"type": "text", "text": `{"id":12,"key":"CHK-12"}`}}, res["content"])

	call(t, h, "search_test_cases", `{"project":"CHK","tag":"smoke & fast","page":2}`)
	assert.Equal(t, "/api/v1/test-cases?page=2&project=CHK&tag=smoke+%26+fast", api.got.URL.String())
	assert.Empty(t, api.got.Header.Get("Authorization"))
	call(t, h, "get_project_quality", `{"projectKey":"CHK","window":5}`)
	assert.Equal(t, "/api/v1/projects/CHK/quality?window=5", api.got.URL.RequestURI())
	call(t, h, "list_projects", `{}`)
	assert.Equal(t, "/api/v1/projects", api.got.URL.String())

	// API errors are tool errors (the agent reads the problem), not protocol errors.
	api.status, api.body = http.StatusNotFound, `{"code":"not_found","detail":"test case 99 not found"}`
	res = call(t, h, "get_test_case", `{"testCaseId":99}`)
	assert.Equal(t, true, res["isError"])
	assert.Nil(t, res["structuredContent"])
	assert.Equal(t, []any{map[string]any{"type": "text", "text": `HTTP 404: {"code":"not_found","detail":"test case 99 not found"}`}}, res["content"])
}

func TestToolArguments(t *testing.T) {
	h := server(&fakeAPI{})
	for _, args := range []string{`{}`, `{"testCaseId":0}`, `{"testCaseId":1.5}`, `{"testCaseId":"12"}`, `{"testCaseId":-3}`,
		`{"testCaseId":1e300}`, `{"testCaseId":12,"extra":1}`} {
		out := decode(t, post(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_test_case","arguments":`+args+`}}`))
		require.NotNil(t, out.Error, args)
		assert.Equal(t, codeInvalidParams, out.Error.Code, args)
	}
	for _, args := range []string{`{"projectKey":"A/B"}`, `{"projectKey":".."}`, `{"projectKey":"."}`, `{"projectKey":"a?b"}`} {
		out := decode(t, post(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_issues","arguments":`+args+`}}`))
		require.NotNil(t, out.Error, args)
		assert.Contains(t, out.Error.Message, "projectKey must be a key", args)
	}
	for _, args := range []string{`{"project":""}`, `{"project":5}`} {
		out := decode(t, post(t, h, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_test_runs","arguments":`+args+`}}`))
		require.NotNil(t, out.Error, args)
		assert.Contains(t, out.Error.Message, "project must be a non-empty string")
	}
}
