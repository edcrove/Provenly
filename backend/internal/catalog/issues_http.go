package catalog

import (
	"net/http"
	"strconv"
	"time"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/httpx"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

// LinkVerificationDTO is the wire form of LinkVerification.
type LinkVerificationDTO struct {
	TestCaseID         int64   `json:"testCaseId"`
	TestCaseKey        *string `json:"testCaseKey"`
	Status             string  `json:"status"`
	Evidence           *string `json:"evidence"`
	EvidenceRunID      *int64  `json:"evidenceRunId"`
	LatestInconclusive bool    `json:"latestInconclusive"`
}

// VerificationDTO is the wire form of Verification.
type VerificationDTO struct {
	Status    string                `json:"status"`
	TestCases []LinkVerificationDTO `json:"testCases"`
}

// IssueDTO is the wire form of IssueView.
type IssueDTO struct {
	ID             int64           `json:"id"`
	Provider       string          `json:"provider"`
	ExternalID     string          `json:"externalId"`
	Title          string          `json:"title"`
	Description    string          `json:"description"`
	URL            string          `json:"url"`
	State          string          `json:"state"`
	ProviderStatus string          `json:"providerStatus"`
	ClosedAt       *time.Time      `json:"closedAt"`
	LastSyncedAt   *time.Time      `json:"lastSyncedAt"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	TestCaseIDs    []int64         `json:"testCaseIds"`
	Verification   VerificationDTO `json:"verification"`
}

func issueDTO(is IssueView) IssueDTO {
	links := make([]LinkVerificationDTO, len(is.Verification.Links))
	for i, l := range is.Verification.Links {
		links[i] = LinkVerificationDTO{TestCaseID: l.TestCaseID, TestCaseKey: keyOf(is.Keys, l.TestCaseID), Status: l.Status, EvidenceRunID: l.EvidenceRunID, LatestInconclusive: l.LatestInconclusive}
		if l.Evidence != "" {
			ev := l.Evidence
			links[i].Evidence = &ev
		}
	}
	return IssueDTO{
		ID: is.ID, Provider: is.Provider, ExternalID: is.ExternalID, Title: is.Title, Description: is.Description, URL: is.URL,
		State: is.State, ProviderStatus: is.ProviderStatus, ClosedAt: is.ClosedAt, LastSyncedAt: is.LastSyncedAt,
		CreatedAt: is.CreatedAt, UpdatedAt: is.UpdatedAt, TestCaseIDs: is.TestCaseIDs,
		Verification: VerificationDTO{Status: is.Verification.Status, TestCases: links},
	}
}

// issuePage is a page of issues with the verification of every issue that matches (not only this page's).
type issuePage struct {
	httpx.PageResponse[IssueDTO]
	VerificationCounts map[string]int `json:"verificationCounts"`
}

type issueRequest struct {
	Provider       string `json:"provider"`
	ExternalID     string `json:"externalId"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	URL            string `json:"url"`
	State          string `json:"state"`
	ProviderStatus string `json:"providerStatus"`
}

type issueImportItem struct {
	ExternalID     string `json:"externalId"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	URL            string `json:"url"`
	State          string `json:"state"`
	ProviderStatus string `json:"providerStatus"`
}

type issueImportRequest struct {
	Provider string            `json:"provider"`
	Items    []issueImportItem `json:"items"`
}

type updateIssueRequest struct {
	Title          *string `json:"title"`
	Description    *string `json:"description"`
	URL            *string `json:"url"`
	State          *string `json:"state"`
	ProviderStatus *string `json:"providerStatus"`
}

func (h *Handler) listIssues(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var f IssueFilter
	q := r.URL.Query()
	if q.Has("testCase") {
		id, err := strconv.ParseInt(q.Get("testCase"), 10, 64)
		if err != nil || id < 1 {
			httpx.WriteError(w, r, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: "testCase", Message: "must be a positive integer"}))
			return
		}
		f.TestCaseID = &id
	}
	if q.Has("state") {
		st := q.Get("state")
		f.State = &st
	}
	p, _, ok := h.project(w, r, authz.RoleViewer)
	if !ok {
		return
	}
	issues, err := h.api.Issues(r.Context(), p.ID, f)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	counts := map[string]int{}
	for _, is := range issues {
		counts[is.Verification.Status]++
	}
	httpx.WriteJSON(w, http.StatusOK, issuePage{httpx.NewPage(pagination.Slice(issues, page), issueDTO), counts})
}

func (h *Handler) createIssue(w http.ResponseWriter, r *http.Request) {
	var req issueRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, _, ok := h.project(w, r, authz.RoleMember)
	if !ok {
		return
	}
	created, err := h.api.CreateIssue(r.Context(), p.ID, IssueInput(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, issueDTO(created))
}

func (h *Handler) importIssues(w http.ResponseWriter, r *http.Request) {
	var req issueImportRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, _, ok := h.project(w, r, authz.RoleMaintainer)
	if !ok {
		return
	}
	items := make([]IssueInput, len(req.Items))
	for i, it := range req.Items {
		items[i] = IssueInput{ExternalID: it.ExternalID, Title: it.Title, Description: it.Description, URL: it.URL, State: it.State, ProviderStatus: it.ProviderStatus}
	}
	res, err := h.api.ImportIssues(r.Context(), p.ID, req.Provider, items)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, importResponse(res))
}

// issue resolves {projectKey} (with minRole) and {issueId}.
func (h *Handler) issue(w http.ResponseWriter, r *http.Request, minRole authz.Role) (Project, int64, bool) {
	id, err := httpx.PathID(r, "issueId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return Project{}, 0, false
	}
	p, _, ok := h.project(w, r, minRole)
	return p, id, ok
}

func (h *Handler) getIssue(w http.ResponseWriter, r *http.Request) {
	p, id, ok := h.issue(w, r, authz.RoleViewer)
	if !ok {
		return
	}
	is, err := h.api.Issue(r.Context(), p.ID, id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, issueDTO(is))
}

func (h *Handler) updateIssue(w http.ResponseWriter, r *http.Request) {
	var req updateIssueRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, id, ok := h.issue(w, r, authz.RoleMember)
	if !ok {
		return
	}
	updated, err := h.api.UpdateIssue(r.Context(), p.ID, id, UpdateIssueInput(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, issueDTO(updated))
}

func (h *Handler) setIssueTestCases(w http.ResponseWriter, r *http.Request) {
	var req linksRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if req.TestCaseIDs == nil {
		httpx.WriteError(w, r, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: "testCaseIds", Message: "is required (an empty list unlinks every test case)"}))
		return
	}
	p, id, ok := h.issue(w, r, authz.RoleMember)
	if !ok {
		return
	}
	updated, err := h.api.SetIssueTestCases(r.Context(), p.ID, id, req.TestCaseIDs)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, issueDTO(updated))
}
