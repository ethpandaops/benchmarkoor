package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/api/store"
)

type contextKey string

const userContextKey contextKey = "user"

// requestLogger logs incoming HTTP requests.
func (s *server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)

		s.log.WithField("method", r.Method).
			WithField("path", r.URL.Path).
			WithField("remote", r.RemoteAddr).
			WithField("duration", time.Since(start)).
			Debug("Request handled")
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
