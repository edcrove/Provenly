package catalog

import (
	"net/http"
	"time"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/httpx"
)

// SuiteQueryDTO is the wire form of SuiteQuery.
type SuiteQueryDTO struct {
	Tag            *string  `json:"tag"`
	Classification []string `json:"classification"`
}

// SuiteDTO is the wire form of Suite.
type SuiteDTO struct {
	Key         string         `json:"key"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Kind        SuiteKind      `json:"kind"`
	Query       *SuiteQueryDTO `json:"query"`
	ArchivedAt  *time.Time     `json:"archivedAt"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
	CaseCount   int32          `json:"caseCount"`
	// TestCaseIDs are the test cases a static suite lists (only when reading one suite).
	TestCaseIDs []int64 `json:"testCaseIds,omitempty"`
}

func suiteDTO(su Suite, withCases bool) SuiteDTO {
	dto := SuiteDTO{
		Key: su.Key, Name: su.Name, Description: su.Description, Kind: su.Kind, ArchivedAt: su.ArchivedAt,
		CreatedAt: su.CreatedAt, UpdatedAt: su.UpdatedAt, CaseCount: su.CaseCount,
	}
	if su.Kind == SuiteKindQuery {
		dto.Query = &SuiteQueryDTO{Tag: su.Query.Tag, Classification: su.Query.Classified}
		if dto.Query.Classification == nil {
			dto.Query.Classification = []string{}
		}
	}
	if withCases && su.Kind == SuiteKindStatic {
		dto.TestCaseIDs = su.CaseIDs
	}
	return dto
}

type suiteList struct {
	Items []SuiteDTO `json:"items"`
}

type createSuiteRequest struct {
	Key         string         `json:"key"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Kind        SuiteKind      `json:"kind"`
	Query       *SuiteQueryDTO `json:"query"`
	TestCaseIDs []int64        `json:"testCaseIds"`
}

type updateSuiteRequest struct {
	Name        *string        `json:"name"`
	Description *string        `json:"description"`
	Query       *SuiteQueryDTO `json:"query"`
	Archived    *bool          `json:"archived"`
}

type suiteCasesRequest struct {
	TestCaseIDs []int64 `json:"testCaseIds"`
}

func toQuery(q *SuiteQueryDTO) *SuiteQuery {
	if q == nil {
		return nil
	}
	return &SuiteQuery{Tag: q.Tag, Classified: q.Classification}
}

func (h *Handler) listSuites(w http.ResponseWriter, r *http.Request) {
	p, _, ok := h.project(w, r, authz.RoleViewer)
	if !ok {
		return
	}
	suites, err := h.api.Suites(r.Context(), p.ID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := suiteList{Items: make([]SuiteDTO, len(suites))}
	for i, su := range suites {
		out.Items[i] = suiteDTO(su, false)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) createSuite(w http.ResponseWriter, r *http.Request) {
	var req createSuiteRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, _, ok := h.project(w, r, authz.RoleMaintainer)
	if !ok {
		return
	}
	in := SuiteInput{Key: req.Key, Name: req.Name, Description: req.Description, Kind: req.Kind, TestCaseIDs: req.TestCaseIDs}
	if q := toQuery(req.Query); q != nil {
		in.Query = *q
	}
	su, err := h.api.CreateSuite(r.Context(), p.ID, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, suiteDTO(su, true))
}

func (h *Handler) getSuite(w http.ResponseWriter, r *http.Request) {
	key, ok := pathKey(w, r, "suiteKey", SuiteKeyPattern, SuiteKeyMessage)
	if !ok {
		return
	}
	p, _, ok := h.project(w, r, authz.RoleViewer)
	if !ok {
		return
	}
	su, err := h.api.Suite(r.Context(), p.ID, key)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, suiteDTO(su, true))
}

func (h *Handler) updateSuite(w http.ResponseWriter, r *http.Request) {
	key, ok := pathKey(w, r, "suiteKey", SuiteKeyPattern, SuiteKeyMessage)
	if !ok {
		return
	}
	var req updateSuiteRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, _, ok := h.project(w, r, authz.RoleMaintainer)
	if !ok {
		return
	}
	su, err := h.api.UpdateSuite(r.Context(), p.ID, key, UpdateSuiteInput{
		Name: req.Name, Description: req.Description, Query: toQuery(req.Query), Archived: req.Archived,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, suiteDTO(su, true))
}

func (h *Handler) setSuiteCases(w http.ResponseWriter, r *http.Request) {
	key, ok := pathKey(w, r, "suiteKey", SuiteKeyPattern, SuiteKeyMessage)
	if !ok {
		return
	}
	var req suiteCasesRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if req.TestCaseIDs == nil {
		httpx.WriteError(w, r, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: "testCaseIds", Message: "is required (an empty list empties the suite)"}))
		return
	}
	p, _, ok := h.project(w, r, authz.RoleMaintainer)
	if !ok {
		return
	}
	su, err := h.api.SetSuiteCases(r.Context(), p.ID, key, req.TestCaseIDs)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, suiteDTO(su, true))
}

// suiteQuery applies ?suite=<key> (which needs ?project=) to a test case list filter.
func (h *Handler) suiteQuery(r *http.Request, f ListFilter) (ListFilter, error) {
	key, err := httpx.PatternQuery(r, "suite", SuiteKeyPattern, SuiteKeyMessage)
	if err != nil || key == nil {
		return f, err
	}
	if r.URL.Query().Has("tag") || r.URL.Query().Has("classification") {
		return f, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: "suite", Message: "cannot be combined with tag or classification"})
	}
	if len(f.ProjectIDs) != 1 || !r.URL.Query().Has("project") {
		return f, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: "suite", Message: "needs ?project=<KEY>: suites belong to a project"})
	}
	su, err := h.api.Suite(r.Context(), f.ProjectIDs[0], *key)
	if err != nil {
		return f, err
	}
	return SuiteFilter(f, su), nil
}
