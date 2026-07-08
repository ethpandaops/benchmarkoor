package executor

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/eest"
	"github.com/sirupsen/logrus"
)

// EESTSource provides tests from EEST fixtures in GitHub releases or artifacts.
type EESTSource struct {
	log           logrus.FieldLogger
	cfg           *config.EESTFixturesSource
	cacheDir      string
	filter        *filterMatcher
	githubToken   string
	fixturesDir   string
	genesisDir    string
	tests         []*TestWithSteps
	genesisGroups []*GenesisGroup
	// resolvedFixturesRunID and resolvedGenesisRunID store the actual run IDs
	// used when downloading artifacts. When the config doesn't specify a run ID,
	// these capture the latest run ID that was resolved during download.
	resolvedFixturesRunID string
	resolvedGenesisRunID  string
}

// preAllocFile represents the JSON structure of a pre_alloc file.
type preAllocFile struct {
	TestIDs []string `json:"testIds"`
}

// NewEESTSource creates a new EEST source.
func NewEESTSource(log logrus.FieldLogger, cfg *config.EESTFixturesSource, cacheDir string, filter *filterMatcher, githubToken string) *EESTSource {
	return &EESTSource{
		log:         log.WithField("source", "eest"),
		cfg:         cfg,
		cacheDir:    cacheDir,
		filter:      filter,
		githubToken: githubToken,
	}
}

// Prepare downloads and extracts fixtures from GitHub releases, artifacts,
// or resolves them from local directories/tarballs.
func (s *EESTSource) Prepare(ctx context.Context) (*PreparedSource, error) {
	// Handle local directory mode — no downloading or caching needed.
	if s.cfg.UseLocalDir() {
		s.fixturesDir = s.cfg.LocalFixturesDir
		s.genesisDir = s.cfg.LocalGenesisDir

		s.log.WithFields(logrus.Fields{
			"fixtures_dir": s.fixturesDir,
			"genesis_dir":  s.genesisDir,
		}).Info("Using local EEST fixtures directories")

		return s.discoverTests()
	}

	// Handle local tarball mode — extract to cache.
	if s.cfg.UseLocalTarball() {
		return s.prepareLocalTarballs()
	}

	// Handle standalone URL mode — a release/plain .tar.gz URL or a GitHub
	// Actions artifact URL. Downloads + extracts to cache; no genesis.
	if s.cfg.UseFixturesURL() {
		return s.prepareFromURL(ctx)
	}

	// Build cache path based on source type.
	repoHash := hashRepoURL(s.cfg.GitHubRepo)

	var cacheBase string

	if s.cfg.UseArtifacts() {
		// GitHub token is required for all artifact operations.
		if s.githubToken == "" {
			return nil, fmt.Errorf(
				"GitHub token is required for artifact downloads. " +
					"Set runner.github_token in config or BENCHMARKOOR_RUNNER_GITHUB_TOKEN env var",
			)
		}

		// Resolve run IDs upfront so the cache key always includes a run ID.
		fixturesArtifact := s.cfg.FixturesArtifactName
		if fixturesArtifact == "" {
			fixturesArtifact = "fixtures_benchmark"
		}

		if s.cfg.FixturesArtifactRunID != "" {
			s.resolvedFixturesRunID = s.cfg.FixturesArtifactRunID
		} else {
			runID, err := s.resolveArtifactRunID(ctx, fixturesArtifact)
			if err != nil {
				return nil, fmt.Errorf("resolving fixtures artifact run ID: %w", err)
			}

			s.resolvedFixturesRunID = runID

			s.log.WithField("run_id", runID).Info("Resolved latest fixtures artifact run ID")
		}

		genesisArtifact := s.cfg.GenesisArtifactName
		if genesisArtifact == "" {
			genesisArtifact = "benchmark_genesis"
		}

		if s.cfg.GenesisArtifactRunID != "" {
			s.resolvedGenesisRunID = s.cfg.GenesisArtifactRunID
		} else {
			runID, err := s.resolveArtifactRunID(ctx, genesisArtifact)
			if err != nil {
				return nil, fmt.Errorf("resolving genesis artifact run ID: %w", err)
			}

			s.resolvedGenesisRunID = runID

			s.log.WithField("run_id", runID).Info("Resolved latest genesis artifact run ID")
		}

		artifactKey := fmt.Sprintf("%s-%s", fixturesArtifact, s.resolvedFixturesRunID)

		cacheBase = filepath.Join(s.cacheDir, "eest-artifacts", repoHash, artifactKey)
	} else {
		// For releases, use the release tag.
		cacheBase = filepath.Join(s.cacheDir, "eest", repoHash, s.cfg.GitHubRelease)
	}

	s.fixturesDir = filepath.Join(cacheBase, "fixtures")
	s.genesisDir = filepath.Join(cacheBase, "genesis")

	// Fixtures and genesis are extracted in two steps. The existence of either
	// directory does not mean the cache is complete, so gate reuse on a marker
	// written only after both steps succeed. Without it, a run interrupted
	// between the two steps would be treated as cached and reused forever with
	// genesis missing.
	completeMarker := filepath.Join(cacheBase, ".complete")

	if _, err := os.Stat(completeMarker); err == nil {
		s.log.WithField("path", cacheBase).Info("Using cached EEST fixtures")

		return s.discoverTests()
	}

	// Clear any partial cache left by an earlier interrupted run before
	// re-downloading.
	if err := os.RemoveAll(s.fixturesDir); err != nil {
		return nil, fmt.Errorf("clearing partial fixtures cache: %w", err)
	}

	if err := os.RemoveAll(s.genesisDir); err != nil {
		return nil, fmt.Errorf("clearing partial genesis cache: %w", err)
	}

	if s.cfg.UseArtifacts() {
		s.log.Info("Downloading EEST fixtures from GitHub artifacts")

		if err := s.downloadArtifacts(ctx, cacheBase); err != nil {
			return nil, fmt.Errorf("downloading artifacts: %w", err)
		}
	} else {
		s.log.Info("Downloading EEST fixtures from GitHub release")

		if err := s.downloadAndExtract(ctx, cacheBase); err != nil {
			return nil, fmt.Errorf("downloading fixtures: %w", err)
		}
	}

	if err := os.WriteFile(completeMarker, nil, 0o644); err != nil {
		return nil, fmt.Errorf("writing cache completion marker: %w", err)
	}

	// Parse fixtures and build tests.
	return s.discoverTests()
}

