package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
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

// MkdirAll creates directory and sets ownership on all newly created
// directories, not just the leaf. This ensures intermediate directories
// are also chowned when the path contains multiple new segments.
func MkdirAll(path string, perm os.FileMode, owner *OwnerConfig) error {
	// Find the deepest existing ancestor before creating new directories.
	existing := path
	if owner != nil {
		for existing != "/" && existing != "." {
			if _, err := os.Stat(existing); err == nil {
				break
			}

			existing = filepath.Dir(existing)
		}
	}

	if err := os.MkdirAll(path, perm); err != nil {
		return err
	}

	// Chown all newly created directories (from leaf up to existing ancestor).
	for p := path; p != existing; p = filepath.Dir(p) {
		Chown(p, owner)
	}

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

// CopyDir recursively copies the directory tree rooted at src into dst,
// creating dst (and any missing parents) and applying owner to every directory
// and file it writes. Only regular files and directories are copied; symlinks
// and other special files are skipped. Intended for small auxiliary trees.
func CopyDir(src, dst string, owner *OwnerConfig) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("reading source dir %q: %w", src, err)
	}

	if err := MkdirAll(dst, 0755, owner); err != nil {
		return fmt.Errorf("creating dest dir %q: %w", dst, err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		switch {
		case entry.IsDir():
			if err := CopyDir(srcPath, dstPath, owner); err != nil {
				return err
			}
		case entry.Type().IsRegular():
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return fmt.Errorf("reading %q: %w", srcPath, err)
			}

			if err := WriteFile(dstPath, data, 0644, owner); err != nil {
				return fmt.Errorf("writing %q: %w", dstPath, err)
			}
		}
	}

	return nil
}
