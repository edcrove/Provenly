// Package audit keeps the audit log (Incubator, Project & Authorization): every authenticated change made through the
// API is recorded with who made it, which operation, the request path and the project it addressed, never the request
// body. The log is append-only, read by administrators and, for the projects they maintain, by maintainers.
package audit

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/edcrove/provenly/backend/internal/identity"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/auditnote"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/clientinfo"
	"github.com/edcrove/provenly/backend/internal/platform/httpx"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

// Event is one recorded change.
type Event struct {
	ID         int64
	OccurredAt time.Time
	Actor      string
	Action     string
	Path       string
	ProjectKey string
	Status     int32
	// Summary says what the change did ("edited CHK-4 step 3"); empty for events recorded before it existed.
	Summary string
	// TestCaseKey is the test case the change touched (CHK-4), if any.
	TestCaseKey string
	// IP and UserAgent are the client's (card #49); empty for events recorded before.
	IP        string
	UserAgent string
}

// Filter narrows the log; empty fields match everything.
type Filter struct {
	ProjectKey  string
	Actor       string
	TestCaseKey string
	// ProjectKeys, when not nil, keeps only events filed under these projects (a maintainer's view).
	ProjectKeys []string
}

// Repository is the persistence port of the audit module.
type Repository interface {
	Insert(ctx context.Context, e Event) error
	List(ctx context.Context, f Filter, limit, offset int32) ([]Event, error)
	Count(ctx context.Context, f Filter) (int64, error)
}

// Access says what the caller may read of the log: everything (administrators) or their projects' events.
type Access interface {
	Scope(ctx context.Context) (authz.Scope, error)
}

// Service records and lists audit events.
type Service struct {
	repo     Repository
	access   Access
	resolver Resolver
}

// NewService builds a Service.
func NewService(repo Repository, access Access) *Service { return &Service{repo: repo, access: access} }

// SetResolver lets the log name the projects, test cases and steps that changes touched.
func (s *Service) SetResolver(r Resolver) { s.resolver = r }

// Page is one page of events.
type Page = pagination.Result[Event]

// Events lists the log, newest first: all of it for administrators, their projects' events for maintainers.
func (s *Service) Events(ctx context.Context, f Filter, page pagination.Page) (Page, error) {
	var v apperr.Validator
	v.Check(utf8.RuneCountInString(f.Actor) <= 200, "actor", "must be at most 200 characters")
	v.CheckText("actor", f.Actor)
	v.Check(utf8.RuneCountInString(f.ProjectKey) <= 50, "project", "must be at most 50 characters")
	v.Check(utf8.RuneCountInString(f.TestCaseKey) <= 40, "testCase", "must be at most 40 characters")
	v.CheckText("project", f.ProjectKey)
	if err := v.Err(); err != nil {
		return Page{}, err
	}
	// Reading the log is administration: a personal access token never does it (card #62).
	if _, byToken := identity.TokenFrom(ctx); byToken {
		return Page{}, identity.ErrTokenAdmin
	}
	scope, err := s.access.Scope(ctx)
	if err != nil {
		return Page{}, err
	}
	if !scope.All {
		if f.ProjectKeys, err = s.maintained(ctx, scope); err != nil {
			return Page{}, err
		}
		if f.ProjectKey != "" && !slices.Contains(f.ProjectKeys, f.ProjectKey) {
			return Page{}, apperr.Forbidden("you can read the audit log of the projects you maintain")
		}
	}
	items, err := s.repo.List(ctx, f, page.Limit(), page.Offset())
	if err != nil {
		return Page{}, err
	}
	if !scope.All {
		// Where a change came from (address, browser) stays with administrators.
		for i := range items {
			items[i].IP, items[i].UserAgent = "", ""
		}
	}
	total, err := s.repo.Count(ctx, f)
	if err != nil {
		return Page{}, err
	}
	return Page{Items: items, Page: page, Total: total}, nil
}

