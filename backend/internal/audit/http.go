package audit

import (
	"context"
	"net/http"
	"regexp"
	"time"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/platform/httpx"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

var nonEmpty = regexp.MustCompile(`^(?s).+$`)

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
}

func eventDTO(e Event) EventDTO {
	dto := EventDTO{ID: e.ID, OccurredAt: e.OccurredAt, Actor: e.Actor, Action: e.Action, Path: e.Path, Status: e.Status}
	if e.ProjectKey != "" {
		p := e.ProjectKey
		dto.Project = &p
	}
	return dto
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f Filter
	project, err := httpx.PatternQuery(r, "project", catalog.ProjectKeyPattern, catalog.ProjectKeyMessage)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	actor, err := httpx.PatternQuery(r, "actor", nonEmpty, "must not be empty")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
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
