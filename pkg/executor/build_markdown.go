package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ethpandaops/benchmarkoor/pkg/builder"
)

// Build-summary artifact filenames (written under each target's output_dir).
const (
	stateActorManifestFile = "state-actor-manifest.json"
	eestFillResultFile     = ".benchmarkoor-fill.json"
	buildFingerprintFile   = ".benchmarkoor-build.json"
	pytestReportFile       = ".benchmarkoor-pytest-report.json"
)

// BuildSummary is the per-invocation result of `benchmarkoor build`, persisted
// by the build command and rendered to markdown by GenerateBuildMarkdown.
type BuildSummary struct {
	GeneratedAt string               `json:"generated_at,omitempty"`
	Targets     []BuildTargetSummary `json:"targets"`
}

// BuildTargetSummary is one builder target's outcome. It carries only the
// in-memory build result; richer per-target detail (state-actor manifest, eest
// provenance + fill counts) is read from the target's output_dir at render time.
type BuildTargetSummary struct {
	Builder   string `json:"builder"` // "state-actor" | "eest-payloads"
	Name      string `json:"name"`
	Client    string `json:"client"`
	OutputDir string `json:"output_dir"`
	// BundleDir is where a pre-runs target wrote its replay bundle (in a
	// pre_run_bundle subdirectory). Defaults to OutputDir but bundle_dir can move
	// it elsewhere; empty for builders that produce no bundle.
	BundleDir string `json:"bundle_dir,omitempty"`
	// Bundle describes the replay bundle a pre-runs target recorded: the block it
	// attaches to, the head it reaches, and how many payloads it carries. Nil for
	// builders and targets that record none.
	Bundle    *builder.PreRunBundleInfo `json:"bundle,omitempty"`
	Status    string                    `json:"status"` // "OK" | "SKIP" | "ERR"
	Error     string                    `json:"error,omitempty"`
	ElapsedMs int64                     `json:"elapsed_ms"`
}

// stateActorManifest mirrors the fields of state-actor-manifest.json we render.
// The manifest is written by the external state-actor binary; this is a partial
// view (unused fields are ignored).
type stateActorManifest struct {
	Flags struct {
		Client     string `json:"client"`
		Fork       string `json:"fork"`
		Seed       int64  `json:"seed"`
		ChainID    int64  `json:"chain_id"`
		GasLimit   int64  `json:"gas_limit"`
		TargetSize string `json:"target_size"`
	} `json:"flags"`
	Result *struct {
		StateRoot        string `json:"state_root"`
		AccountsCreated  int64  `json:"accounts_created"`
		ContractsCreated int64  `json:"contracts_created"`
		StorageSlots     int64  `json:"storage_slots"`
		TotalDBSizeBytes int64  `json:"total_db_size_bytes"`
		ElapsedMs        int64  `json:"elapsed_ms"`
	} `json:"result"`
	StateActor struct {
		Version string `json:"version"`
	} `json:"state_actor"`
	// Benchmarkoor is the benchmarkoor-namespaced block added after generation,
	// carrying the docker image used to produce the datadir + its sha256 digest.
	Benchmarkoor *struct {
		Image       string `json:"image"`
		ImageDigest string `json:"image_digest"`
	} `json:"benchmarkoor"`
}

// eestFingerprint mirrors the config-level fill inputs of the
// .benchmarkoor-build.json fingerprint sidecar that we surface in the summary.
type eestFingerprint struct {
	Inputs struct {
		Tests              []string `json:"tests"`
		GasBenchmarkValues []int    `json:"gas_benchmark_values"`
		Marker             string   `json:"marker"`
		DataDirMethod      string   `json:"datadir_method"`
		EESTRepo           string   `json:"eest_repo"`
	} `json:"inputs"`
}

// pytestReport mirrors the fields of the pytest json report we surface (the
// fill's wall-clock duration).
type pytestReport struct {
	Duration float64 `json:"duration"`
}

// eestFillResult mirrors the .benchmarkoor-fill.json sidecar: the eest target's
// provenance plus the authoritative fill counts. Written even after a failed
// fill, so a failed target is still fully described.
type eestFillResult struct {
	SourceDir    string `json:"source_dir"`
	FillerClient string `json:"filler_client"`
	FillerImage  string `json:"filler_image"`
	EESTSHA      string `json:"eest_sha"`
	Fork         string `json:"fork"`
	Filter       string `json:"filter"`
	SizeBytes    int64  `json:"size_bytes"`
	Filled       int    `json:"filled"`
	Failed       int    `json:"failed"`
}

// ReadBuildSummary parses a build-summary.json written by
// `benchmarkoor build --summary-json`.
func ReadBuildSummary(path string) (*BuildSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading build summary %s: %w", path, err)
	}

	var summary BuildSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return nil, fmt.Errorf("parsing build summary %s: %w", path, err)
	}

	return &summary, nil
}

