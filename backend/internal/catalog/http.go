package catalog

import (
	"context"
	"net/http"
	"time"

	"github.com/edcrove/provenly/backend/internal/platform/httpx"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

// API is the set of catalog use cases exposed over REST.
type API interface {
	Create(ctx context.Context, in CreateInput) (TestCase, error)
	Get(ctx context.Context, id int64) (TestCase, error)
	List(ctx context.Context, status *Status, page pagination.Page) (pagination.Result[TestCase], error)
	Update(ctx context.Context, id int64, in UpdateInput) (TestCase, error)
	Deprecate(ctx context.Context, id int64) (TestCase, error)
	Reactivate(ctx context.Context, id int64) (TestCase, error)
	ListSteps(ctx context.Context, testCaseID int64, page pagination.Page) (pagination.Result[TestStep], error)
	CreateStep(ctx context.Context, testCaseID int64, in CreateStepInput) (TestStep, error)
	UpdateStep(ctx context.Context, testCaseID, stepID int64, in UpdateStepInput) (TestStep, error)
	DeleteStep(ctx context.Context, testCaseID, stepID int64) error
	ReorderSteps(ctx context.Context, testCaseID int64, stepIDs []int64) ([]TestStep, error)
}

// TestCaseDTO is the wire form of TestCase.
type TestCaseDTO struct {
	ID             int64      `json:"id"`
	Key            string     `json:"key"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	ExpectedResult string     `json:"expectedResult"`
	Status         Status     `json:"status"`
	Automated      bool       `json:"automated"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	DeprecatedAt   *time.Time `json:"deprecatedAt"`
}

// ToDTO converts a TestCase to its wire form.
func ToDTO(tc TestCase) TestCaseDTO {
	return TestCaseDTO{
		ID: tc.ID, Key: tc.Key(), Title: tc.Title, Description: tc.Description,
		ExpectedResult: tc.ExpectedResult, Status: tc.Status, Automated: tc.Automated,
		CreatedAt: tc.CreatedAt, UpdatedAt: tc.UpdatedAt, DeprecatedAt: tc.DeprecatedAt,
	}
}

// TestStepDTO is the wire form of TestStep.
type TestStepDTO struct {
	ID             int64     `json:"id"`
	TestCaseID     int64     `json:"testCaseId"`
	Position       int32     `json:"position"`
	Action         string    `json:"action"`
	ExpectedResult string    `json:"expectedResult"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

func stepDTO(s TestStep) TestStepDTO { return TestStepDTO(s) }

type createTestCaseRequest struct {
	Title          string `json:"title"`
	Description    string `json:"description"`
	ExpectedResult string `json:"expectedResult"`
	Automated      bool   `json:"automated"`
}

type updateTestCaseRequest struct {
	Title          *string `json:"title"`
	Description    *string `json:"description"`
	ExpectedResult *string `json:"expectedResult"`
	Automated      *bool   `json:"automated"`
}

type createStepRequest struct {
	Action         string `json:"action"`
	ExpectedResult string `json:"expectedResult"`
	Position       *int32 `json:"position"`
}

type updateStepRequest struct {
	Action         *string `json:"action"`
	ExpectedResult *string `json:"expectedResult"`
}

type reorderRequest struct {
	StepIDs []int64 `json:"stepIds"`
}

type stepList struct {
	Items []TestStepDTO `json:"items"`
}

// Handler is the REST adapter of the catalog module.
type Handler struct {
	api API
}

// NewHandler builds a Handler.
func NewHandler(api API) *Handler { return &Handler{api: api} }

// Register mounts the catalog routes.
func (h *Handler) Register(mux httpx.Router) {
	mux.HandleFunc("GET /api/v1/test-cases", h.list)
	mux.HandleFunc("POST /api/v1/test-cases", h.create)
	mux.HandleFunc("GET /api/v1/test-cases/{testCaseId}", h.get)
	mux.HandleFunc("PATCH /api/v1/test-cases/{testCaseId}", h.update)
	mux.HandleFunc("POST /api/v1/test-cases/{testCaseId}/deprecate", h.deprecate)
	mux.HandleFunc("POST /api/v1/test-cases/{testCaseId}/reactivate", h.reactivate)
	mux.HandleFunc("GET /api/v1/test-cases/{testCaseId}/steps", h.listSteps)
	mux.HandleFunc("POST /api/v1/test-cases/{testCaseId}/steps", h.createStep)
	mux.HandleFunc("PUT /api/v1/test-cases/{testCaseId}/steps/order", h.reorderSteps)
	mux.HandleFunc("PATCH /api/v1/test-cases/{testCaseId}/steps/{stepId}", h.updateStep)
	mux.HandleFunc("DELETE /api/v1/test-cases/{testCaseId}/steps/{stepId}", h.deleteStep)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	raw, err := httpx.EnumQuery(r, "status", string(StatusActive), string(StatusDeprecated))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var status *Status
	if raw != nil {
		s := Status(*raw)
		status = &s
	}
	res, err := h.api.List(r.Context(), status, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewPage(res, ToDTO))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createTestCaseRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	tc, err := h.api.Create(r.Context(), CreateInput(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, ToDTO(tc))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "testCaseId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	tc, err := h.api.Get(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ToDTO(tc))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "testCaseId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var req updateTestCaseRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	tc, err := h.api.Update(r.Context(), id, UpdateInput(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ToDTO(tc))
}

func (h *Handler) deprecate(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "testCaseId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	tc, err := h.api.Deprecate(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ToDTO(tc))
}

func (h *Handler) reactivate(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "testCaseId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	tc, err := h.api.Reactivate(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ToDTO(tc))
}

func (h *Handler) listSteps(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "testCaseId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	res, err := h.api.ListSteps(r.Context(), id, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewPage(res, stepDTO))
}

func (h *Handler) createStep(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "testCaseId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var req createStepRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	step, err := h.api.CreateStep(r.Context(), id, CreateStepInput(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, stepDTO(step))
}

func (h *Handler) reorderSteps(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "testCaseId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var req reorderRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	steps, err := h.api.ReorderSteps(r.Context(), id, req.StepIDs)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := stepList{Items: make([]TestStepDTO, len(steps))}
	for i, s := range steps {
		out.Items[i] = stepDTO(s)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) stepIDs(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	id, err := httpx.PathID(r, "testCaseId")
	if err == nil {
		var stepID int64
		stepID, err = httpx.PathID(r, "stepId")
		if err == nil {
			return id, stepID, true
		}
	}
	httpx.WriteError(w, r, err)
	return 0, 0, false
}

func (h *Handler) updateStep(w http.ResponseWriter, r *http.Request) {
	id, stepID, ok := h.stepIDs(w, r)
	if !ok {
		return
	}
	var req updateStepRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	step, err := h.api.UpdateStep(r.Context(), id, stepID, UpdateStepInput(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, stepDTO(step))
}

func (h *Handler) deleteStep(w http.ResponseWriter, r *http.Request) {
	id, stepID, ok := h.stepIDs(w, r)
	if !ok {
		return
	}
	if err := h.api.DeleteStep(r.Context(), id, stepID); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