// downloadAndExtract downloads and extracts the fixtures and genesis tarballs.
func (s *EESTSource) downloadAndExtract(ctx context.Context, cacheBase string) error {
	if err := os.MkdirAll(cacheBase, 0755); err != nil {
		return fmt.Errorf("creating cache directory: %w", err)
	}

	// Build download URLs.
	fixturesURL := s.cfg.FixturesURL
	if fixturesURL == "" {
		fixturesURL = fmt.Sprintf(
			"https://github.com/%s/releases/download/%s/fixtures_benchmark.tar.gz",
			s.cfg.GitHubRepo, s.cfg.GitHubRelease,
		)
	}

	genesisURL := s.cfg.GenesisURL
	if genesisURL == "" {
		genesisURL = fmt.Sprintf(
			"https://github.com/%s/releases/download/%s/benchmark_genesis.tar.gz",
			s.cfg.GitHubRepo, s.cfg.GitHubRelease,
		)
	}

	// Download and extract fixtures.
	s.log.WithField("url", fixturesURL).Info("Downloading fixtures tarball")

	if err := s.downloadAndExtractTarball(ctx, fixturesURL, s.fixturesDir); err != nil {
		return fmt.Errorf("extracting fixtures: %w", err)
	}

	// Download and extract genesis.
	s.log.WithField("url", genesisURL).Info("Downloading genesis tarball")

	if err := s.downloadAndExtractTarball(ctx, genesisURL, s.genesisDir); err != nil {
		return fmt.Errorf("extracting genesis: %w", err)
	}

	return nil
}

// downloadArtifacts downloads fixtures and genesis from GitHub Actions artifacts.
func (s *EESTSource) downloadArtifacts(ctx context.Context, cacheBase string) error {
	if err := os.MkdirAll(cacheBase, 0755); err != nil {
		return fmt.Errorf("creating cache directory: %w", err)
	}

	// Download fixtures artifact.
	fixturesArtifact := s.cfg.FixturesArtifactName
	if fixturesArtifact == "" {
		fixturesArtifact = "fixtures_benchmark"
	}

	s.log.WithFields(logrus.Fields{
		"artifact": fixturesArtifact,
		"repo":     s.cfg.GitHubRepo,
		"run_id":   s.resolvedFixturesRunID,
	}).Info("Downloading fixtures artifact")

	if _, err := s.downloadGitHubArtifact(ctx, fixturesArtifact, s.resolvedFixturesRunID, s.fixturesDir); err != nil {
		return fmt.Errorf("downloading fixtures artifact: %w", err)
	}

	// Extract any .tar.gz files found inside the artifact.
	if err := s.extractInnerTarballs(ctx, s.fixturesDir); err != nil {
		return fmt.Errorf("extracting fixtures tarballs: %w", err)
	}

	// Download genesis artifact.
	genesisArtifact := s.cfg.GenesisArtifactName
	if genesisArtifact == "" {
		genesisArtifact = "benchmark_genesis"
	}

	s.log.WithFields(logrus.Fields{
		"artifact": genesisArtifact,
		"repo":     s.cfg.GitHubRepo,
		"run_id":   s.resolvedGenesisRunID,
	}).Info("Downloading genesis artifact")

	if _, err := s.downloadGitHubArtifact(ctx, genesisArtifact, s.resolvedGenesisRunID, s.genesisDir); err != nil {
		return fmt.Errorf("downloading genesis artifact: %w", err)
	}

	// Extract any .tar.gz files found inside the artifact.
	if err := s.extractInnerTarballs(ctx, s.genesisDir); err != nil {
		return fmt.Errorf("extracting genesis tarballs: %w", err)
	}

	return nil
}

