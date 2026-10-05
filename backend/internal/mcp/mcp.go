// Package mcp is the agent interface of Provenly (Incubator, DEC-10): a Model Context Protocol server over Streamable
// HTTP (JSON responses, no server-sent events) at POST /api/v1/mcp. Its tools are read-only and each one is a call to
// the public REST API made in-process with the caller's own credentials, so agents see exactly what the caller may see
// through the API, with the same validation and the same JSON shapes (api/openapi.yaml).
package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/edcrove/provenly/backend/internal/platform/httpx"
)

// ProtocolVersion is the MCP revision the server speaks (it accepts the older ones in SupportedVersions).
const ProtocolVersion = "2025-06-18"

// SupportedVersions are the MCP revisions a client may negotiate.
var SupportedVersions = []string{ProtocolVersion, "2025-03-26"}

// maxBody bounds a JSON-RPC message.
const maxBody = 1 << 20

// JSON-RPC error codes.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// Handler serves the MCP endpoint; Bind gives it the API its tools call.
type Handler struct {
	api     http.Handler
	version string
}

// NewHandler builds a Handler reporting the given server version.
func NewHandler(version string) *Handler { return &Handler{version: version} }

// Bind sets the REST API the tools call (the application's own handler, built after the routes).
func (h *Handler) Bind(api http.Handler) { h.api = api }

// Register mounts the MCP endpoint (a session route: agents act as the signed-in user).
func (h *Handler) Register(mux httpx.Router) {
	mux.HandleFunc("POST /api/v1/mcp", h.serve)
}

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

var null = json.RawMessage("null")

func reply(w http.ResponseWriter, id json.RawMessage, result any, err *rpcError) {
	httpx.WriteJSON(w, http.StatusOK, response{JSONRPC: "2.0", ID: id, Result: result, Error: err})
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		httpx.WriteError(w, r, httpx.ErrUnsupportedMediaType)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 && trimmed[0] == '[' {
		reply(w, null, nil, &rpcError{codeInvalidRequest, "batches are not supported"})
		return
	}
	var msg message
	if err := json.Unmarshal(body, &msg); err != nil {
		reply(w, null, nil, &rpcError{codeParse, "parse error: " + err.Error()})
		return
	}
	if len(msg.ID) == 0 || bytes.Equal(msg.ID, null) {
		// A notification (e.g. notifications/initialized) or a response to the server: nothing to answer.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if msg.JSONRPC != "2.0" || msg.Method == "" {
		reply(w, msg.ID, nil, &rpcError{codeInvalidRequest, `a request needs "jsonrpc": "2.0" and a method`})
		return
	}
	result, rerr := h.dispatch(r, msg)
	reply(w, msg.ID, result, rerr)
}

func (h *Handler) dispatch(r *http.Request, msg message) (any, *rpcError) {
	switch msg.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		version := ProtocolVersion
		if slices.Contains(SupportedVersions, p.ProtocolVersion) {
			version = p.ProtocolVersion
		}
		return map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "provenly", "title": "Provenly", "version": h.version},
			"instructions": "Provenly links test cases (TC-<id> keys such as CHK-12), CI results and history. " +
				"Tools are read-only and see what your user may see. Start with list_projects or search_test_cases.",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		list := make([]map[string]any, len(tools))
		for i, t := range tools {
			list[i] = t.describe()
		}
		return map[string]any{"tools": list}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			return nil, &rpcError{codeInvalidParams, "params must be {name, arguments}"}
		}
		i := slices.IndexFunc(tools, func(t tool) bool { return t.name == p.Name })
		if i < 0 {
			return nil, &rpcError{codeInvalidParams, fmt.Sprintf("unknown tool %q", p.Name)}
		}
		target, err := tools[i].target(p.Arguments)
		if err != nil {
			return nil, &rpcError{codeInvalidParams, err.Error()}
		}
		return h.call(r, target), nil
	}
	return nil, &rpcError{codeMethodNotFound, fmt.Sprintf("method %q not found", msg.Method)}
}

