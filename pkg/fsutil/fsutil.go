package fsutil

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// OwnerConfig holds parsed UID/GID for file ownership.
type OwnerConfig struct {
	UID int
	GID int
}

// ParseOwner parses "UID:GID" string. Returns nil if empty.
func ParseOwner(owner string) (*OwnerConfig, error) {
	if owner == "" {
		return nil, nil
	}

	parts := strings.Split(owner, ":")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid format %q, expected UID:GID", owner)
	}

	uid, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid UID %q: %w", parts[0], err)
	}

	gid, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid GID %q: %w", parts[1], err)
	}

	return &OwnerConfig{UID: uid, GID: gid}, nil
}

// Chown sets ownership if owner is not nil. Best-effort, ignores errors.
func Chown(path string, owner *OwnerConfig) {
	if owner == nil {
		return
	}

	_ = os.Chown(path, owner.UID, owner.GID)
}

// MkdirAll creates directory and sets ownership.
func MkdirAll(path string, perm os.FileMode, owner *OwnerConfig) error {
	if err := os.MkdirAll(path, perm); err != nil {
		return err
	}

	Chown(path, owner)

	return nil
}

// WriteFile writes file and sets ownership.
func WriteFile(path string, data []byte, perm os.FileMode, owner *OwnerConfig) error {
	if err := os.WriteFile(path, data, perm); err != nil {
		return err
	}

	Chown(path, owner)

	return nil
}

// Create creates file and sets ownership.
func Create(path string, owner *OwnerConfig) (*os.File, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	Chown(path, owner)

	return f, nil
}
