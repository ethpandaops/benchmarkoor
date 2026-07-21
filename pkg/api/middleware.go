package api

import (
	"compress/gzip"
	"context"
	"crypto/subtle"
	"io"
	"net/http"
	"strings"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/ethpandaops/benchmarkoor/pkg/api/store"
)

type contextKey string

const (
	userContextKey                  contextKey = "user"
	compressedRequestSizeContextKey contextKey = "compressed_request_size"
)

const (
	// maxIngestBodyBytes bounds the raw, on-the-wire ingest request body,
	// before any decompression. Applies whether or not the request is
	// gzip-encoded. Comfortably above any real live-run report while still
	// being a hard limit.
	maxIngestBodyBytes = 10 << 20 // 10 MiB

	// maxIngestDecompressedBytes bounds the decompressed size of a gzipped
	// ingest body. Without this, a small gzip payload can inflate to an
	// arbitrary size in memory before any JSON validation ever runs.
	maxIngestDecompressedBytes = 50 << 20 // 50 MiB
)

// countingReader wraps an io.Reader and tracks total bytes read. Used by
// gzipRequestBody to surface the on-the-wire (gzipped) request size to
// handlers via context — the decompressed body size is just len(body)
// at the handler.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)

	return n, err
}

// requestLogger logs incoming HTTP requests with status code, bytes written,
// and duration. Canceled or slow/errored requests are logged at Warn level.
func (s *server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()

		next.ServeHTTP(ww, r)

		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}

		duration := time.Since(start)

		entry := s.log.WithField("method", r.Method).
			WithField("path", r.URL.Path).
			WithField("remote", r.RemoteAddr).
			WithField("status", status).
			WithField("bytes", ww.BytesWritten()).
			WithField("duration", duration)

		switch {
		case r.Context().Err() == context.Canceled:
			entry.WithField("canceled", true).
				Warn("Request canceled")
		case status >= 500 || duration > 5*time.Second:
			entry.Warn("Request handled")
		default:
			entry.Info("Request handled")
		}
	})
}

// requireAuth checks for a Bearer API key or session cookie and injects
// the user into the request context.
func (s *server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try Bearer token first.
		if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
			s.authenticateAPIKey(w, r, next, authHeader[7:])

			return
		}

		// Fall back to session cookie.
		s.authenticateSession(w, r, next)
	})
}

// authenticateAPIKey validates a Bearer API key and serves the request.
func (s *server) authenticateAPIKey(
	w http.ResponseWriter,
	r *http.Request,
	next http.Handler,
	token string,
) {
	hash := hashAPIKey(token)

	apiKey, err := s.store.GetAPIKeyByHash(r.Context(), hash)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized,
			errorResponse{"invalid api key"})

		return
	}

	if apiKey.ExpiresAt != nil && time.Now().UTC().After(*apiKey.ExpiresAt) {
		writeJSON(w, http.StatusUnauthorized,
			errorResponse{"api key expired"})

		return
	}

	// API keys are restricted to read-only operations.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSON(w, http.StatusForbidden,
			errorResponse{"api keys are restricted to read-only operations"})

		return
	}

	user, err := s.store.GetUserByID(r.Context(), apiKey.UserID)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized,
			errorResponse{"user not found"})

		return
	}

	// Throttle LastUsedAt updates to every 5 minutes.
	if apiKey.LastUsedAt == nil ||
		time.Since(*apiKey.LastUsedAt) > 5*time.Minute {
		go func() {
			if err := s.store.UpdateAPIKeyLastUsed(
				context.Background(), apiKey.ID, time.Now().UTC(),
			); err != nil {
				s.log.WithError(err).
					Warn("Failed to update api key last used")
			}
		}()
	}

	ctx := context.WithValue(r.Context(), userContextKey, user)
	next.ServeHTTP(w, r.WithContext(ctx))
}

