package ingestion

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/ingestion/junit"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/httpx"
)

// API is the set of ingestion use cases exposed over REST.
type API interface {
	IngestJUnit(ctx context.Context, meta RunMeta, body io.Reader) (Outcome, error)
}

type diagnosticDTO struct {
	TestName            string                `json:"testName"`
	Correlation         execution.Correlation `json:"correlation"`
	RequestedTestCaseID *string               `json:"requestedTestCaseId"`
	Message             string                `json:"message"`
}

type ingestionResponse struct {
	Created     bool                      `json:"created"`
	TestRun     execution.TestRunDTO      `json:"testRun"`
	Received    int                       `json:"received"`
	Persisted   int                       `json:"persisted"`
	Diagnostics []diagnosticDTO           `json:"diagnostics"`
	ParseErrors []execution.ParseErrorDTO `json:"parseErrors"`
	Warnings    []string                  `json:"warnings"`
}

// Handler is the REST adapter of the ingestion module.
type Handler struct {
	api      API
	maxBytes int64
}

// NewHandler builds a Handler accepting reports up to maxBytes.
func NewHandler(api API, maxBytes int64) *Handler { return &Handler{api: api, maxBytes: maxBytes} }

// Register mounts the ingestion routes.
func (h *Handler) Register(mux httpx.Router) {
	mux.HandleFunc("POST /api/v1/ingestion/junit", h.ingestJUnit)
}

// contentEncoding accepts an uncompressed body (no encoding or identity) or gzip (MVP D6).
func contentEncoding(raw string) (gzipped, ok bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "identity":
		return false, true
	case "gzip", "x-gzip":
		return true, true
	}
	return false, false
}

// gzipError turns a broken gzip stream into a client error; a body over the size limit stays a 413.
func gzipError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return err
	}
	return apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: "body", Message: "is not valid gzip: " + err.Error()})
}

// decompressed reads a gzip stream up to a limit of decompressed bytes.
type decompressed struct {
	r     io.Reader
	left  int64
	limit int64
}

func (d *decompressed) Read(p []byte) (int, error) {
	if d.left <= 0 {
		// Probe one more byte: exactly the limit is fine, anything past it is too large.
		var one [1]byte
		if n, _ := d.r.Read(one[:]); n > 0 {
			return 0, &http.MaxBytesError{Limit: d.limit}
		}
	}
	if int64(len(p)) > d.left {
		p = p[:max(d.left, 0)]
	}
	n, err := d.r.Read(p)
	d.left -= int64(n)
	if err != nil && !errors.Is(err, io.EOF) {
		err = gzipError(err)
	}
	return n, err
}

// xmlMediaType returns the charset parameter of an XML Content-Type (application/xml,
// text/xml or any +xml type, RFC 7303), and false when the type is not XML.
func xmlMediaType(contentType string) (charset string, ok bool) {
	mt, params, err := mime.ParseMediaType(contentType)
	if err != nil || (mt != "application/xml" && mt != "text/xml" && !strings.HasSuffix(mt, "+xml")) {
		return "", false
	}
	return params["charset"], true
}

func (h *Handler) ingestJUnit(w http.ResponseWriter, r *http.Request) {
	charset, ok := xmlMediaType(r.Header.Get("Content-Type"))
	if !ok {
		httpx.WriteProblem(w, http.StatusUnsupportedMediaType, httpx.CodeUnsupportedMediaType, "Content-Type must be application/xml")
		return
	}
	if charset != "" && !junit.SupportedCharset(charset) {
		httpx.WriteProblem(w, http.StatusUnsupportedMediaType, httpx.CodeUnsupportedMediaType,
			"charset "+charset+" is not supported; send UTF-8 (or ISO-8859-1, windows-1252, UTF-16)")
		return
	}
	gzipped, ok := contentEncoding(r.Header.Get("Content-Encoding"))
	if !ok {
		httpx.WriteProblem(w, http.StatusUnsupportedMediaType, httpx.CodeUnsupportedMediaType,
			"Content-Encoding "+r.Header.Get("Content-Encoding")+" is not supported; send the report uncompressed or gzip")
		return
	}
	q := r.URL.Query()
	meta := RunMeta{
		ProjectKey: q.Get("project"), Provider: q.Get("provider"), ProviderRunID: q.Get("runId"),
		Pipeline: q.Get("pipeline"), Branch: q.Get("branch"), Commit: q.Get("commit"),
		Status: execution.RunStatus(q.Get("status")), Charset: charset,
	}
	// Every parameter error is reported at once.
	var fields []apperr.FieldError
	if q.Has("status") && meta.Status == "" {
		fields = append(fields, apperr.FieldError{Field: "status", Message: "must be one of completed, interrupted, cancelled"})
	}
	if q.Has("project") && meta.ProjectKey == "" {
		fields = append(fields, apperr.FieldError{Field: "project", Message: catalog.ProjectKeyMessage})
	}
	attempt, err := strconv.ParseInt(q.Get("runAttempt"), 10, 32)
	if err != nil {
		attempt = 0 // reported by ValidateMeta as "must be an integer >= 1"
	}
	meta.RunAttempt = int32(attempt)
	if e, ok := apperr.As(ValidateMeta(meta)); ok {
		fields = append(fields, e.Fields...)
	}
	if len(fields) > 0 {
		httpx.WriteError(w, r, apperr.Validation(apperr.ValidationFailed, fields...))
		return
	}
	body := io.Reader(http.MaxBytesReader(w, r.Body, h.maxBytes))
	if gzipped {
		zr, err := gzip.NewReader(body)
		if err != nil {
			httpx.WriteError(w, r, gzipError(err))
			return
		}
		// The size limit applies to the decompressed report (MVP D6): a small compressed body cannot expand past it.
		body = &decompressed{r: zr, left: h.maxBytes, limit: h.maxBytes}
	}
	out, err := h.api.IngestJUnit(r.Context(), meta, body)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	resp := ingestionResponse{
		Created: out.Created, TestRun: execution.RunDTO(out.Run), Received: out.Received, Persisted: out.Persisted,
		Diagnostics: make([]diagnosticDTO, len(out.Diagnostics)), ParseErrors: make([]execution.ParseErrorDTO, len(out.ParseErrors)),
		Warnings: out.Warnings,
	}
	for i, d := range out.Diagnostics {
		resp.Diagnostics[i] = diagnosticDTO(d)
	}
	for i, e := range out.ParseErrors {
		resp.ParseErrors[i] = execution.ToParseErrorDTO(e)
	}
	status := http.StatusOK
	if out.Created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, resp)
}