// prepareFromURL downloads + extracts fixtures from a standalone URL: a
// release/plain .tar.gz URL or a GitHub Actions artifact URL. No genesis is
// fetched (stateful-engine fixtures boot from the snapshot datadir).
func (s *EESTSource) prepareFromURL(ctx context.Context) (*PreparedSource, error) {
	cacheBase := filepath.Join(s.cacheDir, "eest-url", hashRepoURL(s.cfg.FixturesURL))
	s.fixturesDir = filepath.Join(cacheBase, "fixtures")
	s.genesisDir = filepath.Join(cacheBase, "genesis")

	completeMarker := filepath.Join(cacheBase, ".complete")
	if _, err := os.Stat(completeMarker); err == nil {
		s.log.WithField("path", cacheBase).Info("Using cached EEST fixtures")

		return s.discoverTests()
	}

	if err := os.RemoveAll(s.fixturesDir); err != nil {
		return nil, fmt.Errorf("clearing partial fixtures cache: %w", err)
	}

	if err := s.downloadFromURL(ctx); err != nil {
		return nil, fmt.Errorf("downloading fixtures from URL: %w", err)
	}

	if err := os.WriteFile(completeMarker, nil, 0o644); err != nil {
		return nil, fmt.Errorf("writing cache completion marker: %w", err)
	}

	return s.discoverTests()
}

// downloadFromURL fetches fixtures from s.cfg.FixturesURL into s.fixturesDir. A
// GitHub Actions artifact URL is downloaded via the API (needs a token) and its
// inner .tar.gz extracted; any other URL is treated as a direct .tar.gz.
func (s *EESTSource) downloadFromURL(ctx context.Context) error {
	if err := os.MkdirAll(s.fixturesDir, 0o755); err != nil {
		return fmt.Errorf("creating fixtures directory: %w", err)
	}

	url := s.cfg.FixturesURL

	if owner, repo, artifactID, ok := parseGitHubArtifactURL(url); ok {
		s.log.WithFields(logrus.Fields{
			"owner": owner, "repo": repo, "artifact_id": artifactID,
		}).Info("Downloading fixtures from GitHub Actions artifact URL")

		if err := s.downloadArtifactZip(ctx, owner+"/"+repo, artifactID, s.fixturesDir); err != nil {
			return fmt.Errorf("downloading artifact: %w", err)
		}

		// Build artifacts wrap the fixtures in an inner .tar.gz.
		if err := s.extractInnerTarballs(ctx, s.fixturesDir); err != nil {
			return fmt.Errorf("extracting inner tarballs: %w", err)
		}

		return nil
	}

	s.log.WithField("url", url).Info("Downloading fixtures tarball from URL")

	if err := s.downloadAndExtractTarball(ctx, url, s.fixturesDir); err != nil {
		return fmt.Errorf("downloading fixtures tarball: %w", err)
	}

	return nil
}

// ghArtifactURLRe matches a GitHub Actions artifact web URL, capturing owner,
// repo, and the numeric artifact id:
// https://github.com/<owner>/<repo>/actions/runs/<run>/artifacts/<id>
var ghArtifactURLRe = regexp.MustCompile(
	`^https://github\.com/([^/]+)/([^/]+)/actions/runs/\d+/artifacts/(\d+)`,
)

// parseGitHubArtifactURL returns the owner, repo, and artifact id for a GitHub
// Actions artifact URL, or ok=false for any other URL.
func parseGitHubArtifactURL(rawURL string) (owner, repo, artifactID string, ok bool) {
	m := ghArtifactURLRe.FindStringSubmatch(rawURL)
	if m == nil {
		return "", "", "", false
	}

	return m[1], m[2], m[3], true
}

