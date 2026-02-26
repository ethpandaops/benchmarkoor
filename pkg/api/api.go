package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexer"
	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
	"github.com/ethpandaops/benchmarkoor/pkg/api/storage"
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
	log           logrus.FieldLogger
	cfg           *config.APIConfig
	store         store.Store
	presigner     *s3Presigner
	localServer   *localFileServer
	indexStore    indexstore.Store
	indexer       indexer.Indexer
	storageReader storage.Reader
	httpServer    *http.Server
	wg            sync.WaitGroup
	done          chan struct{}
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

	// Initialize local file server if configured.
	if s.cfg.Storage.Local != nil && s.cfg.Storage.Local.Enabled {
		s.localServer = newLocalFileServer(s.log, s.cfg.Storage.Local)

		s.log.Info("Local file serving enabled")
	}

	// Prepare the indexing service (store + reader) before building the
	// router so that the index endpoints are wired, but do NOT start the
	// background indexer yet — the HTTP server must be listening first.
	if s.cfg.Indexing != nil && s.cfg.Indexing.Enabled {
		if err := s.prepareIndexing(ctx); err != nil {
			return fmt.Errorf("preparing indexing: %w", err)
		}
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

				if err := s.store.DeleteExpiredAPIKeys(ctx); err != nil {
					s.log.WithError(err).
						Warn("Failed to clean expired API keys")
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

	// Start the background indexer AFTER the API is listening so that
	// the server is reachable while the first (potentially slow) pass runs.
	if s.indexer != nil {
		if err := s.indexer.Start(ctx); err != nil {
			return fmt.Errorf("starting indexer: %w", err)
		}
	}

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

	if s.indexer != nil {
		if err := s.indexer.Stop(); err != nil {
			s.log.WithError(err).Warn("Indexer stop error")
		}
	}

	if s.indexStore != nil {
		if err := s.indexStore.Stop(); err != nil {
			s.log.WithError(err).Warn("Index store stop error")
		}
	}

	if s.store != nil {
		if err := s.store.Stop(); err != nil {
			return fmt.Errorf("stopping store: %w", err)
		}
	}

	s.log.Info("API server stopped")

	return nil
}

const defaultIndexingInterval = 60 * time.Second

// prepareIndexing creates the storage reader, index store, and indexer
// without starting the background goroutine. Call indexer.Start() separately
// after the HTTP server is listening.
func (s *server) prepareIndexing(ctx context.Context) error {
	// Create storage reader based on configured backend.
	switch {
	case s.cfg.Storage.S3 != nil && s.cfg.Storage.S3.Enabled:
		s.storageReader = storage.NewS3Reader(s.cfg.Storage.S3)
	case s.cfg.Storage.Local != nil && s.cfg.Storage.Local.Enabled:
		s.storageReader = storage.NewLocalReader(s.cfg.Storage.Local)
	default:
		return fmt.Errorf("no storage backend configured for indexing")
	}

	// Create and start the index store (DB connection + migrations).
	s.indexStore = indexstore.NewStore(
		s.log, &s.cfg.Indexing.Database,
	)

	if err := s.indexStore.Start(ctx); err != nil {
		return fmt.Errorf("starting index store: %w", err)
	}

	// Parse interval with default.
	interval := defaultIndexingInterval

	if s.cfg.Indexing.Interval != "" {
		d, err := time.ParseDuration(s.cfg.Indexing.Interval)
		if err != nil {
			return fmt.Errorf("parsing indexing interval: %w", err)
		}

		interval = d
	}

	// Create the indexer (not started yet).
	s.indexer = indexer.NewIndexer(
		s.log, s.indexStore, s.storageReader, interval,
	)

	s.log.Info("Indexing service enabled")

	return nil
}
