package catalog

import (
	"cmp"
	"context"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/etag"
	"github.com/edcrove/provenly/backend/internal/platform/httpx"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
	"github.com/edcrove/provenly/backend/internal/platform/projectkey"
)

// API is the set of catalog use cases exposed over REST.
type API interface {
	Create(ctx context.Context, in CreateInput) (TestCase, error)
	Get(ctx context.Context, id int64) (TestCase, error)
	List(ctx context.Context, f ListFilter, page pagination.Page) (pagination.Result[TestCase], error)
	Update(ctx context.Context, id int64, in UpdateInput, m etag.Match) (TestCase, error)
	Deprecate(ctx context.Context, id int64, m etag.Match) (TestCase, error)
	Reactivate(ctx context.Context, id int64, m etag.Match) (TestCase, error)
	ListSteps(ctx context.Context, testCaseID int64, page pagination.Page) (pagination.Result[TestStep], error)
	CreateStep(ctx context.Context, testCaseID int64, in CreateStepInput, m etag.Match) (TestStep, int64, error)
	UpdateStep(ctx context.Context, testCaseID, stepID int64, in UpdateStepInput, m etag.Match) (TestStep, int64, error)
	DeleteStep(ctx context.Context, testCaseID, stepID int64, m etag.Match) (int64, error)
	ReorderSteps(ctx context.Context, testCaseID int64, stepIDs []int64, m etag.Match) ([]TestStep, int64, error)
	CreateProject(ctx context.Context, in CreateProjectInput) (Project, error)
	ProjectByKey(ctx context.Context, key string) (Project, error)
	ListProjects(ctx context.Context, projectIDs []int64, page pagination.Page) (pagination.Result[Project], error)
	UpdateProject(ctx context.Context, key string, in UpdateProjectInput) (Project, error)
	Dimensions(ctx context.Context, projectID int64) ([]Dimension, error)
	CreateDimension(ctx context.Context, projectID int64, in DimensionInput) (Dimension, error)
	UpdateDimension(ctx context.Context, projectID int64, key string, in UpdateDimensionInput) (Dimension, error)
	CreateDimensionValue(ctx context.Context, projectID int64, dimensionKey string, in DimensionInput) (Dimension, error)
	UpdateDimensionValue(ctx context.Context, projectID int64, dimensionKey, valueKey string, in UpdateDimensionInput) (Dimension, error)
	Suites(ctx context.Context, projectID int64) ([]Suite, error)
	Suite(ctx context.Context, projectID int64, key string) (Suite, error)
	CreateSuite(ctx context.Context, projectID int64, in SuiteInput) (Suite, error)
	UpdateSuite(ctx context.Context, projectID int64, key string, in UpdateSuiteInput) (Suite, error)
	SetSuiteCases(ctx context.Context, projectID int64, key string, ids []int64) (Suite, error)
	Requirements(ctx context.Context, projectID int64, testCaseID *int64) ([]RequirementView, error)
	Requirement(ctx context.Context, projectID, id int64) (RequirementView, error)
	CreateRequirement(ctx context.Context, projectID int64, in RequirementInput) (RequirementView, error)
	ImportRequirements(ctx context.Context, projectID int64, provider string, items []RequirementInput) (ImportResult, error)
	UpdateRequirement(ctx context.Context, projectID, id int64, in UpdateRequirementInput) (RequirementView, error)
	SetRequirementTestCases(ctx context.Context, projectID, id int64, ids []int64) (RequirementView, error)
	Issues(ctx context.Context, projectID int64, f IssueFilter) ([]IssueView, error)
	Issue(ctx context.Context, projectID, id int64) (IssueView, error)
	CreateIssue(ctx context.Context, projectID int64, in IssueInput) (IssueView, error)
	ImportIssues(ctx context.Context, projectID int64, provider string, items []IssueInput) (ImportResult, error)
	UpdateIssue(ctx context.Context, projectID, id int64, in UpdateIssueInput) (IssueView, error)
	SetIssueTestCases(ctx context.Context, projectID, id int64, ids []int64) (IssueView, error)
}

// ProjectKeyMessage is the validation message of a malformed project key.
const ProjectKeyMessage = projectkey.Message

