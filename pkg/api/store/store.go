package store

import (
	"context"
	"fmt"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Store provides persistence for API resources.
type Store interface {
	Start(ctx context.Context) error
	Stop() error

	// User CRUD.
	GetUserByID(ctx context.Context, id uint) (*User, error)
	GetUserByUsername(ctx context.Context, username string) (*User, error)
	ListUsers(ctx context.Context) ([]User, error)
	CreateUser(ctx context.Context, user *User) error
	UpdateUser(ctx context.Context, user *User) error
	DeleteUser(ctx context.Context, id uint) error

	// Session CRUD.
	CreateSession(ctx context.Context, session *Session) error
	GetSessionByToken(ctx context.Context, token string) (*Session, error)
	ListSessions(ctx context.Context) ([]Session, error)
	UpdateSessionLastActive(ctx context.Context, id uint, t time.Time) error
	DeleteSession(ctx context.Context, token string) error
	DeleteSessionByID(ctx context.Context, id uint) error
	DeleteExpiredSessions(ctx context.Context) error

	// GitHub org mapping CRUD.
	ListGitHubOrgMappings(ctx context.Context) ([]GitHubOrgMapping, error)
	UpsertGitHubOrgMapping(ctx context.Context, m *GitHubOrgMapping) error
	DeleteGitHubOrgMapping(ctx context.Context, id uint) error

	// GitHub user mapping CRUD.
	ListGitHubUserMappings(ctx context.Context) ([]GitHubUserMapping, error)
	UpsertGitHubUserMapping(ctx context.Context, m *GitHubUserMapping) error
	DeleteGitHubUserMapping(ctx context.Context, id uint) error

	// Seeding from config.
	SeedUsers(ctx context.Context, users []config.BasicAuthUser) error
	SeedGitHubMappings(
		ctx context.Context,
		orgMappings map[string]string,
		userMappings map[string]string,
	) error
}

// Compile-time interface check.
var _ Store = (*store)(nil)

type store struct {
	log logrus.FieldLogger
	cfg *config.APIDatabaseConfig
	db  *gorm.DB
}

// NewStore creates a new Store backed by the configured database driver.
func NewStore(
	log logrus.FieldLogger,
	cfg *config.APIDatabaseConfig,
) Store {
	return &store{
		log: log.WithField("component", "store"),
		cfg: cfg,
	}
}

// Start opens the database connection and runs migrations.
func (s *store) Start(ctx context.Context) error {
	var (
		dialector gorm.Dialector
		err       error
	)

	gormCfg := &gorm.Config{
		Logger: logger.Discard,
	}

	switch s.cfg.Driver {
	case "sqlite":
		dialector = sqlite.Open(s.cfg.SQLite.Path)
	case "postgres":
		dsn := fmt.Sprintf(
			"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			s.cfg.Postgres.Host,
			s.cfg.Postgres.Port,
			s.cfg.Postgres.User,
			s.cfg.Postgres.Password,
			s.cfg.Postgres.Database,
			s.cfg.Postgres.SSLMode,
		)
		dialector = postgres.Open(dsn)
	default:
		return fmt.Errorf("unsupported database driver: %s", s.cfg.Driver)
	}

	s.db, err = gorm.Open(dialector, gormCfg)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}

	if err := s.db.WithContext(ctx).AutoMigrate(
		&User{},
		&Session{},
		&GitHubOrgMapping{},
		&GitHubUserMapping{},
	); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	s.log.WithField("driver", s.cfg.Driver).Info("Database connected")

	return nil
}

// Stop closes the underlying database connection.
func (s *store) Stop() error {
	if s.db == nil {
		return nil
	}

	sqlDB, err := s.db.DB()
	if err != nil {
		return fmt.Errorf("getting underlying db: %w", err)
	}

	return sqlDB.Close()
}

// --- User CRUD ---

func (s *store) GetUserByID(
	ctx context.Context, id uint,
) (*User, error) {
	var user User
	if err := s.db.WithContext(ctx).First(&user, id).Error; err != nil {
		return nil, fmt.Errorf("getting user by id: %w", err)
	}

	return &user, nil
}

