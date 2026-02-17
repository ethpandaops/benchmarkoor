package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/api/store"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/sirupsen/logrus"
)

const (
	shutdownTimeout        = 10 * time.Second
	sessionCleanupInterval = 15 * time.Minute
)

// Server exposes the API HTTP server lifecycle.
type Server interface {
	Start(ctx context.Context) error
	Stop() error
}

// Compile-time interface check.
var _ Server = (*server)(nil)

type server struct {
	log        logrus.FieldLogger
	cfg        *config.APIConfig
	store      store.Store
	presigner  *s3Presigner
	httpServer *http.Server
	wg         sync.WaitGroup
	done       chan struct{}
}

// NewServer creates a new API server.
func NewServer(
	log logrus.FieldLogger,
	cfg *config.APIConfig,
) Server {
	return &server{
		log:  log.WithField("component", "api"),
		cfg:  cfg,
		done: make(chan struct{}),
	}
}

// Start initializes the store, seeds config data, and starts the HTTP server.
func (s *server) Start(ctx context.Context) error {
	// Create and start the database store.
	s.store = store.NewStore(s.log, &s.cfg.Database)
	if err := s.store.Start(ctx); err != nil {
		return fmt.Errorf("starting store: %w", err)
	}

	// Seed users from config.
	if s.cfg.Auth.Basic.Enabled {
		if err := s.store.SeedUsers(
			ctx, s.cfg.Auth.Basic.Users,
		); err != nil {
			return fmt.Errorf("seeding users: %w", err)
		}
	}

	// Seed GitHub mappings from config.
	if s.cfg.Auth.GitHub.Enabled {
		if err := s.store.SeedGitHubMappings(
			ctx,
			s.cfg.Auth.GitHub.OrgRoleMapping,
			s.cfg.Auth.GitHub.UserRoleMapping,
		); err != nil {
			return fmt.Errorf("seeding github mappings: %w", err)
		}
	}

	// Initialize S3 presigner if configured.
	if s.cfg.Storage.S3 != nil && s.cfg.Storage.S3.Enabled {
		presigner, err := newS3Presigner(s.log, s.cfg.Storage.S3)
		if err != nil {
			return fmt.Errorf("initializing s3 presigner: %w", err)
		}

		s.presigner = presigner

		s.log.Info("S3 presigned URL generation enabled")
	}

	// Build router and start HTTP server.
	router := s.buildRouter()

	s.httpServer = &http.Server{
		Addr:              s.cfg.Server.Listen,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start session cleanup goroutine.
	s.wg.Add(1)

	go func() {
		defer s.wg.Done()

		ticker := time.NewTicker(sessionCleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := s.store.DeleteExpiredSessions(ctx); err != nil {
					s.log.WithError(err).
						Warn("Failed to clean expired sessions")
				}
			case <-s.done:
				return
			}
		}
	}()

	// Bind the listener synchronously so we fail fast on port conflicts.
	ln, err := net.Listen("tcp", s.cfg.Server.Listen)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", s.cfg.Server.Listen, err)
	}

	// Start HTTP server.
	s.wg.Add(1)

	go func() {
		defer s.wg.Done()

		s.log.WithField("listen", s.cfg.Server.Listen).
			Info("API server starting")

		if err := s.httpServer.Serve(ln); err != nil &&
			err != http.ErrServerClosed {
			s.log.WithError(err).Error("HTTP server error")
		}
	}()

	return nil
}

// Stop gracefully shuts down the HTTP server and closes the store.
func (s *server) Stop() error {
	close(s.done)

	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(
			context.Background(), shutdownTimeout,
		)
		defer cancel()

		if err := s.httpServer.Shutdown(ctx); err != nil {
			s.log.WithError(err).Warn("HTTP server shutdown error")
		}
	}

	s.wg.Wait()

	if s.store != nil {
		if err := s.store.Stop(); err != nil {
			return fmt.Errorf("stopping store: %w", err)
		}
	}

	s.log.Info("API server stopped")

	return nil
}