// ProjectDTO is the wire form of Project.
type ProjectDTO struct {
	ID          int64     `json:"id"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	// MyRole is what the signed-in user may do in the project (admin, maintainer, member or viewer).
	MyRole string `json:"myRole"`
}

// ProjectToDTO converts a Project to its wire form, with the caller's role in it.
func ProjectToDTO(p Project, role authz.Role) ProjectDTO {
	return ProjectDTO{
		ID: p.ID, Key: p.Key, Name: p.Name, Description: p.Description, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
		MyRole: role.String(),
	}
}

type createProjectRequest struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type updateProjectRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// ProjectID is the id of a project (for projectkey.Resolve).
func ProjectID(p Project) int64 { return p.ID }

// ProjectQuery resolves the optional ?project=<KEY> filter to a project id.
func ProjectQuery(r *http.Request, byKey func(context.Context, string) (Project, error)) (*int64, error) {
	key, err := projectkey.Query(r)
	if err != nil || key == nil {
		return nil, err
	}
	p, err := byKey(r.Context(), *key)
	if err != nil {
		return nil, err
	}
	return &p.ID, nil
}

// TestCaseDTO is the wire form of TestCase.
type TestCaseDTO struct {
	ID             int64             `json:"id"`
	Key            string            `json:"key"`
	ProjectID      int64             `json:"projectId"`
	ProjectKey     string            `json:"projectKey"`
	Number         int64             `json:"number"`
	Title          string            `json:"title"`
	Description    string            `json:"description"`
	ExpectedResult string            `json:"expectedResult"`
	Status         Status            `json:"status"`
	Automated      bool              `json:"automated"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
	DeprecatedAt   *time.Time        `json:"deprecatedAt"`
	Version        int64             `json:"version"`
	Tags           []string          `json:"tags"`
	Classification map[string]string `json:"classification"`
}

// ToDTO converts a TestCase to its wire form.
func ToDTO(tc TestCase) TestCaseDTO {
	dto := TestCaseDTO{
		ID: tc.ID, Key: tc.Key(), ProjectID: tc.ProjectID, ProjectKey: tc.ProjectKey, Number: tc.Number, Title: tc.Title, Description: tc.Description,
		ExpectedResult: tc.ExpectedResult, Status: tc.Status, Automated: tc.Automated,
		CreatedAt: tc.CreatedAt, UpdatedAt: tc.UpdatedAt, DeprecatedAt: tc.DeprecatedAt, Version: tc.Version,
		Tags: tc.Tags, Classification: tc.Classification,
	}
	if dto.Tags == nil {
		dto.Tags = []string{}
	}
	if dto.Classification == nil {
		dto.Classification = map[string]string{}
	}
	return dto
}

// writeVersioned answers a test case read or write with its version as the ETag (optimistic locking).
func writeVersioned(w http.ResponseWriter, status int, version int64, body any) {
	w.Header().Set("ETag", etag.Tag(version))
	httpx.WriteJSON(w, status, body)
}

// ifMatch parses the request's If-Match (400 when malformed).
func ifMatch(w http.ResponseWriter, r *http.Request) (etag.Match, bool) {
	m, err := etag.FromRequest(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return m, false
	}
	return m, true
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
	Project        string            `json:"project"`
	Title          string            `json:"title"`
	Description    string            `json:"description"`
	ExpectedResult string            `json:"expectedResult"`
	Automated      bool              `json:"automated"`
	Tags           []string          `json:"tags"`
	Classification map[string]string `json:"classification"`
}

type updateTestCaseRequest struct {
	Title          *string            `json:"title"`
	Description    *string            `json:"description"`
	ExpectedResult *string            `json:"expectedResult"`
	Automated      *bool              `json:"automated"`
	Tags           *[]string          `json:"tags"`
	Classification map[string]*string `json:"classification"`
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
	api   API
	guard authz.Guard
}

// NewHandler builds a Handler.
func NewHandler(api API, guard authz.Guard) *Handler { return &Handler{api: api, guard: guard} }

// onTestCase authorizes routes on /test-cases/{testCaseId}: the test case's project must give the user at
// least minRole; a test case of a project the user cannot see is "not found". A malformed id is left to next.
func (h *Handler) onTestCase(minRole authz.Role, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if id, err := httpx.PathID(r, "testCaseId"); err == nil {
			tc, err := h.api.Get(r.Context(), id)
			if err == nil {
				err = h.guard.Require(r.Context(), tc.ProjectID, minRole, notFound(id))
			}
			if err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), testCaseKey{}, tc))
		}
		next(w, r)
	}
}

type testCaseKey struct{}

// VisibleProjects narrows a list to ?project=<KEY> (which the user must see) or to every project the user can see
// (nil: every project, for administrators).
func VisibleProjects(r *http.Request, byKey func(context.Context, string) (Project, error), guard authz.Guard) ([]int64, error) {
	id, err := ProjectQuery(r, byKey)
	if err != nil {
		return nil, err
	}
	if id != nil {
		if err := guard.Require(r.Context(), *id, authz.RoleViewer, projectNotFound(r.URL.Query().Get("project"))); err != nil {
			return nil, err
		}
		return []int64{*id}, nil
	}
	scope, err := guard.Scope(r.Context())
	return scope.ProjectIDs(), err
}