// call runs a GET on the API as the caller and turns the answer into a tool result.
func (h *Handler) call(r *http.Request, target string) map[string]any {
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	for _, name := range []string{"Authorization", "Cookie"} {
		if v := r.Header.Get(name); v != "" {
			req.Header.Set(name, v)
		}
	}
	rec := &recorder{header: http.Header{}, status: http.StatusOK}
	h.api.ServeHTTP(rec, req)
	text := strings.TrimSpace(rec.body.String())
	if rec.status >= 300 {
		return map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": fmt.Sprintf("HTTP %d: %s", rec.status, text)}}}
	}
	var structured map[string]any
	_ = json.Unmarshal(rec.body.Bytes(), &structured)
	return map[string]any{"isError": false, "content": []map[string]any{{"type": "text", "text": text}}, "structuredContent": structured}
}

// recorder captures an in-process API response.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header         { return r.header }
func (r *recorder) Write(b []byte) (int, error) { return r.body.Write(b) }
func (r *recorder) WriteHeader(status int)      { r.status = status }

// param is a tool argument: a path segment or a query parameter.
type param struct {
	name, kind, description string
	path, required          bool
}

// tool is a read-only API operation exposed to agents.
type tool struct {
	name, title, description, path string
	params                         []param
}

func (t tool) describe() map[string]any {
	props := map[string]any{}
	required := []string{}
	for _, p := range t.params {
		schema := map[string]any{"type": p.kind, "description": p.description}
		if p.kind == "integer" {
			schema["minimum"] = 1
		}
		props[p.name] = schema
		if p.required {
			required = append(required, p.name)
		}
	}
	return map[string]any{
		"name": t.name, "title": t.title, "description": t.description,
		"inputSchema": map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false},
		"annotations": map[string]any{"readOnlyHint": true, "openWorldHint": false},
	}
}

var errArguments = errors.New("invalid arguments")

