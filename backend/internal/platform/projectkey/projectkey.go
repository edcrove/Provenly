// Package projectkey is the one place that knows what a project key looks like, how a malformed or unknown key is
// answered and how a key becomes a project the caller may see (card #45): every route, the ingestion and the MCP tools
// give the same code and message for the same mistake.
package projectkey

import (
	"context"
	"net/http"
	"regexp"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
)

// Pattern is the format of a project key (mirrored in the OpenAPI contract and the database).
var Pattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)

// Message is the field error of a malformed project key.
const Message = "must be a project key: 2 to 10 upper-case letters or digits, starting with a letter (e.g. CHK)"

// Valid tells whether key has the format of a project key.
func Valid(key string) bool { return Pattern.MatchString(key) }

// Check records a field error when key is not a project key.
func Check(v *apperr.Validator, field, key string) { v.Check(Valid(key), field, Message) }

// Invalid is the 400 of a malformed project key in field.
func Invalid(field string) error {
	return apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: field, Message: Message})
}

// NotFound is the 404 of a project that does not exist or that the caller may not see: the two are never told apart.
func NotFound(key string) error { return apperr.NotFound("project %s not found", key) }

// Path reads the {projectKey} path segment: 400 when it is not a project key.
func Path(r *http.Request) (string, error) {
	key := r.PathValue("projectKey")
	if !Valid(key) {
		return "", Invalid("projectKey")
	}
	return key, nil
}

// Query reads the optional ?project= parameter: nil when absent, 400 when present but not a project key.
func Query(r *http.Request) (*string, error) {
	q := r.URL.Query()
	if !q.Has("project") {
		return nil, nil
	}
	key := q.Get("project")
	if !Valid(key) {
		return nil, Invalid("project")
	}
	return &key, nil
}

// Resolve turns a key into the project it names, checking the caller holds at least role in it. A malformed key is a
// 400 on field; an unknown key and a project the caller may not see are the same 404.
func Resolve[P any](ctx context.Context, field, key string, lookup func(context.Context, string) (P, error), id func(P) int64,
	guard interface {
		Require(ctx context.Context, projectID int64, minRole authz.Role, notFound error) error
	}, role authz.Role) (P, error) {
	var zero P
	if !Valid(key) {
		return zero, Invalid(field)
	}
	p, err := lookup(ctx, key)
	if err != nil {
		if e, ok := apperr.As(err); ok && e.Kind == apperr.KindNotFound {
			return zero, NotFound(key)
		}
		return zero, err
	}
	if err := guard.Require(ctx, id(p), role, NotFound(key)); err != nil {
		return zero, err
	}
	return p, nil
}
