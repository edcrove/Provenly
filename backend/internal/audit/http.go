package audit

import (
	"context"
	"net/http"
	"regexp"
	"time"

	"github.com/edcrove/provenly/backend/internal/platform/httpx"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
	"github.com/edcrove/provenly/backend/internal/platform/projectkey"
)

var (
	nonEmpty = regexp.MustCompile(`^(?s).+$`)
	// testCaseKey is a test case key, e.g. CHK-4.
	testCaseKey = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}-[1-9][0-9]{0,17}$`)
)

// API is the audit use case exposed over REST.
type API interface {
	Events(ctx context.Context, f Filter, page pagination.Page) (Page, error)
}

// Handler is the REST adapter of the audit log (session route, administrators).
type Handler struct{ api API }

// NewHandler builds a Handler.
func NewHandler(api API) *Handler { return &Handler{api: api} }

// Register mounts the audit route.
func (h *Handler) Register(mux httpx.Router) {
	mux.HandleFunc("GET /api/v1/audit", h.list)
}

// EventDTO is the wire form of Event.
type EventDTO struct {
	ID         int64     `json:"id"`
	OccurredAt time.Time `json:"occurredAt"`
	Actor      string    `json:"actor"`
	Action     string    `json:"action"`
	Path       string    `json:"path"`
	Project    *string   `json:"project"`
	Status     int32     `json:"status"`
	Summary    *string   `json:"summary"`
	TestCase   *string   `json:"testCase"`
	IP         *string   `json:"ip"`
	UserAgent  *string   `json:"userAgent"`
}

// optional is s, or nil when empty.
func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func eventDTO(e Event) EventDTO {
	return EventDTO{ID: e.ID, OccurredAt: e.OccurredAt, Actor: e.Actor, Action: e.Action, Path: e.Path, Status: e.Status,
		Project: optional(e.ProjectKey), Summary: optional(e.Summary), TestCase: optional(e.TestCaseKey),
		IP: optional(e.IP), UserAgent: optional(e.UserAgent)}
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f Filter
	project, err := projectkey.Query(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	actor, err := httpx.PatternQuery(r, "actor", nonEmpty, "must not be empty")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	tc, err := httpx.PatternQuery(r, "testCase", testCaseKey, "must be a test case key (e.g. CHK-4)")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if tc != nil {
		f.TestCaseKey = *tc
	}
	if project != nil {
		f.ProjectKey = *project
	}
	if actor != nil {
		f.Actor = *actor
	}
	res, err := h.api.Events(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewPage(res, eventDTO))
}
