// Package httpx holds the REST adapter conventions shared by every module:
// the Problem error model, JSON encoding and pagination/path parameters.
package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

// Stable error codes of the Problem model (see the OpenAPI contract).
const (
	CodeBadRequest           = "bad_request"
	CodeValidation           = "validation_error"
	CodeInvalidJUnit         = "invalid_junit"
	CodeNotFound             = "not_found"
	CodeConflict             = "conflict"
	CodeUnauthorized         = "unauthorized"
	CodeForbidden            = "forbidden"
	CodeMethodNotAllowed     = "method_not_allowed"
	CodePayloadTooLarge      = "payload_too_large"
	CodeUnsupportedMediaType = "unsupported_media_type"
	CodeServiceUnavailable   = "service_unavailable"
	CodeInternal             = "internal_error"
)

// FieldError is the wire form of apperr.FieldError.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Problem is the single error body of the API (application/problem+json).
type Problem struct {
	Type   string       `json:"type"`
	Title  string       `json:"title"`
	Status int          `json:"status"`
	Code   string       `json:"code"`
	Detail string       `json:"detail,omitempty"`
	Errors []FieldError `json:"errors,omitempty"`
}

// WriteProblem writes a Problem response.
func WriteProblem(w http.ResponseWriter, status int, code, detail string, fields ...FieldError) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Problem{
		Type:   "about:blank",
		Title:  http.StatusText(status),
		Status: status,
		Code:   code,
		Detail: detail,
		Errors: fields,
	})
}

// WriteError maps an error returned by a service to a Problem response.
// Unknown errors are logged and reported as a generic 500 without internals.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrUnsupportedMediaType) {
		WriteProblem(w, http.StatusUnsupportedMediaType, CodeUnsupportedMediaType, err.Error())
		return
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		WriteProblem(w, http.StatusRequestEntityTooLarge, CodePayloadTooLarge, "request body is too large")
		return
	}
	if e, ok := apperr.As(err); ok {
		switch e.Kind {
		case apperr.KindNotFound:
			WriteProblem(w, http.StatusNotFound, CodeNotFound, e.Message)
			return
		case apperr.KindConflict:
			WriteProblem(w, http.StatusConflict, CodeConflict, e.Message)
			return
		case apperr.KindUnauthorized:
			WriteProblem(w, http.StatusUnauthorized, CodeUnauthorized, e.Message)
			return
		case apperr.KindForbidden:
			WriteProblem(w, http.StatusForbidden, CodeForbidden, e.Message)
			return
		case apperr.KindInvalidDocument:
			WriteProblem(w, http.StatusBadRequest, CodeInvalidJUnit, e.Message)
			return
		case apperr.KindValidation:
			fields := make([]FieldError, len(e.Fields))
			for i, f := range e.Fields {
				fields[i] = FieldError(f)
			}
			WriteProblem(w, http.StatusBadRequest, CodeValidation, e.Message, fields...)
			return
		}
	}
	slog.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	WriteProblem(w, http.StatusInternalServerError, CodeInternal, "an unexpected error occurred")
}
