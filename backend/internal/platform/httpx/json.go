package httpx

import (
	"encoding/json"
	"errors"
	"io"
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

// DecodeJSON strictly decodes a JSON object body into dst: unknown fields
// (for example a client-supplied id) and trailing data are rejected.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
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
