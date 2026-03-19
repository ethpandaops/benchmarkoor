package generate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

// ExecuteRemoteOpts contains options for running execute remote.
type ExecuteRemoteOpts struct {
	Fork               string
	RPCEndpoint        string
	SeedKey            string
	ChainID            int
	GasBenchmarkValues string
	TestPath           string
	EESTMode           string
	ParameterFilter    string
	AddressStubs       string
	ExtraPytestArgs    []string
}

// PythonEnv manages the execution-specs Python environment.
type PythonEnv struct {
	log      logrus.FieldLogger
	specsDir string
}

// NewPythonEnv creates a new Python environment manager.
func NewPythonEnv(log logrus.FieldLogger) *PythonEnv {
	return &PythonEnv{
		log: log,
	}
}

// SpecsDir returns the path to the execution-specs directory.
func (p *PythonEnv) SpecsDir() string {
	return p.specsDir
}

// Setup prepares the execution-specs environment.
// If localPath is set, uses it directly. Otherwise clones the repo.
func (p *PythonEnv) Setup(
	ctx context.Context,
	repo, branch, commit, localPath, cacheDir string,
) error {
	// Check uv is available.
	if _, err := exec.LookPath("uv"); err != nil {
		return fmt.Errorf("uv not found in PATH: install it from https://docs.astral.sh/uv/")
	}

	if localPath != "" {
		p.specsDir = localPath
		p.log.WithField("path", localPath).Info("Using local execution-specs")
	} else {
		// Clone to cache dir.
		specsDir := filepath.Join(cacheDir, "execution-specs")
		p.specsDir = specsDir

		if err := p.cloneOrUpdate(ctx, repo, branch, commit, specsDir); err != nil {
			return fmt.Errorf("preparing execution-specs: %w", err)
		}
	}

	// Run uv sync.
	p.log.Info("Running uv sync")

	cmd := exec.CommandContext(ctx, "uv", "sync", "--all-extras")
	cmd.Dir = p.specsDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("uv sync failed: %w", err)
	}

	return nil
}

// RunExecuteRemote runs the EEST execute remote command.
func (p *PythonEnv) RunExecuteRemote(ctx context.Context, opts *ExecuteRemoteOpts) error {
	args := []string{
		"run", "execute", "remote",
		"--fork=" + opts.Fork,
		"--rpc-endpoint=" + opts.RPCEndpoint,
	}

	if opts.SeedKey != "" {
		args = append(args, "--rpc-seed-key="+opts.SeedKey)
	}

	if opts.ChainID > 0 {
		args = append(args, fmt.Sprintf("--rpc-chain-id=%d", opts.ChainID))
	}

	if opts.GasBenchmarkValues != "" {
		args = append(args, "--gas-benchmark-values="+opts.GasBenchmarkValues)
	}

	if opts.AddressStubs != "" {
		args = append(args, "--address-stubs="+opts.AddressStubs)
	}

	// Add test path.
	if opts.TestPath != "" {
		args = append(args, opts.TestPath)
	}

	// Add pytest args after --.
	if opts.EESTMode != "" || opts.ParameterFilter != "" || len(opts.ExtraPytestArgs) > 0 {
		args = append(args, "--")

		if opts.EESTMode != "" {
			args = append(args, "-m", opts.EESTMode)
		}

		if opts.ParameterFilter != "" {
			args = append(args, "-k", opts.ParameterFilter)
		}

		args = append(args, opts.ExtraPytestArgs...)
	}

	p.log.WithField("args", strings.Join(args, " ")).Info("Running execute remote")

	cmd := exec.CommandContext(ctx, "uv", args...)
	cmd.Dir = p.specsDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("execute remote failed: %w", err)
	}

	return nil
}

// cloneOrUpdate clones the repo if not present, or updates it.
func (p *PythonEnv) cloneOrUpdate(
	ctx context.Context,
	repo, branch, commit, targetDir string,
) error {
	if _, err := os.Stat(filepath.Join(targetDir, ".git")); err == nil {
		// Already cloned, fetch and checkout.
		p.log.WithField("dir", targetDir).Info("Updating existing execution-specs clone")

		if err := p.runGit(ctx, targetDir, "fetch", "origin"); err != nil {
			return fmt.Errorf("git fetch: %w", err)
		}

		ref := commit
		if ref == "" {
			ref = "origin/" + branch
		}

		if err := p.runGit(ctx, targetDir, "checkout", ref); err != nil {
			return fmt.Errorf("git checkout: %w", err)
		}

		return nil
	}

	// Clone fresh.
	p.log.WithFields(logrus.Fields{
		"repo":   repo,
		"branch": branch,
	}).Info("Cloning execution-specs")

	args := []string{"clone", "--branch", branch, "--depth", "1", repo, targetDir}
	if commit != "" {
		// Need full clone for specific commit.
		args = []string{"clone", "--branch", branch, repo, targetDir}
	}

	if err := p.runGit(ctx, "", args...); err != nil {
		return fmt.Errorf("git clone: %w", err)
	}

	if commit != "" {
		if err := p.runGit(ctx, targetDir, "checkout", commit); err != nil {
			return fmt.Errorf("git checkout commit: %w", err)
		}
	}

	return nil
}

// runGit runs a git command.
func (p *PythonEnv) runGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)

	if dir != "" {
		cmd.Dir = dir
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