// project resolves {projectKey} and checks the user's role in it; the role is returned for the DTO.
func (h *Handler) project(w http.ResponseWriter, r *http.Request, minRole authz.Role) (Project, authz.Role, bool) {
	key, ok := h.projectKey(w, r)
	if !ok {
		return Project{}, authz.RoleNone, false
	}
	p, err := projectkey.Resolve(r.Context(), "projectKey", key, h.api.ProjectByKey, ProjectID, h.guard, minRole)
	var scope authz.Scope
	if err == nil {
		scope, err = h.guard.Scope(r.Context())
	}
	if err != nil {
		httpx.WriteError(w, r, err)
		return Project{}, authz.RoleNone, false
	}
	return p, scope.RoleIn(p.ID), true
}

// Register mounts the catalog routes.
func (h *Handler) Register(mux httpx.Router) {
	mux.HandleFunc("GET /api/v1/projects", h.listProjects)
	mux.HandleFunc("POST /api/v1/projects", h.createProject)
	mux.HandleFunc("GET /api/v1/projects/{projectKey}", h.getProject)
	mux.HandleFunc("PATCH /api/v1/projects/{projectKey}", h.updateProject)
	mux.HandleFunc("GET /api/v1/projects/{projectKey}/dimensions", h.listDimensions)
	mux.HandleFunc("POST /api/v1/projects/{projectKey}/dimensions", h.createDimension)
	mux.HandleFunc("PATCH /api/v1/projects/{projectKey}/dimensions/{dimensionKey}", h.updateDimension)
	mux.HandleFunc("POST /api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values", h.createDimensionValue)
	mux.HandleFunc("PATCH /api/v1/projects/{projectKey}/dimensions/{dimensionKey}/values/{valueKey}", h.updateDimensionValue)
	mux.HandleFunc("GET /api/v1/projects/{projectKey}/suites", h.listSuites)
	mux.HandleFunc("POST /api/v1/projects/{projectKey}/suites", h.createSuite)
	mux.HandleFunc("GET /api/v1/projects/{projectKey}/suites/{suiteKey}", h.getSuite)
	mux.HandleFunc("PATCH /api/v1/projects/{projectKey}/suites/{suiteKey}", h.updateSuite)
	mux.HandleFunc("PUT /api/v1/projects/{projectKey}/suites/{suiteKey}/cases", h.setSuiteCases)
	mux.HandleFunc("GET /api/v1/projects/{projectKey}/requirements", h.listRequirements)
	mux.HandleFunc("POST /api/v1/projects/{projectKey}/requirements", h.createRequirement)
	mux.HandleFunc("POST /api/v1/projects/{projectKey}/requirements/import", h.importRequirements)
	mux.HandleFunc("GET /api/v1/projects/{projectKey}/requirements/{requirementId}", h.getRequirement)
	mux.HandleFunc("PATCH /api/v1/projects/{projectKey}/requirements/{requirementId}", h.updateRequirement)
	mux.HandleFunc("PUT /api/v1/projects/{projectKey}/requirements/{requirementId}/test-cases", h.setRequirementTestCases)
	mux.HandleFunc("GET /api/v1/projects/{projectKey}/issues", h.listIssues)
	mux.HandleFunc("POST /api/v1/projects/{projectKey}/issues", h.createIssue)
	mux.HandleFunc("POST /api/v1/projects/{projectKey}/issues/import", h.importIssues)
	mux.HandleFunc("GET /api/v1/projects/{projectKey}/issues/{issueId}", h.getIssue)
	mux.HandleFunc("PATCH /api/v1/projects/{projectKey}/issues/{issueId}", h.updateIssue)
	mux.HandleFunc("PUT /api/v1/projects/{projectKey}/issues/{issueId}/test-cases", h.setIssueTestCases)
	mux.HandleFunc("GET /api/v1/test-cases", h.list)
	mux.HandleFunc("POST /api/v1/test-cases", h.create)
	mux.HandleFunc("GET /api/v1/test-cases/{testCaseId}", h.onTestCase(authz.RoleViewer, h.get))
	mux.HandleFunc("PATCH /api/v1/test-cases/{testCaseId}", h.onTestCase(authz.RoleMember, h.update))
	mux.HandleFunc("POST /api/v1/test-cases/{testCaseId}/deprecate", h.onTestCase(authz.RoleMaintainer, h.deprecate))
	mux.HandleFunc("POST /api/v1/test-cases/{testCaseId}/reactivate", h.onTestCase(authz.RoleMaintainer, h.reactivate))
	mux.HandleFunc("GET /api/v1/test-cases/{testCaseId}/steps", h.onTestCase(authz.RoleViewer, h.listSteps))
	mux.HandleFunc("POST /api/v1/test-cases/{testCaseId}/steps", h.onTestCase(authz.RoleMember, h.createStep))
	mux.HandleFunc("PUT /api/v1/test-cases/{testCaseId}/steps/order", h.onTestCase(authz.RoleMember, h.reorderSteps))
	mux.HandleFunc("PATCH /api/v1/test-cases/{testCaseId}/steps/{stepId}", h.onTestCase(authz.RoleMember, h.updateStep))
	mux.HandleFunc("DELETE /api/v1/test-cases/{testCaseId}/steps/{stepId}", h.onTestCase(authz.RoleMember, h.deleteStep))
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
	var f ListFilter
	if raw != nil {
		s := Status(*raw)
		f.Status = &s
	}
	if f.Tag, err = httpx.PatternQuery(r, "tag", TagPattern, TagMessage); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	// ?automated=true|false: automated or manual test cases only (the execution mode is the automated flag, P9-2).
	automated, err := httpx.EnumQuery(r, "automated", "true", "false")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if automated != nil {
		v := *automated == "true"
		f.Automated = &v
	}
	if f.Classified, err = classifiedQuery(r); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if f.ProjectIDs, err = VisibleProjects(r, h.api.ProjectByKey, h.guard); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if f, err = h.suiteQuery(r, f); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if f, err = searchQuery(r, f); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var none bool
	if f, none, err = h.keyQuery(r, f); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if none {
		httpx.WriteJSON(w, http.StatusOK, httpx.NewPage(pagination.Result[TestCase]{Items: []TestCase{}, Page: page}, ToDTO))
		return
	}
	res, err := h.api.List(r.Context(), f, page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewPage(res, ToDTO))
}

