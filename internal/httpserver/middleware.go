package httpserver

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"f1-jf/internal/ctxlog"
)

// requestIDHeader is the header honored (and used for correlation) when a
// client supplies its own request ID.
const requestIDHeader = "X-Request-ID"

// withMiddleware wraps the router in panic recovery and structured request
// logging. Each request gets a request ID shared by every log line emitted
// for it.
func withMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return recoverer(requestLogger(logger, next))
}

// requestLogger records each request at debug level and attaches a request ID
// (honoring an inbound X-Request-ID, otherwise generating one) to the request
// context so downstream packages log with it. The application logger is stashed
// in the context too, letting service internals pick up the request ID.
func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := r.Header.Get(requestIDHeader)
		if id == "" {
			id = newRequestID()
		}
		reqLog := logger.With(slog.String("request_id", id))
		ctx := ctxlog.With(r.Context(), reqLog)
		r = r.WithContext(ctx)

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		reqLog.Debug("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(start).String(),
			"remote", r.RemoteAddr,
		)
	})
}

// recoverer converts panics into 500 responses and logs them.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				ctxlog.From(r.Context()).Error("panic recovered", "panic", rec, "path", r.URL.Path)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// newRequestID returns a random hex request ID.
func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().Format("req-20060102T150405.000000000")
	}
	return hex.EncodeToString(b[:])
}

// statusRecorder captures the response status code for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Flush satisfies http.Flusher so streaming responses (raw TS) are pushed to
// the client promptly even though the middleware wraps the writer.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
