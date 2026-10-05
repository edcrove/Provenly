// Package audit keeps the audit log (Incubator, Project & Authorization): every authenticated change made through the
// API is recorded with who made it, which operation, the request path and the project it addressed, never the request
// body. The log is append-only and read by administrators.
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
}

// Filter narrows the log; empty fields match everything.
type Filter struct {
	ProjectKey string
	Actor      string
}

// Repository is the persistence port of the audit module.
type Repository interface {
	Insert(ctx context.Context, e Event) error
	List(ctx context.Context, f Filter, limit, offset int32) ([]Event, error)
	Count(ctx context.Context, f Filter) (int64, error)
}

// Access authorizes reading the log (administrators).
type Access interface {
	RequireAdmin(ctx context.Context) error
}

// Service records and lists audit events.
type Service struct {
	repo   Repository
	access Access
}

// NewService builds a Service.
func NewService(repo Repository, access Access) *Service { return &Service{repo: repo, access: access} }

// Page is one page of events.
type Page = pagination.Result[Event]

// Events lists the log, newest first (administrators).
func (s *Service) Events(ctx context.Context, f Filter, page pagination.Page) (Page, error) {
	var v apperr.Validator
	v.Check(utf8.RuneCountInString(f.Actor) <= 200, "actor", "must be at most 200 characters")
	v.CheckText("actor", f.Actor)
	v.Check(utf8.RuneCountInString(f.ProjectKey) <= 50, "project", "must be at most 50 characters")
	v.CheckText("project", f.ProjectKey)
	if err := v.Err(); err != nil {
		return Page{}, err
	}
	if err := s.access.RequireAdmin(ctx); err != nil {
		return Page{}, err
	}
	items, err := s.repo.List(ctx, f, page.Limit(), page.Offset())
	if err != nil {
		return Page{}, err
	}
	total, err := s.repo.Count(ctx, f)
	if err != nil {
		return Page{}, err
	}
	return Page{Items: items, Page: page, Total: total}, nil
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

// record stores the event of a successful change; a failure is logged, never returned (the change happened).
func (s *Service) record(r *http.Request, action string, status int) {
	project := r.PathValue("projectKey")
	if project == "" {
		project = r.URL.Query().Get("project")
	}
	e := Event{Actor: actor(r.Context()), Action: action, Path: truncate(storable(r.URL.Path), 2000), ProjectKey: truncate(storable(project), 50), Status: int32(status)}
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
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		h(sw, r)
		if sw.status < 300 {
			a.svc.record(r, pattern, sw.status)
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
