package catalog

import (
	"net/http"
	"strconv"
	"time"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/httpx"
)

// CoverageCaseDTO is the latest status of one covering test case (null: no result yet).
type CoverageCaseDTO struct {
	TestCaseID int64   `json:"testCaseId"`
	Status     *string `json:"status"`
}

// CoverageDTO is the wire form of Coverage.
type CoverageDTO struct {
	Status    string            `json:"status"`
	Linked    int32             `json:"linked"`
	Passed    int32             `json:"passed"`
	Failed    int32             `json:"failed"`
	NotRun    int32             `json:"notRun"`
	TestCases []CoverageCaseDTO `json:"testCases"`
}

// RequirementDTO is the wire form of RequirementView.
type RequirementDTO struct {
	ID             int64       `json:"id"`
	Provider       string      `json:"provider"`
	ExternalID     string      `json:"externalId"`
	Title          string      `json:"title"`
	Description    string      `json:"description"`
	URL            string      `json:"url"`
	ProviderStatus string      `json:"providerStatus"`
	ArchivedAt     *time.Time  `json:"archivedAt"`
	LastSyncedAt   *time.Time  `json:"lastSyncedAt"`
	CreatedAt      time.Time   `json:"createdAt"`
	UpdatedAt      time.Time   `json:"updatedAt"`
	TestCaseIDs    []int64     `json:"testCaseIds"`
	Coverage       CoverageDTO `json:"coverage"`
}

func requirementDTO(r RequirementView) RequirementDTO {
	cases := make([]CoverageCaseDTO, len(r.TestCaseIDs))
	for i, id := range r.TestCaseIDs {
		cases[i] = CoverageCaseDTO{TestCaseID: id}
		if st := r.Coverage.Latest[id]; st != "" {
			cases[i].Status = &st
		}
	}
	return RequirementDTO{
		ID: r.ID, Provider: r.Provider, ExternalID: r.ExternalID, Title: r.Title, Description: r.Description, URL: r.URL,
		ProviderStatus: r.ProviderStatus, ArchivedAt: r.ArchivedAt, LastSyncedAt: r.LastSyncedAt, CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt, TestCaseIDs: r.TestCaseIDs,
		Coverage: CoverageDTO{Status: r.Coverage.Status, Linked: r.Coverage.Linked, Passed: r.Coverage.Passed, Failed: r.Coverage.Failed,
			NotRun: r.Coverage.NotRun, TestCases: cases},
	}
}

type requirementList struct {
	Items []RequirementDTO `json:"items"`
}

type requirementRequest struct {
	Provider       string `json:"provider"`
	ExternalID     string `json:"externalId"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	URL            string `json:"url"`
	ProviderStatus string `json:"providerStatus"`
}

type importItem struct {
	ExternalID     string `json:"externalId"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	URL            string `json:"url"`
	ProviderStatus string `json:"providerStatus"`
}

type importRequest struct {
	Provider string       `json:"provider"`
	Items    []importItem `json:"items"`
}

type importResponse struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
}

type updateRequirementRequest struct {
	Title          *string `json:"title"`
	Description    *string `json:"description"`
	URL            *string `json:"url"`
	ProviderStatus *string `json:"providerStatus"`
	Archived       *bool   `json:"archived"`
}

type linksRequest struct {
	TestCaseIDs []int64 `json:"testCaseIds"`
}

func (h *Handler) listRequirements(w http.ResponseWriter, r *http.Request) {
	var testCase *int64
	if raw := r.URL.Query(); raw.Has("testCase") {
		id, err := strconv.ParseInt(raw.Get("testCase"), 10, 64)
		if err != nil || id < 1 {
			httpx.WriteError(w, r, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: "testCase", Message: "must be a positive integer"}))
			return
		}
		testCase = &id
	}
	p, _, ok := h.project(w, r, authz.RoleViewer)
	if !ok {
		return
	}
	reqs, err := h.api.Requirements(r.Context(), p.ID, testCase)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := requirementList{Items: make([]RequirementDTO, len(reqs))}
	for i, req := range reqs {
		out.Items[i] = requirementDTO(req)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) createRequirement(w http.ResponseWriter, r *http.Request) {
	var req requirementRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, _, ok := h.project(w, r, authz.RoleMember)
	if !ok {
		return
	}
	created, err := h.api.CreateRequirement(r.Context(), p.ID, RequirementInput(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, requirementDTO(created))
}

func (h *Handler) importRequirements(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, _, ok := h.project(w, r, authz.RoleMaintainer)
	if !ok {
		return
	}
	items := make([]RequirementInput, len(req.Items))
	for i, it := range req.Items {
		items[i] = RequirementInput{ExternalID: it.ExternalID, Title: it.Title, Description: it.Description, URL: it.URL, ProviderStatus: it.ProviderStatus}
	}
	res, err := h.api.ImportRequirements(r.Context(), p.ID, req.Provider, items)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, importResponse(res))
}

// requirement resolves {projectKey} (with minRole) and {requirementId}.
func (h *Handler) requirement(w http.ResponseWriter, r *http.Request, minRole authz.Role) (Project, int64, bool) {
	id, err := httpx.PathID(r, "requirementId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return Project{}, 0, false
	}
	p, _, ok := h.project(w, r, minRole)
	return p, id, ok
}

func (h *Handler) getRequirement(w http.ResponseWriter, r *http.Request) {
	p, id, ok := h.requirement(w, r, authz.RoleViewer)
	if !ok {
		return
	}
	req, err := h.api.Requirement(r.Context(), p.ID, id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, requirementDTO(req))
}

func (h *Handler) updateRequirement(w http.ResponseWriter, r *http.Request) {
	var req updateRequirementRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, id, ok := h.requirement(w, r, authz.RoleMember)
	if !ok {
		return
	}
	updated, err := h.api.UpdateRequirement(r.Context(), p.ID, id, UpdateRequirementInput(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, requirementDTO(updated))
}

func (h *Handler) setRequirementTestCases(w http.ResponseWriter, r *http.Request) {
	var req linksRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if req.TestCaseIDs == nil {
		httpx.WriteError(w, r, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: "testCaseIds", Message: "is required (an empty list unlinks every test case)"}))
		return
	}
	p, id, ok := h.requirement(w, r, authz.RoleMember)
	if !ok {
		return
	}
	updated, err := h.api.SetRequirementTestCases(r.Context(), p.ID, id, req.TestCaseIDs)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, requirementDTO(updated))
}