// searchNumber reads a key or number typed in a picker: CHK-12, chk-12 or 12.
var searchNumber = regexp.MustCompile(`^(?:[A-Za-z][A-Za-z0-9]{1,9}-)?([1-9][0-9]{0,17})$`)

// likeEscaper escapes LIKE's wildcards, so ?q=50% looks for "50%".
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// searchQuery narrows the list to ?q=: a title containing the text (any case), or the test case whose number (or key)
// it is (DEC-78: the pickers search on the server).
func searchQuery(r *http.Request, f ListFilter) (ListFilter, error) {
	q := r.URL.Query()
	if !q.Has("q") {
		return f, nil
	}
	raw := strings.TrimSpace(q.Get("q"))
	var v apperr.Validator
	v.Check(raw != "", "q", "must not be empty")
	v.Check(utf8.RuneCountInString(raw) <= 200, "q", "must be at most 200 characters")
	v.CheckText("q", raw)
	if err := v.Err(); err != nil {
		return f, err
	}
	pattern := likeEscaper.Replace(raw)
	f.Search = &pattern
	if m := searchNumber.FindStringSubmatch(raw); m != nil {
		n, _ := strconv.ParseInt(m[1], 10, 64) // at most 18 digits: always an int64
		f.SearchNumber = &n
	}
	return f, nil
}

// testCaseKeyPattern is a test case key: <PROJECT>-<number>.
var testCaseKeyPattern = regexp.MustCompile(`^([A-Z][A-Z0-9]{1,9})-([1-9][0-9]{0,17})$`)

