// Package gitrepo clones git repositories into an on-disk cache and keeps the
// cached checkout at a requested branch, tag, or commit. It is the shared
// implementation behind the test-source git clones and the EEST fill-repo
// clone, so recurring clones of the same repo are avoided.
package gitrepo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

// CloneOrUpdate ensures repo is cloned under cacheDir and checked out at
// version (a branch, tag, or commit hash), reusing an existing cached clone
// when present. It returns the local path of the checkout. The cache key is
// derived from the repo URL, so different versions of the same repo share one
// directory (the checkout is moved to the requested version).
func CloneOrUpdate(ctx context.Context, log logrus.FieldLogger, repo, version, cacheDir string) (string, error) {
	localPath := filepath.Join(cacheDir, hashRepoURL(repo))

	log = log.WithFields(logrus.Fields{
		"repo":    repo,
		"version": version,
		"path":    localPath,
	})

	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		log.Info("Cloning repository")

		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			return "", fmt.Errorf("creating cache directory: %w", err)
		}

		if looksLikeCommitHash(version) {
			// Commit hashes can't be used with --branch, so init + fetch.
			if err := cloneByCommitHash(ctx, repo, version, localPath); err != nil {
				return "", err
			}
		} else if err := gitRun(ctx, "git", "clone",
			"--depth=1", "--branch", version, "--single-branch", repo, localPath); err != nil {
			return "", fmt.Errorf("cloning repository: %w", err)
		}

		return localPath, nil
	}

	// Cached clone exists. For commit hashes, skip the fetch when HEAD already
	// matches the requested commit.
	if looksLikeCommitHash(version) {
		if sha, err := HeadSHA(ctx, localPath); err == nil && strings.HasPrefix(sha, version) {
			log.Info("Cached repository already at requested version")

			return localPath, nil
		}
	}

	log.Info("Updating cached repository")

	if err := gitRun(ctx, "git", "-C", localPath, "fetch", "--depth=1", "origin", version); err != nil {
		return "", fmt.Errorf("fetching version: %w", err)
	}

	if err := gitRun(ctx, "git", "-C", localPath, "checkout", "FETCH_HEAD"); err != nil {
		return "", fmt.Errorf("checking out version: %w", err)
	}

	return localPath, nil
}

// HeadSHA returns the current HEAD commit hash for the repository at repoPath.
func HeadSHA(ctx context.Context, repoPath string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("getting commit SHA: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

// cloneByCommitHash initializes a repo and fetches a specific commit hash, then
// checks it out (a commit hash cannot be passed to `git clone --branch`).
func cloneByCommitHash(ctx context.Context, repo, version, localPath string) error {
	if err := gitRun(ctx, "git", "init", localPath); err != nil {
		return fmt.Errorf("initializing repository: %w", err)
	}

	if err := gitRun(ctx, "git", "-C", localPath, "remote", "add", "origin", repo); err != nil {
		return fmt.Errorf("adding remote: %w", err)
	}

	if err := gitRun(ctx, "git", "-C", localPath, "fetch", "--depth=1", "origin", version); err != nil {
		return fmt.Errorf("fetching commit %s: %w", version, err)
	}

	if err := gitRun(ctx, "git", "-C", localPath, "checkout", "FETCH_HEAD"); err != nil {
		return fmt.Errorf("checking out commit %s: %w", version, err)
	}

	return nil
}

// gitRun runs a git command, forwarding its output to the process stdio.
func gitRun(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// looksLikeCommitHash returns true if s looks like a git commit hash
// (7-40 hex characters).
func looksLikeCommitHash(s string) bool {
	if len(s) < 7 || len(s) > 40 {
		return false
	}

	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}

	return true
}

// hashRepoURL creates a short stable hash of the repository URL for caching.
func hashRepoURL(url string) string {
	hash := sha256.Sum256([]byte(url))

	return hex.EncodeToString(hash[:8])
}
