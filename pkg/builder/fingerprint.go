package builder

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// buildSidecarFile is the benchmarkoor-owned build-fingerprint sidecar written
// at an output_dir root after a successful build. When --rebuild-on-diff is set,
// the next build compares the current config fingerprint against this file to
// decide whether the output is stale and must be rebuilt.
const buildSidecarFile = ".benchmarkoor-build.json"

// buildFingerprintSchema versions the sidecar format so a future change can
// invalidate old sidecars deliberately.
const buildFingerprintSchema = 1

// buildSidecar records the config fingerprint of a built output_dir.
type buildSidecar struct {
	SchemaVersion int    `json:"schema_version"`
	Builder       string `json:"builder"`
	Fingerprint   string `json:"fingerprint"`
	// Inputs is the canonical, human-readable view of what the fingerprint
	// covers — persisted so a rebuild can report which fields changed.
	Inputs map[string]any `json:"inputs"`
}

// fingerprintInputs is the canonical, hashable view of a build's
// output-affecting config. Keys are stable; values are strings, numbers, bools,
// or slices/maps thereof so the whole thing marshals to deterministic JSON
// (encoding/json sorts map keys at every level).
type fingerprintInputs map[string]any

// hash returns the sha256 (hex) of the canonical JSON of the inputs.
func (fi fingerprintInputs) hash() (string, error) {
	canonical, err := json.Marshal(fi)
	if err != nil {
		return "", fmt.Errorf("marshalling fingerprint inputs: %w", err)
	}

	sum := sha256.Sum256(canonical)

	return hex.EncodeToString(sum[:]), nil
}

// readBuildSidecar reads the fingerprint sidecar from dir. It returns
// (nil, nil) when the sidecar is absent — an output built before fingerprinting
// existed, or a fresh dir — so callers can treat "no baseline" distinctly from
// a real error.
func readBuildSidecar(dir string) (*buildSidecar, error) {
	data, err := os.ReadFile(filepath.Join(dir, buildSidecarFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("reading build sidecar: %w", err)
	}

	var sc buildSidecar
	if err := json.Unmarshal(data, &sc); err != nil {
		return nil, fmt.Errorf("parsing build sidecar %s: %w", buildSidecarFile, err)
	}

	return &sc, nil
}

// writeBuildSidecar writes the fingerprint sidecar into dir for builderName.
func writeBuildSidecar(dir, builderName string, inputs fingerprintInputs) error {
	fp, err := inputs.hash()
	if err != nil {
		return err
	}

	sc := buildSidecar{
		SchemaVersion: buildFingerprintSchema,
		Builder:       builderName,
		Fingerprint:   fp,
		Inputs:        inputs,
	}

	data, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling build sidecar: %w", err)
	}

	if err := os.WriteFile(filepath.Join(dir, buildSidecarFile), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("writing build sidecar: %w", err)
	}

	return nil
}

// changedInputKeys returns the sorted input keys that differ between a stored
// sidecar and the current inputs — used to report what triggered a rebuild.
func changedInputKeys(old map[string]any, cur fingerprintInputs) []string {
	seen := make(map[string]struct{}, len(cur)+len(old))
	for k, v := range cur {
		ov, ok := old[k]
		if !ok || !jsonEqual(ov, v) {
			seen[k] = struct{}{}
		}
	}

	for k := range old {
		if _, ok := cur[k]; !ok {
			seen[k] = struct{}{}
		}
	}

	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}

// jsonEqual compares two values by their JSON encoding, so an int in the
// current inputs matches the float64 the same value unmarshals to from a stored
// sidecar.
func jsonEqual(a, b any) bool {
	aJSON, errA := json.Marshal(a)
	bJSON, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}

	return string(aJSON) == string(bJSON)
}

// rebuildDecision is the outcome of comparing current inputs against a
// populated output_dir's stored fingerprint under --rebuild-on-diff.
type rebuildDecision struct {
	rebuild bool     // true → wipe and rebuild; false → skip
	reason  string   // why we skipped (only set when rebuild is false)
	changed []string // input keys that changed (only set when rebuild is true)
}

// decideRebuild compares the current fingerprint inputs against the sidecar in
// dir. An absent sidecar (no baseline) or a matching fingerprint yields skip; a
// differing fingerprint yields rebuild with the list of changed keys.
func decideRebuild(dir string, inputs fingerprintInputs) (rebuildDecision, error) {
	cur, err := inputs.hash()
	if err != nil {
		return rebuildDecision{}, err
	}

	sidecar, err := readBuildSidecar(dir)
	if err != nil {
		return rebuildDecision{}, err
	}

	switch {
	case sidecar == nil:
		return rebuildDecision{reason: "no build fingerprint recorded (run --force to record a baseline)"}, nil
	case sidecar.Fingerprint == cur:
		return rebuildDecision{reason: "config unchanged"}, nil
	default:
		return rebuildDecision{rebuild: true, changed: changedInputKeys(sidecar.Inputs, inputs)}, nil
	}
}

// sha256Hex returns the hex sha256 of b.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)

	return hex.EncodeToString(sum[:])
}

// sha256File returns the hex sha256 of the file at path, or "" when path is
// empty. A missing file is an error (a configured input that cannot be read
// should surface, not silently fingerprint as absent).
func sha256File(path string) (string, error) {
	if path == "" {
		return "", nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("hashing %s: %w", path, err)
	}

	return sha256Hex(data), nil
}
