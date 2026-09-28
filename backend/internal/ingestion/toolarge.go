package ingestion

import (
	"errors"
	"net/http"
)

func isBodyTooLarge(err error) bool {
	var tooLarge *http.MaxBytesError
	return errors.As(err, &tooLarge)
}