// downloadArtifactZip downloads a GitHub Actions artifact zip (apiRepo is
// "owner/repo", artifactID is the numeric id) to targetDir and extracts it.
// Requires a GitHub token.
func (s *EESTSource) downloadArtifactZip(ctx context.Context, apiRepo, artifactID, targetDir string) error {
	if s.githubToken == "" {
		return fmt.Errorf(
			"GitHub token is required to download a GitHub Actions artifact. " +
				"Set runner.github_token or BENCHMARKOOR_RUNNER_GITHUB_TOKEN",
		)
	}

	downloadURL := fmt.Sprintf(
		"https://api.github.com/repos/%s/actions/artifacts/%s/zip",
		apiRepo, artifactID,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("creating artifact download request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.githubToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading artifact: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		return fmt.Errorf("downloading artifact: HTTP %d: %s", resp.StatusCode, string(body))
	}

	tmpFile, err := os.CreateTemp("", "gh-artifact-*.zip")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}

	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
	}()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return fmt.Errorf("writing artifact zip: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}

	return s.extractZip(tmpFile.Name(), targetDir)
}

// prepareLocalTarballs extracts local .tar.gz fixtures and genesis to a cache directory.
func (s *EESTSource) prepareLocalTarballs() (*PreparedSource, error) {
	// Build a cache key from the tarball paths so re-extractions can be skipped.
	cacheKey := hashRepoURL(s.cfg.LocalFixturesTarball + "|" + s.cfg.LocalGenesisTarball)
	cacheBase := filepath.Join(s.cacheDir, "eest-local", cacheKey)

	s.fixturesDir = filepath.Join(cacheBase, "fixtures")
	s.genesisDir = filepath.Join(cacheBase, "genesis")

	// Check if already extracted.
	if _, err := os.Stat(s.fixturesDir); os.IsNotExist(err) {
		s.log.WithFields(logrus.Fields{
			"fixtures_tarball": s.cfg.LocalFixturesTarball,
			"genesis_tarball":  s.cfg.LocalGenesisTarball,
			"cache":            cacheBase,
		}).Info("Extracting local EEST tarballs")

		if err := os.MkdirAll(cacheBase, 0755); err != nil {
			return nil, fmt.Errorf("creating cache directory: %w", err)
		}

		if err := s.extractLocalTarball(s.cfg.LocalFixturesTarball, s.fixturesDir); err != nil {
			return nil, fmt.Errorf("extracting fixtures tarball: %w", err)
		}

		if err := s.extractLocalTarball(s.cfg.LocalGenesisTarball, s.genesisDir); err != nil {
			return nil, fmt.Errorf("extracting genesis tarball: %w", err)
		}
	} else {
		s.log.WithField("path", cacheBase).Info("Using cached local EEST tarballs")
	}

	return s.discoverTests()
}

// ghArtifactList represents a GitHub API response listing artifacts.
type ghArtifactList struct {
	Artifacts []ghArtifact `json:"artifacts"`
}

// ghArtifact represents a single GitHub Actions artifact.
type ghArtifact struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	WorkflowRun *ghRunRef `json:"workflow_run,omitempty"`
}

// ghRunRef is a minimal reference to a workflow run inside an artifact response.
type ghRunRef struct {
	ID int64 `json:"id"`
}