// GenerateBuildMarkdown reads a build-summary.json and renders a markdown
// summary of the state-actor and eest-payloads builds, enriching each target
// from its on-disk output_dir artifacts. Output is truncated to maxChars.
func GenerateBuildMarkdown(summaryPath string, maxChars int) (string, error) {
	summary, err := ReadBuildSummary(summaryPath)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString("## 🛠️ Build Summary\n\n")

	if summary.GeneratedAt != "" {
		fmt.Fprintf(&sb, "_Generated at %s_\n\n", summary.GeneratedAt)
	}

	if len(summary.Targets) == 0 {
		sb.WriteString("_No build targets._\n")
	}

	for _, t := range summary.Targets {
		writeTargetSection(&sb, t)
	}

	out := sb.String()
	if len(out) > maxChars {
		out = out[:maxChars] + "\n\n_… truncated._\n"
	}

	return out, nil
}

// writeTargetSection renders one build target as its own section with a
// Field | Value table (matching the run markdown generator's style).
func writeTargetSection(sb *strings.Builder, t BuildTargetSummary) {
	fmt.Fprintf(sb, "### %s %s — %s\n\n", statusEmoji(t.Status), t.Name, t.Builder)

	sb.WriteString("| Field | Value |\n")
	sb.WriteString("|---|---|\n")

	writeField(sb, "Status", statusLabel(t.Status))

	switch t.Builder {
	case "eest-payloads":
		writeEESTSection(sb, t)
	case "pre-runs":
		writePreRunsSection(sb, t)
	default:
		writeStateActorSection(sb, t)
	}

	sb.WriteString("\n")

	if t.Status == "ERR" && t.Error != "" {
		fmt.Fprintf(sb, "> ❌ %s\n\n", t.Error)
	}
}

func writeStateActorSection(sb *strings.Builder, t BuildTargetSummary) {
	writeField(sb, "Client", orDash(t.Client))

	if m := readStateActorManifest(t.OutputDir); m != nil {
		writeField(sb, "Fork", orDash(m.Flags.Fork))

		if m.Benchmarkoor != nil {
			writeField(sb, "Docker image", code(m.Benchmarkoor.Image))
			writeField(sb, "Image digest", code(m.Benchmarkoor.ImageDigest))
		}

		if m.StateActor.Version != "" {
			writeField(sb, "State-actor version", code(m.StateActor.Version))
		}

		if m.Flags.TargetSize != "" {
			writeField(sb, "Target size", m.Flags.TargetSize)
		}

		if m.Flags.GasLimit != 0 {
			writeField(sb, "Gas limit", formatInt(m.Flags.GasLimit))
		}

		if m.Flags.Seed != 0 {
			writeField(sb, "Seed", formatInt(m.Flags.Seed))
		}

		if m.Flags.ChainID != 0 {
			writeField(sb, "Chain ID", formatInt(m.Flags.ChainID))
		}

		if m.Result != nil {
			writeField(sb, "State root", code(shortHash(m.Result.StateRoot)))
			writeField(sb, "Accounts created", formatInt(m.Result.AccountsCreated))
			writeField(sb, "Contracts created", formatInt(m.Result.ContractsCreated))
			writeField(sb, "Storage slots", formatInt(m.Result.StorageSlots))
			writeField(sb, "DB size", formatBytes(m.Result.TotalDBSizeBytes))
		}
	}

	writeField(sb, "Elapsed", formatDurationNs(t.ElapsedMs*1_000_000))
}

// writePreRunsSection renders a pre-runs target: where its advanced datadir and
// replay bundle went, and what the bundle spans. The start block is the one the
// bundle attaches TO, so it doubles as the answer to "which snapshot can replay
// this".
func writePreRunsSection(sb *strings.Builder, t BuildTargetSummary) {
	writeField(sb, "Client", orDash(t.Client))
	writeField(sb, "Advanced datadir", code(t.OutputDir))

	if t.BundleDir != "" && t.BundleDir != t.OutputDir {
		writeField(sb, "Bundle dir", code(t.BundleDir))
	}

	if b := t.Bundle; b != nil {
		writeField(sb, "Payloads", formatInt(int64(b.Payloads)))
		writeField(sb, "Start block",
			fmt.Sprintf("%s %s", formatInt(int64(b.StartBlockNumber)), code(shortHash(b.StartBlockHash))))
		writeField(sb, "End block",
			fmt.Sprintf("%s %s", formatInt(int64(b.EndBlockNumber)), code(shortHash(b.EndBlockHash))))

		if !b.Contiguous() {
			writeField(sb, "Range", fmt.Sprintf(
				"⚠️ %d payloads over %d blocks (gaps or duplicates)",
				b.Payloads, b.EndBlockNumber-b.StartBlockNumber))
		}
	} else {
		// Replay targets consume a bundle instead of recording one.
		writeField(sb, "Bundle", "— (none recorded)")
	}

	writeField(sb, "Elapsed", formatDurationNs(t.ElapsedMs*1_000_000))
}

