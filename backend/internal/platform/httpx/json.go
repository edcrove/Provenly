package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

// maxJSONBody bounds JSON request bodies.
const maxJSONBody = 1 << 20

// WriteJSON writes v as a JSON response with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ErrUnsupportedMediaType is returned for a JSON operation called with another Content-Type.
var ErrUnsupportedMediaType = errors.New("Content-Type must be application/json")

// DecodeJSON strictly decodes a JSON object body into dst: the Content-Type must
// be application/json, and unknown fields (for example a client-supplied id) and
// trailing data are rejected.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return ErrUnsupportedMediaType
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return err
		}
		if errors.Is(err, io.EOF) {
			return apperr.Validation("request body is required")
		}
		return apperr.Validation("malformed JSON body: " + err.Error())
	}
	if dec.More() {
		return apperr.Validation("request body must contain a single JSON object")
	}
	return nil
}
