package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

const (
	// defaultHiveRepo is the GitHub repository for hive mapper files.
	defaultHiveRepo = "ethereum/hive"

	// defaultHiveBranch is the branch to fetch mapper files from.
	defaultHiveBranch = "master"
)

// HiveFile represents the JSON structure of a hive/{hash}.json file
// containing the generic genesis and HIVE_* environment variables.
type HiveFile struct {
	Genesis     json.RawMessage   `json:"genesis"`
	Environment map[string]string `json:"environment"`
}

// mapperOutput defines a single jq mapper invocation and its output file.
type mapperOutput struct {
	MapperFile string // e.g., "mapper.jq"
	OutputFile string // e.g., "genesis.json" or "chainspec.json"
}

// clientGenesisMapping defines the mapper directory and outputs for a client.
type clientGenesisMapping struct {
	MapperDir string         // Directory name under mapperDir (e.g., "go-ethereum")
	Outputs   []mapperOutput // Mapper files to run and their outputs
	// PrimaryOutput is the file benchmarkoor mounts into the container.
	PrimaryOutput string
	// CopyGenesis indicates whether the raw generic genesis should also be
	// written to genesis.json alongside the mapper output.
	CopyGenesis bool
}

// clientGenesisMappings maps client types to their mapper configuration.
var clientGenesisMappings = map[string]clientGenesisMapping{
	"geth": {
		MapperDir:     "go-ethereum",
		Outputs:       []mapperOutput{{MapperFile: "mapper.jq", OutputFile: "genesis.json"}},
		PrimaryOutput: "genesis.json",
	},
	"erigon": {
		MapperDir:     "erigon",
		Outputs:       []mapperOutput{{MapperFile: "mapper.jq", OutputFile: "genesis.json"}},
		PrimaryOutput: "genesis.json",
	},
	"reth": {
		MapperDir:     "reth",
		Outputs:       []mapperOutput{{MapperFile: "mapper.jq", OutputFile: "genesis.json"}},
		PrimaryOutput: "genesis.json",
	},
	"nimbus": {
		MapperDir:     "nimbus-el",
		Outputs:       []mapperOutput{{MapperFile: "mapper.jq", OutputFile: "genesis.json"}},
		PrimaryOutput: "genesis.json",
	},
	"besu": {
		MapperDir:     "besu",
		Outputs:       []mapperOutput{{MapperFile: "mapper.jq", OutputFile: "genesis.json"}},
		PrimaryOutput: "genesis.json",
	},
	"nethermind": {
		MapperDir: "nethermind",
		Outputs: []mapperOutput{
			{MapperFile: "mapper.jq", OutputFile: "chainspec.json"},
		},
		PrimaryOutput: "chainspec.json",
		CopyGenesis:   true, // Also write raw generic genesis as genesis.json
	},
}

// genesisGenerator handles generating client-native genesis files
// from hive files using jq mapper scripts.
type genesisGenerator struct {
	log       logrus.FieldLogger
	mapperDir string // Path to directory containing client mapper.jq files
	cacheDir  string // Path to cache generated genesis files
	mu        sync.RWMutex
	generated map[string]string // cache key -> generated file path
}

// newGenesisGenerator creates a new genesis generator.
func newGenesisGenerator(
	log logrus.FieldLogger,
	mapperDir, cacheDir string,
) *genesisGenerator {
	return &genesisGenerator{
		log:       log,
		mapperDir: mapperDir,
		cacheDir:  cacheDir,
		generated: make(map[string]string, 64),
	}
}

// FetchMappers downloads mapper.jq files from the hive GitHub repository
// for all configured client types. Called once per run to ensure fresh mappers.
func (g *genesisGenerator) FetchMappers(ctx context.Context) error {
	type mapperFile struct {
		dir  string
		file string
	}

	seen := make(map[string]bool, len(clientGenesisMappings)*2)
	files := make([]mapperFile, 0, len(clientGenesisMappings)*2)

	for _, mapping := range clientGenesisMappings {
		for _, out := range mapping.Outputs {
			key := mapping.MapperDir + "/" + out.MapperFile
			if seen[key] {
				continue
			}

			seen[key] = true

			files = append(files, mapperFile{dir: mapping.MapperDir, file: out.MapperFile})
		}
	}

	for _, mf := range files {
		localPath := filepath.Join(g.mapperDir, mf.dir, mf.file)

		rawURL := fmt.Sprintf(
			"https://raw.githubusercontent.com/%s/%s/clients/%s/%s",
			defaultHiveRepo, defaultHiveBranch, mf.dir, mf.file,
		)

		g.log.WithFields(logrus.Fields{
			"client": mf.dir,
			"file":   mf.file,
		}).Info("Fetching mapper from hive")

		if err := downloadFile(ctx, rawURL, localPath); err != nil {
			return fmt.Errorf("fetching mapper %s/%s: %w", mf.dir, mf.file, err)
		}
	}

	return nil
}

// downloadFile fetches a URL and writes the response body to localPath.
func downloadFile(ctx context.Context, rawURL, localPath string) error {
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, rawURL)
	}

	f, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}

	defer func() { _ = f.Close() }()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	return nil
}