// resolveArtifactRunID queries the GitHub API for the latest artifact with the
// given name and returns its workflow run ID.
func (s *EESTSource) resolveArtifactRunID(ctx context.Context, artifactName string) (string, error) {
	listURL := fmt.Sprintf(
		"https://api.github.com/repos/%s/actions/artifacts?name=%s&per_page=1",
		s.cfg.GitHubRepo, artifactName,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating artifact list request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.githubToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("listing artifacts: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		return "", fmt.Errorf("listing artifacts: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var list ghArtifactList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return "", fmt.Errorf("decoding artifact list: %w", err)
	}

	if len(list.Artifacts) == 0 {
		return "", fmt.Errorf("no artifacts found for %q in %s", artifactName, s.cfg.GitHubRepo)
	}

	a := list.Artifacts[0]
	if a.WorkflowRun == nil {
		return "", fmt.Errorf("artifact %q has no workflow_run metadata", artifactName)
	}

	return fmt.Sprintf("%d", a.WorkflowRun.ID), nil
}

// downloadGitHubArtifact downloads an artifact using the GitHub REST API.
// It returns the workflow run ID that the artifact belongs to.
func (s *EESTSource) downloadGitHubArtifact(ctx context.Context, artifactName, runID, targetDir string) (string, error) {
	s.log.WithFields(logrus.Fields{
		"artifact": artifactName,
		"repo":     s.cfg.GitHubRepo,
	}).Info("Downloading artifact via GitHub API")

	// Find the artifact ID.
	var listURL string
	if runID != "" {
		listURL = fmt.Sprintf(
			"https://api.github.com/repos/%s/actions/runs/%s/artifacts",
			s.cfg.GitHubRepo, runID,
		)
	} else {
		listURL = fmt.Sprintf(
			"https://api.github.com/repos/%s/actions/artifacts?name=%s",
			s.cfg.GitHubRepo, artifactName,
		)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating artifact list request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.githubToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("listing artifacts: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("listing artifacts: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var artifactList ghArtifactList
	if err := json.NewDecoder(resp.Body).Decode(&artifactList); err != nil {
		return "", fmt.Errorf("decoding artifact list: %w", err)
	}

	// Find matching artifact.
	var matched *ghArtifact

	for i, a := range artifactList.Artifacts {
		if a.Name == artifactName {
			matched = &artifactList.Artifacts[i]

			break
		}
	}

	if matched == nil {
		return "", fmt.Errorf("artifact %q not found in repository %s", artifactName, s.cfg.GitHubRepo)
	}

	// Extract the resolved run ID from the artifact metadata.
	resolvedRunID := runID
	if resolvedRunID == "" && matched.WorkflowRun != nil {
		resolvedRunID = fmt.Sprintf("%d", matched.WorkflowRun.ID)
	}

	// Download + extract the artifact zip by its resolved numeric id.
	if err := s.downloadArtifactZip(ctx, s.cfg.GitHubRepo, fmt.Sprintf("%d", matched.ID), targetDir); err != nil {
		return "", err
	}

	return resolvedRunID, nil
}

// extractZip extracts a zip archive to the target directory.
func (s *EESTSource) extractZip(zipPath, targetDir string) error {
	return extractZipFile(zipPath, targetDir)
}

// extractInnerTarballs finds .tar.gz files in the directory, extracts them
// in-place, and removes the original tarball. GitHub Actions artifacts contain
// .tar.gz files inside the outer zip.
func (s *EESTSource) extractInnerTarballs(_ context.Context, dir string) error {
	return extractInnerTarballs(dir, s.log)
}

// extractLocalTarball extracts a local .tar.gz file to the target directory.
func (s *EESTSource) extractLocalTarball(tarballPath, targetDir string) error {
	return extractTarGzFile(tarballPath, targetDir)
}

// downloadAndExtractTarball downloads a tarball and extracts it to the target directory.
func (s *EESTSource) downloadAndExtractTarball(ctx context.Context, url, targetDir string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Create gzip reader.
	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("creating gzip reader: %w", err)
	}

	defer func() { _ = gzr.Close() }()

	// Create tar reader.
	tr := tar.NewReader(gzr)

	// Create target directory.
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("creating target directory: %w", err)
	}

	// Extract files.
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			return fmt.Errorf("reading tar: %w", err)
		}

		// Sanitize path to prevent directory traversal.
		target := filepath.Join(targetDir, filepath.Clean(header.Name))
		if !strings.HasPrefix(target, filepath.Clean(targetDir)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid tar entry: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("creating directory: %w", err)
			}
		case tar.TypeReg:
			// Ensure parent directory exists.
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("creating parent directory: %w", err)
			}

			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("creating file: %w", err)
			}

			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()

				return fmt.Errorf("extracting file: %w", err)
			}

			_ = f.Close()
		}
	}

	return nil
}

// statefulPreRunMissing reports whether a stateful fixture's absent pre_run
// file is worth warning about. It is only a concern when the start block is
// ahead of the snapshot: then the snapshot→start advance is skipped and the
// test would replay against the wrong state. When start == snapshot (e.g. a
// pre-funded seed lets fill-stateful skip the funding block) there are no
// pre_run blocks to replay, so the absence is expected and silent.
func statefulPreRunMissing(f *eest.Fixture) bool {
	return f.StartBlockHash != "" && f.StartBlockHash != f.SnapshotBlockHash
}

// loadPreRuns reads the shared pre_run files for stateful-engine fixtures from
// <searchDir>/pre_run/*.json, keyed by start block hash. A missing pre_run
// directory is not an error (the genesis-based format has none) and yields an
// empty map.
func (s *EESTSource) loadPreRuns(searchDir string) (map[string]*eest.StatefulPreRun, error) {
	preRunDir := filepath.Join(searchDir, "pre_run")

	entries, err := os.ReadDir(preRunDir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]*eest.StatefulPreRun{}, nil
		}

		return nil, fmt.Errorf("reading pre_run directory: %w", err)
	}

	preRuns := make(map[string]*eest.StatefulPreRun, len(entries))

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		path := filepath.Join(preRunDir, entry.Name())

		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading pre_run file %s: %w", path, err)
		}

		preRun, err := eest.ParsePreRunFile(data)
		if err != nil {
			s.log.WithFields(logrus.Fields{
				"file":  path,
				"error": err,
			}).Warn("Failed to parse pre_run file, skipping")

			continue
		}

		// Key by the start block hash the fixtures reference. Fall back to the
		// file name (which EEST names <startBlockHash>.json) if the field is
		// absent.
		key := preRun.StartBlockHash
		if key == "" {
			key = strings.TrimSuffix(entry.Name(), ".json")
		}

		preRuns[key] = preRun
	}

	s.log.WithField("count", len(preRuns)).Debug("Loaded stateful pre_run files")

	return preRuns, nil
}