// keyQuery narrows the list to ?key=<PROJECT>-<number> (400 when malformed). none tells that no test case can match:
// the key's project does not exist, the caller cannot see it, or ?project= names another one.
func (h *Handler) keyQuery(r *http.Request, f ListFilter) (ListFilter, bool, error) {
	raw, err := httpx.PatternQuery(r, "key", testCaseKeyPattern, "must be a test case key: <PROJECT>-<number> (e.g. CHK-12)")
	if err != nil || raw == nil {
		return f, false, err
	}
	m := testCaseKeyPattern.FindStringSubmatch(*raw)
	n, _ := strconv.ParseInt(m[2], 10, 64) // at most 18 digits: always an int64
	p, err := h.api.ProjectByKey(r.Context(), m[1])
	if e, ok := apperr.As(err); ok && e.Kind == apperr.KindNotFound {
		return f, true, nil
	}
	if err != nil {
		return f, false, err
	}
	if f.ProjectIDs != nil && !slices.Contains(f.ProjectIDs, p.ID) {
		return f, true, nil
	}
	f.ProjectIDs, f.Number = []int64{p.ID}, &n
	return f, false, nil
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createTestCaseRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, err := projectkey.Resolve(r.Context(), "project", cmp.Or(req.Project, DefaultProjectKey), h.api.ProjectByKey, ProjectID, h.guard, authz.RoleMember)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	tc, err := h.api.Create(r.Context(), CreateInput{
		ProjectID: p.ID, Title: req.Title, Description: req.Description, ExpectedResult: req.ExpectedResult, Automated: req.Automated,
		Tags: req.Tags, Classification: req.Classification,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, ToDTO(tc))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	if _, err := httpx.PathID(r, "testCaseId"); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	// Loaded and authorized by onTestCase.
	tc := r.Context().Value(testCaseKey{}).(TestCase)
	writeVersioned(w, http.StatusOK, tc.Version, ToDTO(tc))
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
	m, ok := ifMatch(w, r)
	if !ok {
		return
	}
	tc, err := h.api.Update(r.Context(), id, UpdateInput(req), m)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	writeVersioned(w, http.StatusOK, tc.Version, ToDTO(tc))
}

func (h *Handler) deprecate(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "testCaseId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	m, ok := ifMatch(w, r)
	if !ok {
		return
	}
	tc, err := h.api.Deprecate(r.Context(), id, m)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	writeVersioned(w, http.StatusOK, tc.Version, ToDTO(tc))
}

func (h *Handler) reactivate(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "testCaseId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	m, ok := ifMatch(w, r)
	if !ok {
		return
	}
	tc, err := h.api.Reactivate(r.Context(), id, m)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	writeVersioned(w, http.StatusOK, tc.Version, ToDTO(tc))
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
	// The version read before the steps: a change in between makes the next write fail safe (412).
	tc := r.Context().Value(testCaseKey{}).(TestCase)
	writeVersioned(w, http.StatusOK, tc.Version, httpx.NewPage(res, stepDTO))
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
	m, ok := ifMatch(w, r)
	if !ok {
		return
	}
	step, version, err := h.api.CreateStep(r.Context(), id, CreateStepInput(req), m)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	writeVersioned(w, http.StatusCreated, version, stepDTO(step))
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
	m, ok := ifMatch(w, r)
	if !ok {
		return
	}
	steps, version, err := h.api.ReorderSteps(r.Context(), id, req.StepIDs, m)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := stepList{Items: make([]TestStepDTO, len(steps))}
	for i, s := range steps {
		out.Items[i] = stepDTO(s)
	}
	writeVersioned(w, http.StatusOK, version, out)
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
	m, ok := ifMatch(w, r)
	if !ok {
		return
	}
	step, version, err := h.api.UpdateStep(r.Context(), id, stepID, UpdateStepInput(req), m)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	writeVersioned(w, http.StatusOK, version, stepDTO(step))
}

func (h *Handler) deleteStep(w http.ResponseWriter, r *http.Request) {
	id, stepID, ok := h.stepIDs(w, r)
	if !ok {
		return
	}
	m, ok := ifMatch(w, r)
	if !ok {
		return
	}
	version, err := h.api.DeleteStep(r.Context(), id, stepID, m)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.Header().Set("ETag", etag.Tag(version))
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) projectKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key, err := projectkey.Path(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return "", false
	}
	return key, true
}

func (h *Handler) listProjects(w http.ResponseWriter, r *http.Request) {
	page, err := httpx.ParsePage(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	scope, err := h.guard.Scope(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	res, err := h.api.ListProjects(r.Context(), scope.ProjectIDs(), page)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.NewPage(res, func(p Project) ProjectDTO { return ProjectToDTO(p, scope.RoleIn(p.ID)) }))
}

func (h *Handler) createProject(w http.ResponseWriter, r *http.Request) {
	if err := h.guard.RequireAdmin(r.Context()); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var req createProjectRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, err := h.api.CreateProject(r.Context(), CreateProjectInput(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, ProjectToDTO(p, authz.RoleAdmin))
}

func (h *Handler) getProject(w http.ResponseWriter, r *http.Request) {
	p, role, ok := h.project(w, r, authz.RoleViewer)
	if !ok {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ProjectToDTO(p, role))
}

func (h *Handler) updateProject(w http.ResponseWriter, r *http.Request) {
	var req updateProjectRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, role, ok := h.project(w, r, authz.RoleMaintainer)
	if !ok {
		return
	}
	p, err := h.api.UpdateProject(r.Context(), p.Key, UpdateProjectInput(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ProjectToDTO(p, role))
}