// authenticateSession validates a session cookie and serves the request.
func (s *server) authenticateSession(
	w http.ResponseWriter,
	r *http.Request,
	next http.Handler,
) {
	cookie, err := r.Cookie("benchmarkoor_session")
	if err != nil {
		writeJSON(w, http.StatusUnauthorized,
			errorResponse{"authentication required"})

		return
	}

	session, err := s.store.GetSessionByToken(r.Context(), cookie.Value)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized,
			errorResponse{"invalid or expired session"})

		return
	}

	if time.Now().UTC().After(session.ExpiresAt) {
		_ = s.store.DeleteSession(r.Context(), cookie.Value)
		writeJSON(w, http.StatusUnauthorized,
			errorResponse{"session expired"})

		return
	}

	user, err := s.store.GetUserByID(r.Context(), session.UserID)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized,
			errorResponse{"user not found"})

		return
	}

	if session.LastActiveAt == nil ||
		time.Since(*session.LastActiveAt) > 5*time.Minute {
		go func() {
			if err := s.store.UpdateSessionLastActive(
				context.Background(), session.ID, time.Now().UTC(),
			); err != nil {
				s.log.WithError(err).
					Warn("Failed to update session last active")
			}
		}()
	}

	ctx := context.WithValue(r.Context(), userContextKey, user)
	next.ServeHTTP(w, r.WithContext(ctx))
}

// requireRole checks that the authenticated user has the specified role.
func (s *server) requireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := userFromContext(r.Context())
			if user == nil || user.Role != role {
				writeJSON(w, http.StatusForbidden,
					errorResponse{"insufficient permissions"})

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// userFromContext extracts the authenticated user from the request context.
func userFromContext(ctx context.Context) *store.User {
	user, _ := ctx.Value(userContextKey).(*store.User)

	return user
}

// requireIngestToken authenticates a runner using the shared ingest token
// configured via api.ingest.token. This is a separate auth path from
// session/API-key auth because runners are not users and the ingest token
// is a single static shared secret.
func (s *server) requireIngestToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := ""
		if s.cfg.Ingest != nil {
			expected = s.cfg.Ingest.Token
		}

		if expected == "" {
			writeJSON(w, http.StatusServiceUnavailable,
				errorResponse{"ingest endpoint is not configured"})

			return
		}

		got := ""
		if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			got = h[7:]
		}

		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
			writeJSON(w, http.StatusUnauthorized,
				errorResponse{"invalid ingest token"})

			return
		}

		next.ServeHTTP(w, r)
	})
}

// gzipRequestBody transparently inflates gzipped request bodies so
// downstream handlers can read them as if they were uncompressed. Used on
// the ingest subrouter where the runner posts large gzipped payloads
// (per-test heatmap data). A malformed gzip stream is returned as 400.
//
// The raw body is capped at maxIngestBodyBytes regardless of encoding, and
// a gzip-encoded body's decompressed output is separately capped at
// maxIngestDecompressedBytes - without the second cap, a small compressed
// payload could inflate to an arbitrary size in memory before the handler
// gets a chance to reject it.
func (s *server) gzipRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxIngestBodyBytes)

		if !strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
			next.ServeHTTP(w, r)

			return
		}

		// Wrap the raw body in a counter so handlers can log the
		// on-the-wire size after they finish reading. The counter
		// accumulates as gzip.NewReader pulls compressed bytes through.
		cr := &countingReader{r: r.Body}

		gz, err := gzip.NewReader(cr)
		if err != nil {
			writeJSON(w, http.StatusBadRequest,
				errorResponse{"invalid gzip request body"})

			return
		}
		defer func() { _ = gz.Close() }()

		// Replace the body with the decompressed reader, capped
		// independently of the compressed-size limit above. Strip the
		// header so handlers can rely on Content-Length being absent
		// rather than stale. ContentLength is also reset because it
		// referred to the compressed size.
		r.Body = http.MaxBytesReader(w, gz, maxIngestDecompressedBytes)
		r.Header.Del("Content-Encoding")
		r.Header.Del("Content-Length")
		r.ContentLength = -1

		ctx := context.WithValue(r.Context(), compressedRequestSizeContextKey, cr)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// compressedRequestSize returns the number of compressed (on-the-wire)
// bytes read by gzipRequestBody for the current request. Returns 0 when
// the request was not gzipped or the middleware did not run. Must be
// called after the handler has fully read the body — until then the
// counter is incomplete.
func compressedRequestSize(r *http.Request) int64 {
	cr, ok := r.Context().Value(compressedRequestSizeContextKey).(*countingReader)
	if !ok {
		return 0
	}

	return cr.n
}