// GenerateClientGenesis generates all client-native genesis files from a hive
// file. It reads the hive file, runs the appropriate mapper.jq(s) with HIVE_*
// env vars, and writes the outputs to the cache directory.
// Return the path to the primary genesis file (the one benchmarkoor mounts).
func (g *genesisGenerator) GenerateClientGenesis(
	ctx context.Context,
	hiveFilePath, genesisHash, clientType string,
) (string, error) {
	mapping, ok := clientGenesisMappings[clientType]
	if !ok {
		return "", fmt.Errorf("unsupported client type: %s", clientType)
	}

	cacheKey := genesisHash + "/" + mapping.MapperDir

	// Check cache first.
	g.mu.RLock()
	if path, cached := g.generated[cacheKey]; cached {
		g.mu.RUnlock()

		return path, nil
	}
	g.mu.RUnlock()

	// Read and parse the hive file.
	data, err := os.ReadFile(hiveFilePath)
	if err != nil {
		return "", fmt.Errorf("reading hive file %s: %w", hiveFilePath, err)
	}

	var hf HiveFile
	if err := json.Unmarshal(data, &hf); err != nil {
		return "", fmt.Errorf("parsing hive file %s: %w", hiveFilePath, err)
	}

	// Create output directory.
	outputDir := filepath.Join(
		g.cacheDir, "generated_genesis", genesisHash, mapping.MapperDir,
	)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("creating output directory: %w", err)
	}

	// Write generic genesis to a temp file for jq input.
	tmpGenesis, err := os.CreateTemp("", "genesis-input-*.json")
	if err != nil {
		return "", fmt.Errorf("creating temp genesis file: %w", err)
	}

	defer func() {
		_ = tmpGenesis.Close()
		_ = os.Remove(tmpGenesis.Name())
	}()

	if _, err := tmpGenesis.Write(hf.Genesis); err != nil {
		return "", fmt.Errorf("writing temp genesis: %w", err)
	}

	if err := tmpGenesis.Close(); err != nil {
		return "", fmt.Errorf("closing temp genesis: %w", err)
	}

	// Build environment for jq: inherit minimal env + HIVE_* vars.
	env := make([]string, 0, len(hf.Environment)+2)
	env = append(env, "PATH="+os.Getenv("PATH"))
	env = append(env, "HOME="+os.Getenv("HOME"))

	for k, v := range hf.Environment {
		env = append(env, k+"="+v)
	}

	// Run each mapper.
	for _, out := range mapping.Outputs {
		mapperPath := filepath.Join(
			g.mapperDir, mapping.MapperDir, out.MapperFile,
		)

		cmd := exec.CommandContext(ctx, "jq", "-f", mapperPath, tmpGenesis.Name())
		cmd.Env = env

		output, err := cmd.Output()
		if err != nil {
			var stderr string

			if exitErr, ok := err.(*exec.ExitError); ok {
				stderr = string(exitErr.Stderr)
			}

			return "", fmt.Errorf(
				"running jq for %s/%s/%s: %w (stderr: %s)",
				genesisHash, mapping.MapperDir, out.MapperFile,
				err, stderr,
			)
		}

		if !json.Valid(output) {
			return "", fmt.Errorf(
				"jq produced invalid JSON for %s/%s/%s",
				genesisHash, mapping.MapperDir, out.MapperFile,
			)
		}

		outputPath := filepath.Join(outputDir, out.OutputFile)
		if err := os.WriteFile(outputPath, output, 0644); err != nil {
			return "", fmt.Errorf("writing %s: %w", out.OutputFile, err)
		}
	}

	// Copy raw generic genesis if needed (e.g., nethermind needs genesis.json).
	if mapping.CopyGenesis {
		genesisPath := filepath.Join(outputDir, "genesis.json")
		if err := os.WriteFile(genesisPath, hf.Genesis, 0644); err != nil {
			return "", fmt.Errorf("writing genesis.json copy: %w", err)
		}
	}

	// Return path to primary output.
	primaryPath := filepath.Join(outputDir, mapping.PrimaryOutput)

	g.mu.Lock()
	g.generated[cacheKey] = primaryPath
	g.mu.Unlock()

	g.log.WithFields(logrus.Fields{
		"genesis_hash": genesisHash,
		"client":       clientType,
		"mapper_dir":   mapping.MapperDir,
		"output":       primaryPath,
	}).Debug("Generated client genesis")

	return primaryPath, nil
}

// SupportedClientTypes returns the list of client types with mapper configs.
func SupportedClientTypes() []string {
	types := make([]string, 0, len(clientGenesisMappings))
	for k := range clientGenesisMappings {
		types = append(types, k)
	}

	return types
}

// readHiveDir returns a list of genesis hashes from the hive directory.
func readHiveDir(hiveDir string) ([]string, error) {
	entries, err := os.ReadDir(hiveDir)
	if err != nil {
		return nil, fmt.Errorf("reading hive directory %s: %w", hiveDir, err)
	}

	hashes := make([]string, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		hash := strings.TrimSuffix(entry.Name(), ".json")
		hashes = append(hashes, hash)
	}

	return hashes, nil
}
