package catalog

import (
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/httpx"
)

// MaxClassifiedFilters bounds the dimension:value pairs of ?classification=.
const MaxClassifiedFilters = 10

const classifiedPair = `[a-z][a-z0-9-]{0,29}:[a-z0-9][a-z0-9-]{0,29}`

var classifiedPattern = regexp.MustCompile(`^` + classifiedPair + `(,` + classifiedPair + `){0,9}$`)

const classifiedMessage = "must be up to 10 comma-separated dimension:value pairs (e.g. risk:critical,feature:payments)"

// classifiedQuery reads ?classification=dim:value,... (all must hold), without duplicates.
func classifiedQuery(r *http.Request) ([]string, error) {
	raw, err := httpx.PatternQuery(r, "classification", classifiedPattern, classifiedMessage)
	if err != nil || raw == nil {
		return nil, err
	}
	pairs := strings.Split(*raw, ",")
	slices.Sort(pairs)
	return slices.Compact(pairs), nil
}

// DimensionValueDTO is the wire form of DimensionValue.
type DimensionValueDTO struct {
	Key        string     `json:"key"`
	Name       string     `json:"name"`
	ArchivedAt *time.Time `json:"archivedAt"`
}

// DimensionDTO is the wire form of Dimension.
type DimensionDTO struct {
	Key        string              `json:"key"`
	Name       string              `json:"name"`
	BuiltIn    bool                `json:"builtIn"`
	ArchivedAt *time.Time          `json:"archivedAt"`
	Values     []DimensionValueDTO `json:"values"`
}

func dimensionDTO(d Dimension) DimensionDTO {
	values := make([]DimensionValueDTO, len(d.Values))
	for i, v := range d.Values {
		values[i] = DimensionValueDTO{Key: v.Key, Name: v.Name, ArchivedAt: v.ArchivedAt}
	}
	return DimensionDTO{Key: d.Key, Name: d.Name, BuiltIn: d.BuiltIn, ArchivedAt: d.ArchivedAt, Values: values}
}

type dimensionList struct {
	Items []DimensionDTO `json:"items"`
}

type dimensionRequest struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type updateDimensionRequest struct {
	Name     *string `json:"name"`
	Archived *bool   `json:"archived"`
}

// pathKey validates a lower-case key path parameter.
func pathKey(w http.ResponseWriter, r *http.Request, name string, re *regexp.Regexp, message string) (string, bool) {
	key := r.PathValue(name)
	if !re.MatchString(key) {
		httpx.WriteError(w, r, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: name, Message: message}))
		return "", false
	}
	return key, true
}

func (h *Handler) listDimensions(w http.ResponseWriter, r *http.Request) {
	p, _, ok := h.project(w, r, authz.RoleViewer)
	if !ok {
		return
	}
	dims, err := h.api.Dimensions(r.Context(), p.ID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := dimensionList{Items: make([]DimensionDTO, len(dims))}
	for i, d := range dims {
		out.Items[i] = dimensionDTO(d)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) createDimension(w http.ResponseWriter, r *http.Request) {
	var req dimensionRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, _, ok := h.project(w, r, authz.RoleMaintainer)
	if !ok {
		return
	}
	d, err := h.api.CreateDimension(r.Context(), p.ID, DimensionInput(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, dimensionDTO(d))
}

func (h *Handler) updateDimension(w http.ResponseWriter, r *http.Request) {
	key, ok := pathKey(w, r, "dimensionKey", DimensionKeyPattern, DimensionKeyMessage)
	if !ok {
		return
	}
	var req updateDimensionRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, _, ok := h.project(w, r, authz.RoleMaintainer)
	if !ok {
		return
	}
	d, err := h.api.UpdateDimension(r.Context(), p.ID, key, UpdateDimensionInput(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, dimensionDTO(d))
}

func (h *Handler) createDimensionValue(w http.ResponseWriter, r *http.Request) {
	key, ok := pathKey(w, r, "dimensionKey", DimensionKeyPattern, DimensionKeyMessage)
	if !ok {
		return
	}
	var req dimensionRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, _, ok := h.project(w, r, authz.RoleMaintainer)
	if !ok {
		return
	}
	d, err := h.api.CreateDimensionValue(r.Context(), p.ID, key, DimensionInput(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, dimensionDTO(d))
}

func (h *Handler) updateDimensionValue(w http.ResponseWriter, r *http.Request) {
	key, ok := pathKey(w, r, "dimensionKey", DimensionKeyPattern, DimensionKeyMessage)
	if !ok {
		return
	}
	value, ok := pathKey(w, r, "valueKey", ValueKeyPattern, ValueKeyMessage)
	if !ok {
		return
	}
	var req updateDimensionRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, _, ok := h.project(w, r, authz.RoleMaintainer)
	if !ok {
		return
	}
	d, err := h.api.UpdateDimensionValue(r.Context(), p.ID, key, value, UpdateDimensionInput(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, dimensionDTO(d))
}
