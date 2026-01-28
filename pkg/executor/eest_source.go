package executor

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/eest"
	"github.com/sirupsen/logrus"
)

// EESTSource provides tests from EEST fixtures in GitHub releases.
type EESTSource struct {
	log         logrus.FieldLogger
	cfg         *config.EESTFixturesSource
	cacheDir    string
	filter      string
	fixturesDir string
	genesisDir  string
	tests       []*TestWithSteps
}

// NewEESTSource creates a new EEST source.
func NewEESTSource(log logrus.FieldLogger, cfg *config.EESTFixturesSource, cacheDir, filter string) *EESTSource {
	return &EESTSource{
		log:      log.WithField("source", "eest"),
		cfg:      cfg,
		cacheDir: cacheDir,
		filter:   filter,
	}
}

// Prepare downloads and extracts fixtures from GitHub releases.
func (s *EESTSource) Prepare(ctx context.Context) (*PreparedSource, error) {
	// Build cache path.
	repoHash := hashRepoURL(s.cfg.GitHubRepo)
	cacheBase := filepath.Join(s.cacheDir, "eest", repoHash, s.cfg.GitHubRelease)

	s.fixturesDir = filepath.Join(cacheBase, "fixtures")
	s.genesisDir = filepath.Join(cacheBase, "genesis")

	// Check if already extracted.
	if _, err := os.Stat(s.fixturesDir); os.IsNotExist(err) {
		s.log.Info("Downloading EEST fixtures")

		if err := s.downloadAndExtract(ctx, cacheBase); err != nil {
			return nil, fmt.Errorf("downloading fixtures: %w", err)
		}
	} else {
		s.log.WithField("path", cacheBase).Info("Using cached EEST fixtures")
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

	s.log.WithField("path", searchDir).Info("Searching for fixtures")

	// Walk fixture directory for JSON files.
	err := filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}

		// Apply filter if provided.
		if s.filter != "" && !strings.Contains(path, s.filter) {
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

		// Get relative path for test naming.
		relPath, err := filepath.Rel(searchDir, path)
		if err != nil {
			relPath = filepath.Base(path)
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
			if s.filter != "" && !strings.Contains(name, s.filter) {
				continue
			}

			converted, err := eest.ConvertFixture(name, fixture)
			if err != nil {
				s.log.WithFields(logrus.Fields{
					"file":    path,
					"fixture": name,
					"error":   err,
				}).Warn("Failed to convert fixture, skipping")

				continue
			}

			// Build test name: file_path/fixture_name.
			testName := strings.TrimSuffix(relPath, ".json") + "/" + name

			test := &TestWithSteps{
				Name: testName,
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

	return &SuiteSource{
		EEST: &EESTSourceInfo{
			GitHubRepo:     s.cfg.GitHubRepo,
			GitHubRelease:  s.cfg.GitHubRelease,
			FixturesURL:    s.cfg.FixturesURL,
			GenesisURL:     s.cfg.GenesisURL,
			FixturesSubdir: fixturesSubdir,
		},
	}, nil
}

// GetGenesisPath returns the genesis file path for a client type.
// Maps client types to their genesis directories in the EEST release.
func (s *EESTSource) GetGenesisPath(clientType string) string {
	// Map client types to genesis directories and filenames.
	var clientDir, filename string

	switch clientType {
	case "geth", "erigon", "reth", "nimbus":
		clientDir = "go-ethereum"
		filename = "genesis.json"
	case "nethermind":
		clientDir = "nethermind"
		filename = "chainspec.json"
	case "besu":
		clientDir = "besu"
		filename = "genesis.json"
	default:
		// Default to geth format.
		clientDir = "go-ethereum"
		filename = "genesis.json"
	}

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
		if entry.IsDir() {
			genesisPath := filepath.Join(genesisBaseDir, entry.Name(), clientDir, filename)
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
	GitHubRepo     string `json:"github_repo"`
	GitHubRelease  string `json:"github_release"`
	FixturesURL    string `json:"fixtures_url,omitempty"`
	GenesisURL     string `json:"genesis_url,omitempty"`
	FixturesSubdir string `json:"fixtures_subdir,omitempty"`
}