func writeEESTSection(sb *strings.Builder, t BuildTargetSummary) {
	fill := readEESTFillResult(t.OutputDir)
	fp := readEESTFingerprint(t.OutputDir)
	rep := readPytestReport(t.OutputDir)

	if fill != nil {
		writeField(sb, "Source", orDash(fill.SourceDir))
		writeField(sb, "Filler", orDash(fill.FillerClient))
		writeField(sb, "Filler image", code(fill.FillerImage))
	}

	if fp != nil && fp.Inputs.EESTRepo != "" {
		writeField(sb, "EEST repo", fp.Inputs.EESTRepo)
	}

	if fill != nil {
		writeField(sb, "EEST commit", code(shortSHA(fill.EESTSHA)))
		writeField(sb, "Fork", orDash(fill.Fork))
	}

	if fp != nil && len(fp.Inputs.Tests) > 0 {
		writeField(sb, "Tests", code(strings.Join(fp.Inputs.Tests, ", ")))
	}

	if fill != nil {
		writeField(sb, "Filter", orDash(fill.Filter))
	}

	if fp != nil {
		if fp.Inputs.Marker != "" {
			writeField(sb, "Marker", code(fp.Inputs.Marker))
		}

		if len(fp.Inputs.GasBenchmarkValues) > 0 {
			writeField(sb, "Gas values", joinInts(fp.Inputs.GasBenchmarkValues))
		}

		if fp.Inputs.DataDirMethod != "" {
			writeField(sb, "Datadir method", fp.Inputs.DataDirMethod)
		}
	}

	if fill != nil {
		writeField(sb, "Filled", formatInt(int64(fill.Filled)))
		writeField(sb, "Failed", formatInt(int64(fill.Failed)))
		writeField(sb, "Fixtures size", formatBytes(fill.SizeBytes))
	}

	if rep != nil && rep.Duration > 0 {
		writeField(sb, "Fill duration", formatDurationNs(int64(rep.Duration*1e9)))
	}

	writeField(sb, "Elapsed", formatDurationNs(t.ElapsedMs*1_000_000))
}

func writeField(sb *strings.Builder, label, value string) {
	fmt.Fprintf(sb, "| %s | %s |\n", label, value)
}

func statusEmoji(status string) string {
	switch status {
	case "OK":
		return "✅"
	case "SKIP":
		return "⏭️"
	case "ERR":
		return "❌"
	default:
		return "•"
	}
}

func statusLabel(status string) string {
	switch status {
	case "OK":
		return "OK"
	case "SKIP":
		return "Skipped (unchanged)"
	case "ERR":
		return "Failed"
	default:
		return orDash(status)
	}
}

// code wraps s in inline-code backticks, or renders a dash when empty.
func code(s string) string {
	if s == "" || s == "-" {
		return "-"
	}

	return "`" + s + "`"
}

// formatInt renders n with thousands separators (1351914 → "1,351,914").
func formatInt(n int64) string {
	s := strconv.FormatInt(n, 10)

	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}

	var out []byte
	for i := 0; i < len(s); i++ {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}

		out = append(out, s[i])
	}

	return sign + string(out)
}

// shortSHA abbreviates a git commit hash to its first 10 chars.
func shortSHA(s string) string {
	if len(s) <= 10 {
		return orDash(s)
	}

	return s[:10]
}

func readStateActorManifest(outputDir string) *stateActorManifest {
	data, err := os.ReadFile(filepath.Join(outputDir, stateActorManifestFile))
	if err != nil {
		return nil
	}

	var m stateActorManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}

	return &m
}

func readEESTFillResult(outputDir string) *eestFillResult {
	data, err := os.ReadFile(filepath.Join(outputDir, eestFillResultFile))
	if err != nil {
		return nil
	}

	var r eestFillResult
	if err := json.Unmarshal(data, &r); err != nil {
		return nil
	}

	return &r
}

func readEESTFingerprint(outputDir string) *eestFingerprint {
	data, err := os.ReadFile(filepath.Join(outputDir, buildFingerprintFile))
	if err != nil {
		return nil
	}

	var fp eestFingerprint
	if err := json.Unmarshal(data, &fp); err != nil {
		return nil
	}

	return &fp
}

func readPytestReport(outputDir string) *pytestReport {
	data, err := os.ReadFile(filepath.Join(outputDir, pytestReportFile))
	if err != nil {
		return nil
	}

	var r pytestReport
	if err := json.Unmarshal(data, &r); err != nil {
		return nil
	}

	return &r
}

// joinInts renders an int slice as a comma-separated string ("200, 300").
func joinInts(vals []int) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = strconv.Itoa(v)
	}

	return strings.Join(parts, ", ")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}

	return s
}

// shortHash abbreviates a 0x hash to first-10…last-6 for table display.
func shortHash(h string) string {
	if len(h) <= 20 {
		return orDash(h)
	}

	return h[:10] + "…" + h[len(h)-6:]
}