// discoverTests parses fixture files and creates test entries.
func (s *EESTSource) discoverTests() (*PreparedSource, error) {
	// Determine the fixtures search directory.
	fixturesSubdir := s.cfg.FixturesSubdir
	if fixturesSubdir == "" {
		fixturesSubdir = config.DefaultEESTFixturesSubdir
	}

	searchDir := filepath.Join(s.fixturesDir, fixturesSubdir)

	// Verify the search directory exists.
	if _, err := os.Stat(searchDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("fixtures subdirectory %q does not exist", fixturesSubdir)
	}

	result := &PreparedSource{
		BasePath:    searchDir,
		PreRunSteps: make([]*StepFile, 0),
		Tests:       make([]*TestWithSteps, 0),
	}

	// EEST writes a .meta directory (fixtures.ini with the fill command + python/
	// tool versions, index.json, report_fill.html) at the root of the fixtures
	// dir. Attach it when present so each suite's output can carry the provenance.
	metaDir := filepath.Join(s.fixturesDir, ".meta")
	if fi, err := os.Stat(metaDir); err == nil && fi.IsDir() {
		result.MetaDir = metaDir

		s.log.WithField("meta_dir", metaDir).Debug("Found EEST .meta directory")
	}

	s.log.WithField("path", searchDir).Info("Searching for fixtures")

	// Load shared pre_run files (stateful-engine format). Keyed by start block
	// hash; empty for the genesis-based format, which has no pre_run dir.
	preRuns, err := s.loadPreRuns(searchDir)
	if err != nil {
		return nil, err
	}

	// Map fixture keys (testIds) to their TestWithSteps for pre_alloc matching.
	testsByFixtureKey := make(map[string]*TestWithSteps, 256)

	// Walk fixture directory for JSON files.
	err = filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			// pre_alloc holds genesis groups; pre_run holds stateful setup
			// payloads loaded separately above. Neither contains fixtures.
			if info.Name() == "pre_alloc" || info.Name() == "pre_run" {
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".json") {
			return nil
		}

		// Parse fixture file.
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading fixture %s: %w", path, err)
		}

		fixtures, err := eest.ParseFixtureFile(data)
		if err != nil {
			s.log.WithFields(logrus.Fields{
				"file":  path,
				"error": err,
			}).Warn("Failed to parse fixture file, skipping")

			return nil
		}

		// Convert each fixture to tests.
		for name, fixture := range fixtures {
			// Skip fixtures that don't have the supported format.
			if !fixture.IsSupportedFormat() {
				format := ""
				if fixture.Info != nil {
					format = fixture.Info.FixtureFormat
				}

				s.log.WithFields(logrus.Fields{
					"file":    path,
					"fixture": name,
					"format":  format,
				}).Debug("Skipping fixture with unsupported format")

				continue
			}

			// Apply filter to individual test names too.
			if !s.filter.match(name) {
				continue
			}

			var converted *eest.ConvertedTest

			if fixture.IsStateful() {
				preRun := preRuns[fixture.StartBlockHash]
				if preRun == nil && statefulPreRunMissing(fixture) {
					s.log.WithFields(logrus.Fields{
						"file":           path,
						"fixture":        name,
						"start_block":    fixture.StartBlockHash,
						"snapshot_block": fixture.SnapshotBlockHash,
					}).Warn("No pre_run file for stateful fixture's start block; " +
						"replaying setup payloads only")
				}

				converted, err = eest.ConvertStatefulFixture(name, fixture, preRun)
			} else {
				converted, err = eest.ConvertFixture(name, fixture)
			}

			if err != nil {
				s.log.WithFields(logrus.Fields{
					"file":    path,
					"fixture": name,
					"error":   err,
				}).Warn("Failed to convert fixture, skipping")

				continue
			}

			// Build test name from the fixture key.
			// The fixture key is a pytest node ID like
			// "tests/benchmark/.../test_foo.py::test_bar[params]".
			// Strip the leading "tests/" directory prefix if present
			// since it's a pytest artifact, not part of the test identity.
			testName := name
			if after, ok := strings.CutPrefix(testName, "tests/"); ok {
				testName = after
			}

			test := &TestWithSteps{
				Name:     testName,
				EESTInfo: fixture.Info,
			}

			// Create setup step if there are setup lines.
			if len(converted.SetupLines) > 0 {
				test.Setup = &StepFile{
					Name:     testName + "/setup",
					Provider: &linesProvider{lines: converted.SetupLines},
				}
			}

			// Create test step.
			if len(converted.TestLines) > 0 {
				test.Test = &StepFile{
					Name:     testName + "/test",
					Provider: &linesProvider{lines: converted.TestLines},
				}
			}

			result.Tests = append(result.Tests, test)
			testsByFixtureKey[name] = test
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walking fixtures directory: %w", err)
	}

	// Sort tests by name for consistent ordering.
	sort.Slice(result.Tests, func(i, j int) bool {
		return result.Tests[i].Name < result.Tests[j].Name
	})

	s.tests = result.Tests

	s.log.WithField("count", len(result.Tests)).Info("Discovered EEST fixtures")

	// Parse pre_alloc directory for multi-genesis support.
	if err := s.parsePreAlloc(searchDir, testsByFixtureKey); err != nil {
		s.log.WithError(err).Warn("Failed to parse pre_alloc directory")
	}

	// If genesis groups were found, reorder result.Tests to match execution
	// order: groups iterated by genesis hash, tests sorted by name within
	// each group. This ensures the suite summary reflects actual execution.
	if len(s.genesisGroups) > 0 {
		reordered := make([]*TestWithSteps, 0, len(result.Tests))

		for _, group := range s.genesisGroups {
			reordered = append(reordered, group.Tests...)
		}

		result.Tests = reordered
		s.tests = reordered
	}

	return result, nil
}