func (s *store) GetUserByUsername(
	ctx context.Context, username string,
) (*User, error) {
	var user User
	if err := s.db.WithContext(ctx).
		Where("username = ?", username).
		First(&user).Error; err != nil {
		return nil, fmt.Errorf("getting user by username: %w", err)
	}

	return &user, nil
}

func (s *store) ListUsers(ctx context.Context) ([]User, error) {
	var users []User
	if err := s.db.WithContext(ctx).
		Order("id ASC").
		Find(&users).Error; err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}

	return users, nil
}

func (s *store) CreateUser(ctx context.Context, user *User) error {
	if err := s.db.WithContext(ctx).Create(user).Error; err != nil {
		return fmt.Errorf("creating user: %w", err)
	}

	return nil
}

func (s *store) UpdateUser(ctx context.Context, user *User) error {
	if err := s.db.WithContext(ctx).Save(user).Error; err != nil {
		return fmt.Errorf("updating user: %w", err)
	}

	return nil
}

func (s *store) DeleteUser(ctx context.Context, id uint) error {
	if err := s.db.WithContext(ctx).
		Delete(&User{}, id).Error; err != nil {
		return fmt.Errorf("deleting user: %w", err)
	}

	return nil
}

// --- Session CRUD ---

func (s *store) CreateSession(
	ctx context.Context, session *Session,
) error {
	if err := s.db.WithContext(ctx).Create(session).Error; err != nil {
		return fmt.Errorf("creating session: %w", err)
	}

	return nil
}

func (s *store) GetSessionByToken(
	ctx context.Context, token string,
) (*Session, error) {
	var session Session
	if err := s.db.WithContext(ctx).
		Where("token = ?", token).
		First(&session).Error; err != nil {
		return nil, fmt.Errorf("getting session by token: %w", err)
	}

	return &session, nil
}

func (s *store) ListSessions(ctx context.Context) ([]Session, error) {
	var sessions []Session
	if err := s.db.WithContext(ctx).
		Order("id ASC").
		Find(&sessions).Error; err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}

	return sessions, nil
}

func (s *store) UpdateSessionLastActive(
	ctx context.Context, id uint, t time.Time,
) error {
	if err := s.db.WithContext(ctx).
		Model(&Session{}).
		Where("id = ?", id).
		Update("last_active_at", t).Error; err != nil {
		return fmt.Errorf("updating session last active: %w", err)
	}

	return nil
}

func (s *store) DeleteSession(ctx context.Context, token string) error {
	if err := s.db.WithContext(ctx).
		Where("token = ?", token).
		Delete(&Session{}).Error; err != nil {
		return fmt.Errorf("deleting session: %w", err)
	}

	return nil
}

func (s *store) DeleteSessionByID(ctx context.Context, id uint) error {
	if err := s.db.WithContext(ctx).
		Delete(&Session{}, id).Error; err != nil {
		return fmt.Errorf("deleting session by id: %w", err)
	}

	return nil
}

