//go:build contract

// Package contract verifies that every operation and response variant of
// api/openapi.yaml is honoured by the real backend (httpexpect + kin-openapi).
// Every exchange is validated against the spec by a recording transport; the
// validated (operationId, status) pairs are the Contract gate evidence.
package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gavv/httpexpect/v2"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"golang.org/x/crypto/bcrypt"

	"github.com/edcrove/provenly/backend/internal/app"
	"github.com/edcrove/provenly/backend/internal/identity"
	"github.com/edcrove/provenly/backend/test/testdb"
)

const specPath = "../../../api/openapi.yaml"

var (
	db       *testdb.DB
	doc      *openapi3.T
	router   routers.Router
	mu       sync.Mutex
	evidence = map[string]bool{}
)

func TestMain(m *testing.M) {
	openapi3filter.RegisterBodyDecoder("application/xml", openapi3filter.PlainBodyDecoder)
	openapi3filter.RegisterBodyDecoder("text/xml", openapi3filter.PlainBodyDecoder)
	loader := openapi3.NewLoader()
	var err error
	doc, err = loader.LoadFromFile(specPath)
	if err == nil {
		err = doc.Validate(context.Background())
	}
	if err == nil {
		// Route on paths only: the servers list points at localhost:8080.
		doc.Servers = nil
		router, err = gorillamux.NewRouter(doc)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid OpenAPI contract:", err)
		os.Exit(1)
	}
	testdb.Main(func(d *testdb.DB) int {
		db = d
		code := m.Run()
		if out := os.Getenv("CONTRACT_EVIDENCE"); out != "" && code == 0 {
			if err := writeEvidence(out); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
		}
		return code
	})
}

func writeEvidence(path string) error {
	keys := make([]string, 0, len(evidence))
	for k := range evidence {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b, err := json.MarshalIndent(map[string]any{"side": "backend", "validated": keys}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// validatingTransport validates every response (and every request that the
// server accepted with 2xx) against the contract and records the variant.
type validatingTransport struct {
	t    *testing.T
	base http.RoundTripper
}

func (v validatingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var reqBody []byte
	if req.Body != nil {
		reqBody, _ = io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(reqBody))
	}
	resp, err := v.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	respBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(respBody))

	route, pathParams, err := router.FindRoute(req)
	if err != nil {
		v.t.Errorf("contract: %s %s is not an operation of the contract: %v", req.Method, req.URL.Path, err)
		return resp, nil
	}
	checkReq := req.Clone(req.Context())
	checkReq.Body = io.NopCloser(bytes.NewReader(reqBody))
	in := &openapi3filter.RequestValidationInput{
		Request: checkReq, PathParams: pathParams, Route: route,
		Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
	}
	if resp.StatusCode < 300 {
		if err := openapi3filter.ValidateRequest(req.Context(), in); err != nil {
			v.t.Errorf("contract: server accepted a request that violates the contract (%s %s): %v", req.Method, req.URL.Path, err)
		}
	}
	out := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: in, Status: resp.StatusCode, Header: resp.Header,
		Body:    io.NopCloser(bytes.NewReader(respBody)),
		Options: &openapi3filter.Options{IncludeResponseStatus: true},
	}
	if err := openapi3filter.ValidateResponse(req.Context(), out); err != nil {
		v.t.Errorf("contract: response violates the contract (%s %s -> %d): %v\nbody: %s", req.Method, req.URL.Path, resp.StatusCode, err, respBody)
		return resp, nil
	}
	mu.Lock()
	evidence[fmt.Sprintf("%s %d", route.Operation.OperationID, resp.StatusCode)] = true
	mu.Unlock()
	return resp, nil
}

// api returns an httpexpect client, signed in as the bootstrapped administrator,
// for a backend built on the given services.
func api(t *testing.T, s app.Services, maxIngest int64) *httpexpect.Expect {
	t.Helper()
	return as(anon(t, s, maxIngest), adminToken(t))
}

// anon returns an httpexpect client without a session.
func anon(t *testing.T, s app.Services, maxIngest int64) *httpexpect.Expect {
	t.Helper()
	srv := httptest.NewServer(app.NewHandler(s, maxIngest))
	t.Cleanup(srv.Close)
	return httpexpect.WithConfig(httpexpect.Config{
		BaseURL:  srv.URL,
		Reporter: httpexpect.NewRequireReporter(t),
		Client:   &http.Client{Transport: validatingTransport{t: t, base: http.DefaultTransport}, Timeout: 30 * time.Second},
	})
}

// as sends every request of e with the given session token.
func as(e *httpexpect.Expect, token string) *httpexpect.Expect {
	return e.Builder(func(r *httpexpect.Request) { r.WithHeader("Authorization", "Bearer "+token) })
}

// The administrator every fresh database starts with, and the secret its sessions are signed with.
const adminUser, adminPassword = "admin", "correct horse"

func identityConfig() identity.Config {
	cfg := identity.DefaultConfig([]byte(strings.Repeat("k", 32)))
	cfg.BcryptCost = bcrypt.MinCost
	return cfg
}

// adminToken signs in as the administrator of the current database.
func adminToken(t *testing.T) string {
	t.Helper()
	s := app.NewServicesWith(db.Pool, time.Now, identityConfig())
	sess, err := s.Identity.Login(context.Background(), adminUser, adminPassword)
	if err != nil {
		t.Fatal(err)
	}
	return sess.Token
}

// offContract serves the same API without the contract validation, to check
// requests the contract does not define (e.g. that no method can edit a run).
func offContract(t *testing.T, s app.Services) *httpexpect.Expect {
	t.Helper()
	srv := httptest.NewServer(app.NewHandler(s, 1<<20))
	t.Cleanup(srv.Close)
	return as(httpexpect.WithConfig(httpexpect.Config{BaseURL: srv.URL, Reporter: httpexpect.NewRequireReporter(t)}), adminToken(t))
}

func fresh(t *testing.T) app.Services {
	t.Helper()
	if err := db.Reset(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := app.NewServicesWith(db.Pool, time.Now, identityConfig())
	if err := s.Identity.Bootstrap(context.Background(), adminUser, adminPassword); err != nil {
		t.Fatal(err)
	}
	return s
}
