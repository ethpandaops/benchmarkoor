package api

import (
	"context"
	"net/http"
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

// requireAuth checks for a valid session cookie and injects the user
// into the request context.
func (s *server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})
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