func (s *store) DeleteExpiredSessions(ctx context.Context) error {
	result := s.db.WithContext(ctx).
		Where("expires_at < ?", time.Now().UTC()).
		Delete(&Session{})
	if result.Error != nil {
		return fmt.Errorf("deleting expired sessions: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		s.log.WithField("count", result.RowsAffected).
			Debug("Cleaned up expired sessions")
	}

	return nil
}

// --- GitHub mapping CRUD ---

func (s *store) ListGitHubOrgMappings(
	ctx context.Context,
) ([]GitHubOrgMapping, error) {
	var mappings []GitHubOrgMapping
	if err := s.db.WithContext(ctx).
		Order("id ASC").
		Find(&mappings).Error; err != nil {
		return nil, fmt.Errorf("listing github org mappings: %w", err)
	}

	return mappings, nil
}

func (s *store) UpsertGitHubOrgMapping(
	ctx context.Context, m *GitHubOrgMapping,
) error {
	result := s.db.WithContext(ctx).
		Where("org = ?", m.Org).
		Assign(GitHubOrgMapping{Role: m.Role}).
		FirstOrCreate(m)
	if result.Error != nil {
		return fmt.Errorf("upserting github org mapping: %w", result.Error)
	}

	return nil
}

func (s *store) DeleteGitHubOrgMapping(
	ctx context.Context, id uint,
) error {
	if err := s.db.WithContext(ctx).
		Delete(&GitHubOrgMapping{}, id).Error; err != nil {
		return fmt.Errorf("deleting github org mapping: %w", err)
	}

	return nil
}

func (s *store) ListGitHubUserMappings(
	ctx context.Context,
) ([]GitHubUserMapping, error) {
	var mappings []GitHubUserMapping
	if err := s.db.WithContext(ctx).
		Order("id ASC").
		Find(&mappings).Error; err != nil {
		return nil, fmt.Errorf("listing github user mappings: %w", err)
	}

	return mappings, nil
}

func (s *store) UpsertGitHubUserMapping(
	ctx context.Context, m *GitHubUserMapping,
) error {
	result := s.db.WithContext(ctx).
		Where("username = ?", m.Username).
		Assign(GitHubUserMapping{Role: m.Role}).
		FirstOrCreate(m)
	if result.Error != nil {
		return fmt.Errorf("upserting github user mapping: %w", result.Error)
	}

	return nil
}

func (s *store) DeleteGitHubUserMapping(
	ctx context.Context, id uint,
) error {
	if err := s.db.WithContext(ctx).
		Delete(&GitHubUserMapping{}, id).Error; err != nil {
		return fmt.Errorf("deleting github user mapping: %w", err)
	}

	return nil
}

// --- Seeding ---

// SeedUsers upserts config-sourced users. Only users with source="config"
// are updated; users created by admins or via GitHub are preserved.
func (s *store) SeedUsers(
	ctx context.Context, users []config.BasicAuthUser,
) error {
	for _, u := range users {
		hash, err := bcrypt.GenerateFromPassword(
			[]byte(u.Password), bcrypt.DefaultCost,
		)
		if err != nil {
			return fmt.Errorf("hashing password for %q: %w", u.Username, err)
		}

		var existing User

		result := s.db.WithContext(ctx).
			Where("username = ? AND source = ?", u.Username, SourceConfig).
			First(&existing)

		if result.Error == nil {
			// Update existing config user.
			existing.PasswordHash = string(hash)
			existing.Role = u.Role

			if err := s.db.WithContext(ctx).Save(&existing).Error; err != nil {
				return fmt.Errorf("updating config user %q: %w", u.Username, err)
			}
		} else {
			// Create new config user (only if username not taken).
			newUser := User{
				Username:     u.Username,
				PasswordHash: string(hash),
				Role:         u.Role,
				Source:       SourceConfig,
			}

			if err := s.db.WithContext(ctx).
				Where("username = ?", u.Username).
				FirstOrCreate(&newUser).Error; err != nil {
				return fmt.Errorf("seeding config user %q: %w", u.Username, err)
			}
		}
	}

	s.log.WithField("count", len(users)).
		Info("Seeded users from config")

	return nil
}

// SeedGitHubMappings upserts GitHub org and user role mappings from config.
func (s *store) SeedGitHubMappings(
	ctx context.Context,
	orgMappings map[string]string,
	userMappings map[string]string,
) error {
	for org, role := range orgMappings {
		m := &GitHubOrgMapping{Org: org, Role: role}
		if err := s.UpsertGitHubOrgMapping(ctx, m); err != nil {
			return err
		}
	}

	for username, role := range userMappings {
		m := &GitHubUserMapping{Username: username, Role: role}
		if err := s.UpsertGitHubUserMapping(ctx, m); err != nil {
			return err
		}
	}

	total := len(orgMappings) + len(userMappings)
	if total > 0 {
		s.log.WithField("count", total).
			Info("Seeded GitHub mappings from config")
	}

	return nil
}