// Cleanup is a no-op for EEST sources (we keep the cache).
func (s *EESTSource) Cleanup() error {
	return nil
}

// GetSourceInfo returns source information for the suite summary.
func (s *EESTSource) GetSourceInfo() (*SuiteSource, error) {
	fixturesSubdir := s.cfg.FixturesSubdir
	if fixturesSubdir == "" {
		fixturesSubdir = config.DefaultEESTFixturesSubdir
	}

	// Use resolved run IDs when available, falling back to config values.
	fixturesRunID := s.resolvedFixturesRunID
	if fixturesRunID == "" {
		fixturesRunID = s.cfg.FixturesArtifactRunID
	}

	genesisRunID := s.resolvedGenesisRunID
	if genesisRunID == "" {
		genesisRunID = s.cfg.GenesisArtifactRunID
	}

	return &SuiteSource{
		EEST: &EESTSourceInfo{
			GitHubRepo:            s.cfg.GitHubRepo,
			GitHubRelease:         s.cfg.GitHubRelease,
			FixturesURL:           s.cfg.FixturesURL,
			GenesisURL:            s.cfg.GenesisURL,
			FixturesSubdir:        fixturesSubdir,
			FixturesArtifactName:  s.cfg.FixturesArtifactName,
			GenesisArtifactName:   s.cfg.GenesisArtifactName,
			FixturesArtifactRunID: fixturesRunID,
			GenesisArtifactRunID:  genesisRunID,
			LocalFixturesDir:      s.cfg.LocalFixturesDir,
			LocalGenesisDir:       s.cfg.LocalGenesisDir,
			LocalFixturesTarball:  s.cfg.LocalFixturesTarball,
			LocalGenesisTarball:   s.cfg.LocalGenesisTarball,
		},
	}, nil
}

// parsePreAlloc scans the pre_alloc directory and builds genesis groups.
func (s *EESTSource) parsePreAlloc(
	searchDir string,
	testsByFixtureKey map[string]*TestWithSteps,
) error {
	preAllocDir := filepath.Join(searchDir, "pre_alloc")

	entries, err := os.ReadDir(preAllocDir)
	if err != nil {
		if os.IsNotExist(err) {
			s.log.Debug("No pre_alloc directory found, skipping multi-genesis")

			return nil
		}

		return fmt.Errorf("reading pre_alloc directory: %w", err)
	}

	groups := make([]*GenesisGroup, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(preAllocDir, entry.Name()))
		if err != nil {
			return fmt.Errorf("reading pre_alloc file %s: %w", entry.Name(), err)
		}

		var paf preAllocFile
		if err := json.Unmarshal(data, &paf); err != nil {
			s.log.WithFields(logrus.Fields{
				"file":  entry.Name(),
				"error": err,
			}).Warn("Failed to parse pre_alloc file, skipping")

			continue
		}

		if len(paf.TestIDs) == 0 {
			continue
		}

		hash := strings.TrimSuffix(entry.Name(), ".json")
		matched := make([]*TestWithSteps, 0, len(paf.TestIDs))

		for _, testID := range paf.TestIDs {
			if t, ok := testsByFixtureKey[testID]; ok {
				t.GenesisHash = hash
				matched = append(matched, t)
			} else {
				s.log.WithFields(logrus.Fields{
					"test_id":      testID,
					"genesis_hash": hash,
				}).Debug("pre_alloc testId not found in discovered tests")
			}
		}

		if len(matched) > 0 {
			// Sort tests by name for consistent ordering within each group.
			sort.Slice(matched, func(i, j int) bool {
				return matched[i].Name < matched[j].Name
			})

			groups = append(groups, &GenesisGroup{
				GenesisHash: hash,
				Tests:       matched,
			})
		}
	}

	if len(groups) > 0 {
		s.genesisGroups = groups

		s.log.WithField("groups", len(groups)).Info("Discovered genesis groups from pre_alloc")
	}

	return nil
}

