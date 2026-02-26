package store

import (
	"time"
)

// User source constants.
const (
	SourceConfig = "config"
	SourceAdmin  = "admin"
	SourceGitHub = "github"
)

// User represents an authenticated user in the system.
type User struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"uniqueIndex;not null" json:"username"`
	PasswordHash string    `gorm:"not null" json:"-"`
	Role         string    `gorm:"not null" json:"role"`
	Source       string    `gorm:"not null" json:"source"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Session represents an active user session.
type Session struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	Token        string     `gorm:"uniqueIndex;not null" json:"-"`
	UserID       uint       `gorm:"not null" json:"user_id"`
	ExpiresAt    time.Time  `gorm:"not null" json:"expires_at"`
	CreatedAt    time.Time  `json:"created_at"`
	LastActiveAt *time.Time `json:"last_active_at"`
}

// APIKey represents a bearer token for programmatic API access.
type APIKey struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	Name       string     `gorm:"not null" json:"name"`
	KeyHash    string     `gorm:"uniqueIndex;not null" json:"-"`
	KeyPrefix  string     `gorm:"not null" json:"key_prefix"`
	UserID     uint       `gorm:"not null" json:"user_id"`
	ExpiresAt  *time.Time `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// GitHubOrgMapping maps a GitHub organization to a role.
type GitHubOrgMapping struct {
	ID   uint   `gorm:"primaryKey" json:"id"`
	Org  string `gorm:"uniqueIndex;not null" json:"org"`
	Role string `gorm:"not null" json:"role"`
}

// GitHubUserMapping maps a GitHub username to a role.
type GitHubUserMapping struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	Username string `gorm:"uniqueIndex;not null" json:"username"`
	Role     string `gorm:"not null" json:"role"`
}