// maintained returns the keys of the projects the caller maintains (P20-5, Ed 2026-10-09): their events are what a
// maintainer reads. Events without a project (sign-ins, accounts, invitations) stay with administrators.
func (s *Service) maintained(ctx context.Context, scope authz.Scope) ([]string, error) {
	var keys []string
	for id, role := range scope.Roles {
		if role < authz.RoleMaintainer || s.resolver == nil {
			continue
		}
		key, err := s.resolver.ProjectKey(ctx, id)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil, apperr.Forbidden("only administrators and project maintainers can read the audit log")
	}
	slices.Sort(keys)
	return keys, nil
}

// actor names who made the request: the signed-in user, or the API key (its name and prefix).
func actor(ctx context.Context) string {
	if u, ok := identity.UserFrom(ctx); ok {
		return u.Username
	}
	if k, ok := identity.APIKeyFrom(ctx); ok {
		return truncate(fmt.Sprintf("api key %s… (%s)", k.Prefix, k.Name), 200)
	}
	return "unknown"
}

// storable is s as PostgreSQL can store it (valid UTF-8, no NUL): paths are decoded and may carry anything.
func storable(s string) string { return strings.ToValidUTF8(strings.ReplaceAll(s, "\x00", ""), "?") }

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// project is the key of the project a change addressed: in its path or ?project=, else the one its authorization
// allowed (a test case, a step or a run is attributed to its project).
func (s *Service) project(r *http.Request, note *auditnote.Note) string {
	if p := r.PathValue("projectKey"); p != "" {
		return p
	}
	if p := r.URL.Query().Get("project"); p != "" {
		return p
	}
	if id := note.ProjectID(); id != 0 && s.resolver != nil {
		if key, err := s.resolver.ProjectKey(r.Context(), id); err == nil {
			return key
		}
	}
	return ""
}

// record stores the event of a successful change; a failure is logged, never returned (the change happened).
func (s *Service) record(r *http.Request, action string, status int, note *auditnote.Note, t target) {
	summary, testCase := t.summary(action), t.testCaseKey
	// A creation names what it made (deployed audit: "created a test case" did not say which).
	if created := note.Created(); created != "" && summary != "" {
		summary += " " + created
	}
	if testCase == "" {
		testCase = note.TestCaseKey()
	}
	e := Event{
		Actor: actor(r.Context()), Action: action, Path: truncate(storable(r.URL.Path), 2000),
		ProjectKey: truncate(storable(s.project(r, note)), 50), Status: int32(status),
		Summary: truncate(storable(summary), 300), TestCaseKey: testCase,
	}
	client := clientinfo.From(r.Context())
	e.IP, e.UserAgent = client.IP, truncate(storable(client.UserAgent), 500)
	if err := s.repo.Insert(context.WithoutCancel(r.Context()), e); err != nil {
		slog.ErrorContext(r.Context(), "audit event not recorded", "action", action, "error", err)
	}
}

// Unaudited routes: reads disguised as POST (MCP) and the high-volume live event stream of CI.
var unaudited = []string{"POST /api/v1/mcp", "POST /api/v1/test-runs/{testRunId}/events"}

// Router records the successful changes made through the routes it registers. Wrap it around a router that
// authenticates (identity.Protect), so the handler it wraps runs with the caller in its context.
type Router struct {
	next httpx.Router
	svc  *Service
}

// Wrap returns a Router recording changes into svc.
func Wrap(next httpx.Router, svc *Service) Router { return Router{next: next, svc: svc} }

// HandleFunc implements httpx.Router.
func (a Router) HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request)) {
	method, _, _ := strings.Cut(pattern, " ")
	if method == http.MethodGet || method == http.MethodHead || slices.Contains(unaudited, pattern) {
		a.next.HandleFunc(pattern, h)
		return
	}
	a.next.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		ctx, note := auditnote.Open(r.Context())
		r = r.WithContext(ctx)
		t := a.svc.resolve(r, pattern)
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		h(sw, r)
		if sw.status < 300 {
			a.svc.record(r, pattern, sw.status, note, t)
		}
	})
}

// statusWriter remembers the status a handler answered.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