var segment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// target validates the arguments and builds the API URL they address.
func (t tool) target(args map[string]any) (string, error) {
	for name := range args {
		if !slices.ContainsFunc(t.params, func(p param) bool { return p.name == name }) {
			return "", fmt.Errorf("%w: unknown argument %q", errArguments, name)
		}
	}
	path, query := t.path, url.Values{}
	for _, p := range t.params {
		v, ok := args[p.name]
		if !ok {
			if p.required {
				return "", fmt.Errorf("%w: %s is required", errArguments, p.name)
			}
			continue
		}
		s, err := p.format(v)
		if err != nil {
			return "", err
		}
		if p.path {
			path = strings.Replace(path, "{"+p.name+"}", url.PathEscape(s), 1)
		} else {
			query.Set(p.name, s)
		}
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return path, nil
}

func (p param) format(v any) (string, error) {
	if p.kind == "integer" {
		n, ok := v.(float64)
		if !ok || n != float64(int64(n)) || n < 1 || n > 1<<53 {
			return "", fmt.Errorf("%w: %s must be a positive integer", errArguments, p.name)
		}
		return strconv.FormatInt(int64(n), 10), nil
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("%w: %s must be a non-empty string", errArguments, p.name)
	}
	if p.path && !segment.MatchString(s) {
		// A path segment never carries "/" or dot segments, which would address another route.
		return "", fmt.Errorf("%w: %s must be a key (letters, digits, - and _)", errArguments, p.name)
	}
	return s, nil
}

var (
	page        = param{name: "page", kind: "integer", description: "Page number (20 items per page), default 1."}
	projectArg  = param{name: "project", kind: "string", description: "Project key (e.g. CHK); default every project you can see."}
	projectPath = param{name: "projectKey", kind: "string", path: true, required: true, description: "Project key, e.g. CHK."}
	testCaseID  = param{name: "testCaseId", kind: "integer", path: true, required: true, description: "Numeric id of the test case (CHK-12 has the id shown by search_test_cases)."}
	testRunID   = param{name: "testRunId", kind: "integer", path: true, required: true, description: "Numeric id of the test run."}
)

var tools = []tool{
	{name: "list_projects", title: "List projects", path: "/api/v1/projects", params: []param{page},
		description: "Projects you can see, with their keys (the prefix of their test case ids)."},
	{name: "search_test_cases", title: "Search test cases", path: "/api/v1/test-cases",
		description: "Test cases, newest first, narrowed by project, status (active, deprecated), tag, classification (dimension:value) or suite.",
		params: []param{projectArg, {name: "status", kind: "string", description: "active or deprecated."},
			{name: "tag", kind: "string", description: "A tag every result must carry."},
			{name: "classification", kind: "string", description: "dimension:value, e.g. risk:high."},
			{name: "suite", kind: "string", description: "A suite key of the project."}, page}},
	{name: "get_test_case", title: "Get a test case", path: "/api/v1/test-cases/{testCaseId}", params: []param{testCaseID},
		description: "A test case with its key, status, automation, tags and classification."},
	{name: "list_test_steps", title: "List test steps", path: "/api/v1/test-cases/{testCaseId}/steps", params: []param{testCaseID, page},
		description: "The ordered steps (action and expected result) of a test case."},
	{name: "get_test_case_history", title: "Test case history", path: "/api/v1/test-cases/{testCaseId}/results", params: []param{testCaseID, page},
		description: "The results of a test case across runs, newest first (status, attempt, run, failure message)."},
	{name: "list_test_runs", title: "List test runs", path: "/api/v1/test-runs",
		description: "Test runs, newest first, with their execution status and outcome (verdict, pass rate, flaky).",
		params:      []param{projectArg, {name: "suite", kind: "string", description: "Only runs reported for this suite key."}, page}},
	{name: "get_test_run", title: "Get a test run", path: "/api/v1/test-runs/{testRunId}", params: []param{testRunID},
		description: "A test run: CI metadata, execution status and outcome."},
	{name: "get_test_run_summary", title: "Test run summary", path: "/api/v1/test-runs/{testRunId}/summary", params: []param{testRunID},
		description: "Per test case outcome of a run against its snapshot of expected test cases, with counts and percentages."},
	{name: "list_test_run_results", title: "Test run results", path: "/api/v1/test-runs/{testRunId}/results",
		description: "Individual results of a run, filtered by status (passed, failed, error, skipped) or TC-ID correlation.",
		params: []param{testRunID, {name: "status", kind: "string", description: "passed, failed, error or skipped."},
			{name: "correlation", kind: "string", description: "valid, missing, malformed, unknown, deprecated or wrong_project."}, page}},
	{name: "get_live_run", title: "Live run state", path: "/api/v1/test-runs/{testRunId}/live", params: []param{testRunID},
		description: "Progress of a live run (per test case state) or, once finished, its reconciliation."},
	{name: "get_project_quality", title: "Project quality", path: "/api/v1/projects/{projectKey}/quality",
		description: "Automation rate, test cases not executed recently and flaky test cases of a project.",
		params: []param{projectPath, {name: "staleDays", kind: "integer", description: "Days without execution that make a test case stale (default 14)."},
			{name: "window", kind: "integer", description: "Latest runs flakiness is counted over (default 20)."}}},
	{name: "list_issues", title: "List issues", path: "/api/v1/projects/{projectKey}/issues",
		description: "Issues of a project with their QA verification (known issue, reopen, validated fixed…).",
		params: []param{projectPath, {name: "state", kind: "string", description: "open or closed."},
			{name: "testCase", kind: "integer", description: "Only issues linked to this test case id."}}},
	{name: "list_requirements", title: "List requirements", path: "/api/v1/projects/{projectKey}/requirements",
		description: "Requirements of a project with their test coverage (not run, failing, passing, partial).",
		params:      []param{projectPath, {name: "testCase", kind: "integer", description: "Only requirements this test case id covers."}}},
}
