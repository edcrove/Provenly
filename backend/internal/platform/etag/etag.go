// Package etag implements optimistic locking over versioned resources (MVP D7):
// a resource's entity tag is its version ("7"), and If-Match lists the tags the
// client last read (RFC 9110 §13.1.1, strong comparison).
package etag

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

// Match is a parsed If-Match header. The zero value (no header) matches every version.
type Match struct {
	present bool
	any     bool
	tags    []string
}

// tagPattern is an entity tag (optionally weak): W/"opaque".
var tagPattern = regexp.MustCompile(`^(W/)?"[\x21\x23-\x7e]*"$`)

// Tag is the entity tag of a version.
func Tag(version int64) string { return `"` + strconv.FormatInt(version, 10) + `"` }

// Parse reads an If-Match value: empty (no precondition), "*" (any version) or a comma-separated list of entity
// tags. Weak tags are accepted but never match: If-Match uses strong comparison.
func Parse(header string) (Match, error) {
	header = strings.TrimSpace(header)
	switch header {
	case "":
		return Match{}, nil
	case "*":
		return Match{present: true, any: true}, nil
	}
	m := Match{present: true}
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimSpace(t)
		if !tagPattern.MatchString(t) {
			return Match{}, apperr.Validation(apperr.ValidationFailed,
				apperr.FieldError{Field: "If-Match", Message: `must be "*" or entity tags such as "7", as returned in ETag`})
		}
		if !strings.HasPrefix(t, "W/") {
			m.tags = append(m.tags, t)
		}
	}
	return m, nil
}

// FromRequest parses every If-Match header of a request.
func FromRequest(r *http.Request) (Match, error) {
	return Parse(strings.Join(r.Header.Values("If-Match"), ","))
}

// Present tells whether the client sent a precondition.
func (m Match) Present() bool { return m.present }

// Matches tells whether the current version satisfies the precondition.
func (m Match) Matches(version int64) bool {
	if !m.present || m.any {
		return true
	}
	tag := Tag(version)
	for _, t := range m.tags {
		if t == tag {
			return true
		}
	}
	return false
}
