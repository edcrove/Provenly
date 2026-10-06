package catalog

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

// Taxonomy limits and formats (mirrored in the OpenAPI contract and the database).
const (
	MaxTags            = 20
	maxDimensions      = 30
	maxDimensionValues = 100
	maxTaxonomyName    = 60
)

var (
	// TagPattern is the format of a tag once normalized (trimmed, lower-cased).
	TagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}$`)
	// DimensionKeyPattern is the format of a dimension key.
	DimensionKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,29}$`)
	// ValueKeyPattern is the format of a dimension value key.
	ValueKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,29}$`)
)

// Messages of malformed taxonomy keys.
const (
	TagMessage          = "must be 1 to 40 lower-case letters, digits, '.', '_' or '-', starting with a letter or digit"
	DimensionKeyMessage = "must be 1 to 30 lower-case letters, digits or '-', starting with a letter"
	ValueKeyMessage     = "must be 1 to 30 lower-case letters, digits or '-', starting with a letter or digit"
)

// normalizeTags trims and lower-cases tags, drops duplicates and sorts them; it reports malformed ones on v.
func normalizeTags(v *apperr.Validator, tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if !TagPattern.MatchString(t) {
			v.Check(false, "tags", fmt.Sprintf("%q %s", t, TagMessage))
			continue
		}
		out = append(out, t)
	}
	slices.Sort(out)
	out = slices.Compact(out)
	v.Check(len(out) <= MaxTags, "tags", fmt.Sprintf("a test case can have at most %d tags", MaxTags))
	return out
}

// assignment is one dimension of a test case to set (valueID > 0) or clear (valueID 0).
type assignment struct{ dimensionID, valueID int64 }

// resolveClassification maps dimension and value keys to ids. A value must belong to an active dimension of the
// project and be active itself, unless the test case already has it (current), so re-sending it is harmless.
func resolveClassification(dims []Dimension, want map[string]*string, current map[string]string) ([]assignment, error) {
	var v apperr.Validator
	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]assignment, 0, len(keys))
	for _, k := range keys {
		field := "classification." + k
		i := slices.IndexFunc(dims, func(d Dimension) bool { return d.Key == k })
		if i < 0 {
			v.Check(false, field, "is not a dimension of the project")
			continue
		}
		d := dims[i]
		if want[k] == nil {
			out = append(out, assignment{dimensionID: d.ID})
			continue
		}
		val, ok := d.value(*want[k])
		switch {
		case !ok:
			v.Check(false, field, fmt.Sprintf("%q is not a value of %s", *want[k], d.Name))
		case current[k] == val.Key:
			out = append(out, assignment{dimensionID: d.ID, valueID: val.ID})
		case d.ArchivedAt != nil:
			v.Check(false, field, "the dimension is archived")
		case val.ArchivedAt != nil:
			v.Check(false, field, fmt.Sprintf("%q is archived", val.Key))
		default:
			out = append(out, assignment{dimensionID: d.ID, valueID: val.ID})
		}
	}
	return out, v.Err()
}

// applyTaxonomy writes the tags (when not nil) and the classification of a test case inside a transaction.
func applyTaxonomy(ctx context.Context, r Repository, tc TestCase, tags *[]string, classification map[string]*string) error {
	if tags != nil {
		if err := r.SetTags(ctx, tc.ID, *tags); err != nil {
			return err
		}
	}
	if len(classification) == 0 {
		return nil
	}
	dims, err := r.ListDimensions(ctx, tc.ProjectID)
	if err != nil {
		return err
	}
	set, err := resolveClassification(dims, classification, tc.Classification)
	if err != nil {
		return err
	}
	for _, a := range set {
		if err := r.SetClassification(ctx, tc.ID, tc.ProjectID, a.dimensionID, a.valueID); err != nil {
			return err
		}
	}
	return nil
}

// Dimensions returns a project's classification dimensions with their values.
func (s *Service) Dimensions(ctx context.Context, projectID int64) ([]Dimension, error) {
	return s.repo.ListDimensions(ctx, projectID)
}

func validateTaxonomyName(v *apperr.Validator, name *string) {
	if name != nil {
		*name = strings.TrimSpace(*name)
		v.Check(*name != "", "name", "must not be empty")
		v.Check(validLen(*name, maxTaxonomyName), "name", fmt.Sprintf("must be at most %d characters", maxTaxonomyName))
		v.CheckText("name", *name)
	}
}

func dimensionNotFound(key string) error { return apperr.NotFound("dimension %s not found", key) }

// dimension returns one of the project's dimensions by key.
func dimension(ctx context.Context, r Repository, projectID int64, key string) (Dimension, error) {
	dims, err := r.ListDimensions(ctx, projectID)
	if err != nil {
		return Dimension{}, err
	}
	i := slices.IndexFunc(dims, func(d Dimension) bool { return d.Key == key })
	if i < 0 {
		return Dimension{}, dimensionNotFound(key)
	}
	return dims[i], nil
}

// CreateDimension adds a project-specific dimension; its key never changes.
func (s *Service) CreateDimension(ctx context.Context, projectID int64, in DimensionInput) (Dimension, error) {
	var v apperr.Validator
	v.Check(DimensionKeyPattern.MatchString(in.Key), "key", DimensionKeyMessage)
	validateTaxonomyName(&v, &in.Name)
	if err := v.Err(); err != nil {
		return Dimension{}, err
	}
	var d Dimension
	err := s.repo.InTx(ctx, func(r Repository) error {
		if err := r.LockScope(ctx, "dimensions", projectID); err != nil {
			return err
		}
		dims, err := r.ListDimensions(ctx, projectID)
		if err != nil {
			return err
		}
		if len(dims) >= maxDimensions {
			return apperr.Validation("too many dimensions", apperr.FieldError{Field: "key", Message: fmt.Sprintf("a project can have at most %d dimensions", maxDimensions)})
		}
		d, err = r.CreateDimension(ctx, projectID, in)
		if errors.Is(err, ErrConflict) {
			return apperr.Conflict("dimension %s already exists", in.Key)
		}
		return err
	})
	return d, err
}

// UpdateDimension renames, archives or restores a dimension. Archived dimensions keep their test case values
// but cannot be assigned anymore.
func (s *Service) UpdateDimension(ctx context.Context, projectID int64, key string, in UpdateDimensionInput) (Dimension, error) {
	var v apperr.Validator
	v.Check(in.Name != nil || in.Archived != nil, "body", "at least one field is required")
	validateTaxonomyName(&v, in.Name)
	if err := v.Err(); err != nil {
		return Dimension{}, err
	}
	if _, err := s.repo.UpdateDimension(ctx, projectID, key, in); err != nil {
		if errors.Is(err, ErrNotFound) {
			return Dimension{}, dimensionNotFound(key)
		}
		return Dimension{}, err
	}
	return dimension(ctx, s.repo, projectID, key)
}

// CreateDimensionValue appends a controlled value to a dimension and returns the dimension.
func (s *Service) CreateDimensionValue(ctx context.Context, projectID int64, dimensionKey string, in DimensionInput) (Dimension, error) {
	var v apperr.Validator
	v.Check(ValueKeyPattern.MatchString(in.Key), "key", ValueKeyMessage)
	validateTaxonomyName(&v, &in.Name)
	if err := v.Err(); err != nil {
		return Dimension{}, err
	}
	var d Dimension
	err := s.repo.InTx(ctx, func(r Repository) error {
		// Count and number the values only once concurrent creations in this dimension are done.
		if err := r.LockScope(ctx, "dimension "+dimensionKey, projectID); err != nil {
			return err
		}
		dim, err := dimension(ctx, r, projectID, dimensionKey)
		if err != nil {
			return err
		}
		if len(dim.Values) >= maxDimensionValues {
			return apperr.Validation("too many values", apperr.FieldError{Field: "key", Message: fmt.Sprintf("a dimension can have at most %d values", maxDimensionValues)})
		}
		if _, err := r.CreateDimensionValue(ctx, dim.ID, in); err != nil {
			if errors.Is(err, ErrConflict) {
				return apperr.Conflict("%s already has the value %s", dimensionKey, in.Key)
			}
			return err
		}
		d, err = dimension(ctx, r, projectID, dimensionKey)
		return err
	})
	return d, err
}

// UpdateDimensionValue renames, archives or restores a value and returns its dimension.
func (s *Service) UpdateDimensionValue(ctx context.Context, projectID int64, dimensionKey, valueKey string, in UpdateDimensionInput) (Dimension, error) {
	var v apperr.Validator
	v.Check(in.Name != nil || in.Archived != nil, "body", "at least one field is required")
	validateTaxonomyName(&v, in.Name)
	if err := v.Err(); err != nil {
		return Dimension{}, err
	}
	dim, err := dimension(ctx, s.repo, projectID, dimensionKey)
	if err != nil {
		return Dimension{}, err
	}
	if _, err := s.repo.UpdateDimensionValue(ctx, dim.ID, valueKey, in); err != nil {
		if errors.Is(err, ErrNotFound) {
			return Dimension{}, apperr.NotFound("value %s of dimension %s not found", valueKey, dimensionKey)
		}
		return Dimension{}, err
	}
	return dimension(ctx, s.repo, projectID, dimensionKey)
}
