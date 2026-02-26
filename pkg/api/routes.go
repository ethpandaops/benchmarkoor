package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

// buildRouter constructs the chi router with all routes and middleware.
func (s *server) buildRouter() http.Handler {
	r := chi.NewRouter()

	// Global middleware.
	r.Use(chimw.Recoverer)
	r.Use(s.requestLogger)
	r.Use(s.corsMiddleware())

	r.Route("/api/v1", func(r chi.Router) {
		// Public endpoints.
		r.Get("/health", s.handleHealth)
		r.Get("/config", s.handleConfig)

		// Auth endpoints.
		r.Route("/auth", func(r chi.Router) {
			// Apply rate limiting to auth endpoints.
			if s.cfg.Server.RateLimit.Enabled {
				r.Use(s.rateLimitMiddleware(
					s.cfg.Server.RateLimit.Auth,
				))
			}

			r.Post("/login", s.handleLogin)
			r.Post("/logout", s.handleLogout)

			r.Group(func(r chi.Router) {
				r.Use(s.requireAuth)
				r.Get("/me", s.handleMe)

				// API key management (authenticated users).
				r.Post("/api-keys", s.handleCreateAPIKey)
				r.Get("/api-keys", s.handleListMyAPIKeys)
				r.Delete("/api-keys/{id}", s.handleDeleteMyAPIKey)
			})

			// GitHub OAuth.
			if s.cfg.Auth.GitHub.Enabled {
				r.Get("/github", s.handleGitHubAuth)
				r.Get("/github/callback", s.handleGitHubCallback)
			}
		})

		// File serving endpoints (local filesystem or S3 presigned URLs).
		r.Route("/files", func(r chi.Router) {
			if !s.cfg.Auth.AnonymousRead {
				r.Use(s.requireAuth)
			}

			if s.cfg.Server.RateLimit.Enabled {
				r.Use(s.rateLimitMiddleware(
					s.cfg.Server.RateLimit.Authenticated,
				))
			}

			r.Get("/*", s.handleFileRequest)
			r.Head("/*", s.handleFileRequest)
		})

		// Index endpoints (when indexing is enabled).
		if s.indexStore != nil {
			r.Route("/index", func(r chi.Router) {
				if !s.cfg.Auth.AnonymousRead {
					r.Use(s.requireAuth)
				}

				if s.cfg.Server.RateLimit.Enabled {
					r.Use(s.rateLimitMiddleware(
						s.cfg.Server.RateLimit.Authenticated,
					))
				}

				r.Get("/", s.handleIndex)
				r.Get("/suites/{hash}/stats", s.handleSuiteStats)
			})
		}

		// Admin endpoints (require auth + admin role).
		r.Route("/admin", func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Use(s.requireRole("admin"))

			if s.cfg.Server.RateLimit.Enabled {
				r.Use(s.rateLimitMiddleware(
					s.cfg.Server.RateLimit.Authenticated,
				))
			}

			// User management.
			r.Get("/users", s.handleListUsers)
			r.Post("/users", s.handleCreateUser)
			r.Put("/users/{id}", s.handleUpdateUser)
			r.Delete("/users/{id}", s.handleDeleteUser)

			// Session management.
			r.Get("/sessions", s.handleListSessions)
			r.Delete("/sessions/{id}", s.handleDeleteSessionByID)

			// API key management (admin).
			r.Get("/api-keys", s.handleListAllAPIKeys)
			r.Delete("/api-keys/{id}", s.handleDeleteAPIKey)

			// GitHub org mappings.
			r.Get("/github/org-mappings", s.handleListOrgMappings)
			r.Post("/github/org-mappings", s.handleUpsertOrgMapping)
			r.Delete("/github/org-mappings/{id}",
				s.handleDeleteOrgMapping)

			// GitHub user mappings.
			r.Get("/github/user-mappings",
				s.handleListUserMappings)
			r.Post("/github/user-mappings",
				s.handleUpsertUserMapping)
			r.Delete("/github/user-mappings/{id}",
				s.handleDeleteUserMapping)
		})
	})

	return r
}

// corsMiddleware returns a CORS handler configured from the API config.
func (s *server) corsMiddleware() func(http.Handler) http.Handler {
	opts := cors.Options{
		AllowedMethods:   []string{"GET", "HEAD", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
		MaxAge:           300,
	}

	origins := s.cfg.Server.CORSOrigins

	if len(origins) == 0 || (len(origins) == 1 && origins[0] == "*") {
		// Reflect the requesting origin so credentials work from any origin.
		opts.AllowOriginFunc = func(_ *http.Request, _ string) bool {
			return true
		}
	} else {
		opts.AllowedOrigins = origins
	}

	return cors.Handler(opts)
}
