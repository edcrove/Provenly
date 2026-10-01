package httpx

import (
	"log/slog"
	"net/http"
	"time"
)

// Recover turns panics into a 500 Problem response.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.ErrorContext(r.Context(), "panic serving request", "method", r.Method, "path", r.URL.Path, "panic", rec)
				WriteProblem(w, http.StatusInternalServerError, CodeInternal, "an unexpected error occurred")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// AccessLog logs one line per request.
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.InfoContext(r.Context(), "http request", "method", r.Method, "path", r.URL.Path, "status", rec.status, "duration", time.Since(start))
	})
}

// Routes serves mux and answers requests it cannot route with a Problem: 405
// (with the Allow header) when the path exists for other methods, 404 otherwise.
func Routes(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, pattern := mux.Handler(r)
		if pattern != "" {
			mux.ServeHTTP(w, r)
			return
		}
		// The mux's own fallback decides between 404 and 405 and sets Allow.
		probe := &probeWriter{header: http.Header{}, status: http.StatusOK}
		h.ServeHTTP(probe, r)
		if probe.status == http.StatusMethodNotAllowed {
			w.Header().Set("Allow", probe.header.Get("Allow"))
			WriteProblem(w, http.StatusMethodNotAllowed, CodeMethodNotAllowed, "method "+r.Method+" is not allowed for "+r.URL.Path)
			return
		}
		WriteProblem(w, http.StatusNotFound, CodeNotFound, "no route for "+r.Method+" "+r.URL.Path)
	})
}

// probeWriter captures the status and headers of a handler without a body.
type probeWriter struct {
	header http.Header
	status int
}

func (p *probeWriter) Header() http.Header         { return p.header }
func (p *probeWriter) Write(b []byte) (int, error) { return len(b), nil }
func (p *probeWriter) WriteHeader(status int)      { p.status = status }