// GetGenesisGroups returns the genesis groups discovered from pre_alloc.
func (s *EESTSource) GetGenesisGroups() []*GenesisGroup {
	return s.genesisGroups
}

// GetGenesisPathForGroup returns the genesis file path for a specific
// genesis hash and client type.
func (s *EESTSource) GetGenesisPathForGroup(genesisHash, clientType string) string {
	clientDirs, filename := s.resolveClientGenesis(clientType)

	tried := make([]string, 0, len(clientDirs))
	for _, clientDir := range clientDirs {
		genesisPath := filepath.Join(
			s.genesisDir, "genesis", genesisHash, clientDir, filename,
		)
		if _, err := os.Stat(genesisPath); err == nil {
			return genesisPath
		}
		tried = append(tried, genesisPath)
	}

	s.log.WithFields(logrus.Fields{
		"genesis_hash": genesisHash,
		"client":       clientType,
		"tried":        tried,
	}).Warn("Genesis file not found for group")

	return ""
}

// resolveClientGenesis maps a client type to its candidate genesis
// directories (in priority order) and filename. Newer EEST releases
// suffix the dir with `_default` (e.g. `go-ethereum_default`); older
// releases use the bare client name. We try the new layout first and
// fall back to the old one so both work.
func (s *EESTSource) resolveClientGenesis(clientType string) ([]string, string) {
	switch clientType {
	case "geth", "erigon", "reth", "nimbus", "ethrex":
		return []string{"go-ethereum_default", "go-ethereum"}, "genesis.json"
	case "nethermind":
		return []string{"nethermind_default", "nethermind"}, "chainspec.json"
	case "besu":
		return []string{"besu_default", "besu"}, "genesis.json"
	default:
		return []string{"go-ethereum_default", "go-ethereum"}, "genesis.json"
	}
}

// GetGenesisPath returns the genesis file path for a client type.
// Maps client types to their genesis directories in the EEST release.
func (s *EESTSource) GetGenesisPath(clientType string) string {
	clientDirs, filename := s.resolveClientGenesis(clientType)

	// Genesis files are in genesis/genesis/<hash>/<client>/<filename>
	// Find the hash subdirectory (there should typically be one).
	genesisBaseDir := filepath.Join(s.genesisDir, "genesis")

	entries, err := os.ReadDir(genesisBaseDir)
	if err != nil {
		s.log.WithError(err).Warn("Failed to read genesis directory")

		return ""
	}

	// Find the first directory (the hash directory).
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		for _, clientDir := range clientDirs {
			genesisPath := filepath.Join(
				genesisBaseDir, entry.Name(), clientDir, filename,
			)
			if _, err := os.Stat(genesisPath); err == nil {
				return genesisPath
			}
		}
	}

	s.log.WithFields(logrus.Fields{
		"client":  clientType,
		"baseDir": genesisBaseDir,
	}).Warn("Genesis file not found")

	return ""
}

// linesProvider implements StepProvider for in-memory lines.
type linesProvider struct {
	lines []string
}

// Lines returns the JSON-RPC lines.
func (p *linesProvider) Lines() []string {
	return p.lines
}

// Content returns the full content as bytes for hashing.
func (p *linesProvider) Content() []byte {
	return []byte(strings.Join(p.lines, "\n"))
}

// EESTSourceInfo contains EEST source information for the suite summary.
type EESTSourceInfo struct {
	GitHubRepo     string `json:"github_repo,omitempty"`
	GitHubRelease  string `json:"github_release,omitempty"`
	FixturesURL    string `json:"fixtures_url,omitempty"`
	GenesisURL     string `json:"genesis_url,omitempty"`
	FixturesSubdir string `json:"fixtures_subdir,omitempty"`
	// Artifact fields (alternative to releases).
	FixturesArtifactName  string `json:"fixtures_artifact_name,omitempty"`
	GenesisArtifactName   string `json:"genesis_artifact_name,omitempty"`
	FixturesArtifactRunID string `json:"fixtures_artifact_run_id,omitempty"`
	GenesisArtifactRunID  string `json:"genesis_artifact_run_id,omitempty"`
	// Local source fields.
	LocalFixturesDir     string `json:"local_fixtures_dir,omitempty"`
	LocalGenesisDir      string `json:"local_genesis_dir,omitempty"`
	LocalFixturesTarball string `json:"local_fixtures_tarball,omitempty"`
	LocalGenesisTarball  string `json:"local_genesis_tarball,omitempty"`
}
