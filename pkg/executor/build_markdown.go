package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Build-summary artifact filenames (written under each target's output_dir).
const (
	stateActorManifestFile = "state-actor-manifest.json"
	eestFillResultFile     = ".benchmarkoor-fill.json"
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
	Status    string `json:"status"` // "OK" | "SKIP" | "ERR"
	Error     string `json:"error,omitempty"`
	ElapsedMs int64  `json:"elapsed_ms"`
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
	// Benchmarkoor is the benchmarkoor-namespaced block added after generation,
	// carrying the docker image used to produce the datadir + its sha256 digest.
	Benchmarkoor *struct {
		Image       string `json:"image"`
		ImageDigest string `json:"image_digest"`
	} `json:"benchmarkoor"`
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

// GenerateBuildMarkdown reads a build-summary.json and renders a markdown
// summary of the state-actor and eest-payloads builds, enriching each target
// from its on-disk output_dir artifacts. Output is truncated to maxChars.
func GenerateBuildMarkdown(summaryPath string, maxChars int) (string, error) {
	data, err := os.ReadFile(summaryPath)
	if err != nil {
		return "", fmt.Errorf("reading build summary %s: %w", summaryPath, err)
	}

	var summary BuildSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return "", fmt.Errorf("parsing build summary %s: %w", summaryPath, err)
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

func writeEESTSection(sb *strings.Builder, t BuildTargetSummary) {
	if fill := readEESTFillResult(t.OutputDir); fill != nil {
		writeField(sb, "Source", orDash(fill.SourceDir))
		writeField(sb, "Filler", orDash(fill.FillerClient))
		writeField(sb, "Filler image", code(fill.FillerImage))
		writeField(sb, "EEST commit", code(shortSHA(fill.EESTSHA)))
		writeField(sb, "Fork", orDash(fill.Fork))
		writeField(sb, "Filter", orDash(fill.Filter))
		writeField(sb, "Filled", formatInt(int64(fill.Filled)))
		writeField(sb, "Failed", formatInt(int64(fill.Failed)))
		writeField(sb, "Fixtures size", formatBytes(fill.SizeBytes))
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
