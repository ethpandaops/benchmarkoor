package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// markdownRunConfig mirrors runner.RunConfig fields needed for markdown
// generation. The executor package cannot import runner, so we define
// local types for JSON unmarshaling.
type markdownRunConfig struct {
	Timestamp                      int64               `json:"timestamp"`
	TimestampEnd                   int64               `json:"timestamp_end,omitempty"`
	SuiteHash                      string              `json:"suite_hash,omitempty"`
	SystemResourceCollectionMethod string              `json:"system_resource_collection_method,omitempty"`
	System                         *markdownSystemInfo `json:"system,omitempty"`
	Instance                       *markdownInstance   `json:"instance"`
	Metadata                       *markdownMetadata   `json:"metadata,omitempty"`
	StartBlock                     *markdownStartBlock `json:"start_block,omitempty"`
	TestCounts                     *markdownTestCounts `json:"test_counts,omitempty"`
	Status                         string              `json:"status,omitempty"`
	TerminationReason              string              `json:"termination_reason,omitempty"`
	ContainerExitCode              *int64              `json:"container_exit_code,omitempty"`
	ContainerOOMKilled             *bool               `json:"container_oom_killed,omitempty"`
}

type markdownSystemInfo struct {
	Hostname           string  `json:"hostname"`
	OS                 string  `json:"os"`
	Platform           string  `json:"platform"`
	PlatformVersion    string  `json:"platform_version"`
	KernelVersion      string  `json:"kernel_version"`
	Arch               string  `json:"arch"`
	Virtualization     string  `json:"virtualization,omitempty"`
	VirtualizationRole string  `json:"virtualization_role,omitempty"`
	CPUVendor          string  `json:"cpu_vendor"`
	CPUModel           string  `json:"cpu_model"`
	CPUCores           int     `json:"cpu_cores"`
	CPUMhz             float64 `json:"cpu_mhz"`
	CPUCacheKB         int     `json:"cpu_cache_kb"`
	MemoryTotalGB      float64 `json:"memory_total_gb"`
}

type markdownInstance struct {
	ID             string                  `json:"id"`
	Client         string                  `json:"client"`
	Image          string                  `json:"image"`
	ClientVersion  string                  `json:"client_version,omitempty"`
	ResourceLimits *markdownResourceLimits `json:"resource_limits,omitempty"`
}

type markdownResourceLimits struct {
	CpusetCpus    string  `json:"cpuset_cpus,omitempty"`
	Memory        string  `json:"memory,omitempty"`
	MemoryBytes   int64   `json:"memory_bytes,omitempty"`
	CPUFreqKHz    *uint64 `json:"cpu_freq_khz,omitempty"`
	CPUTurboBoost *bool   `json:"cpu_turboboost,omitempty"`
	CPUGovernor   string  `json:"cpu_freq_governor,omitempty"`
}

type markdownMetadata struct {
	Labels map[string]string `json:"labels,omitempty"`
}

type markdownStartBlock struct {
	Number    uint64 `json:"number"`
	Hash      string `json:"hash"`
	StateRoot string `json:"state_root"`
}

type markdownTestCounts struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

// failedTestInfo holds information about a single failed test.
type failedTestInfo struct {
	Name        string
	FailedSteps []string
}

// GenerateRunMarkdown generates a markdown summary for a single benchmark
// run directory. The output is capped at maxChars characters.
func GenerateRunMarkdown(
	runDir, runID string,
	maxChars int,
) (string, error) {
	// Parse config.json (required).
	configPath := filepath.Join(runDir, "config.json")

	configData, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("reading config.json: %w", err)
	}

	var cfg markdownRunConfig
	if err := json.Unmarshal(configData, &cfg); err != nil {
		return "", fmt.Errorf("parsing config.json: %w", err)
	}

	// Parse result.json (optional — may not exist for crashed runs).
	var result *RunResult

	resultPath := filepath.Join(runDir, "result.json")

	resultData, readErr := os.ReadFile(resultPath)
	if readErr == nil {
		var rr RunResult
		if err := json.Unmarshal(resultData, &rr); err == nil {
			result = &rr
		}
	}

	// Aggregate step stats and test counts.
	var (
		steps       *IndexStepsStats
		testsPassed int
		testsFailed int
		testsTotal  int
	)

	if result != nil {
		steps, testsPassed, testsFailed = AggregateStepStats(result)
		testsTotal = len(result.Tests)
	}

	// Override with config test_counts when available.
	if cfg.TestCounts != nil {
		testsTotal = cfg.TestCounts.Total
		testsPassed = cfg.TestCounts.Passed
		testsFailed = cfg.TestCounts.Failed
	}

	// Build markdown.
	var sb strings.Builder

	sb.Grow(4096)

	writeTitle(&sb, runID)
	writeOverview(&sb, &cfg)
	writeTestResults(&sb, testsTotal, testsPassed, testsFailed)
	writeStepStats(&sb, steps)
	writeStartBlock(&sb, cfg.StartBlock)
	writeSystem(&sb, cfg.System)
	writeResourceLimits(&sb, cfg.Instance)
	writeMetadata(&sb, cfg.Metadata)

	// Failed tests section is last — it gets truncated if needed.
	failed := collectFailedTests(result)
	writeFailedTests(&sb, failed, maxChars)

	return sb.String(), nil
}

func writeTitle(sb *strings.Builder, runID string) {
	fmt.Fprintf(sb, "# Benchmark Run: %s\n\n", runID)
}

func writeOverview(sb *strings.Builder, cfg *markdownRunConfig) {
	sb.WriteString("## Overview\n\n")
	sb.WriteString("| Field | Value |\n")
	sb.WriteString("|---|---|\n")

	if cfg.Status != "" {
		fmt.Fprintf(sb, "| Status | %s |\n", cfg.Status)
	}

	if cfg.TerminationReason != "" {
		fmt.Fprintf(sb, "| Termination Reason | %s |\n",
			cfg.TerminationReason)
	}

	if cfg.ContainerExitCode != nil {
		fmt.Fprintf(sb, "| Container Exit Code | %d |\n",
			*cfg.ContainerExitCode)
	}

	if cfg.ContainerOOMKilled != nil && *cfg.ContainerOOMKilled {
		sb.WriteString("| Container OOM Killed | yes |\n")
	}

	if cfg.Instance != nil {
		fmt.Fprintf(sb, "| Client | %s |\n", cfg.Instance.Client)
		fmt.Fprintf(sb, "| Image | `%s` |\n", cfg.Instance.Image)

		if cfg.Instance.ClientVersion != "" {
			fmt.Fprintf(sb, "| Client Version | %s |\n",
				cfg.Instance.ClientVersion)
		}
	}

	if cfg.Timestamp > 0 {
		t := time.Unix(cfg.Timestamp, 0).UTC()
		fmt.Fprintf(sb, "| Started | %s |\n",
			t.Format("2006-01-02 15:04:05 UTC"))
	}

	if cfg.TimestampEnd > 0 && cfg.Timestamp > 0 {
		dur := time.Duration(cfg.TimestampEnd-cfg.Timestamp) * time.Second
		fmt.Fprintf(sb, "| Duration | %s |\n", formatDuration(dur))
	}

	if cfg.SuiteHash != "" {
		fmt.Fprintf(sb, "| Suite Hash | `%s` |\n", cfg.SuiteHash)
	}

	sb.WriteByte('\n')
}

func writeSystem(sb *strings.Builder, sys *markdownSystemInfo) {
	if sys == nil {
		return
	}

	sb.WriteString("## System\n\n")
	sb.WriteString("| Field | Value |\n")
	sb.WriteString("|---|---|\n")

	if sys.Hostname != "" {
		fmt.Fprintf(sb, "| Hostname | %s |\n", sys.Hostname)
	}

	if sys.CPUModel != "" {
		fmt.Fprintf(sb, "| CPU | %s |\n", sys.CPUModel)
	}

	if sys.CPUCores > 0 {
		fmt.Fprintf(sb, "| Cores | %d |\n", sys.CPUCores)
	}

	if sys.CPUMhz > 0 {
		fmt.Fprintf(sb, "| CPU MHz | %.1f |\n", sys.CPUMhz)
	}

	if sys.MemoryTotalGB > 0 {
		fmt.Fprintf(sb, "| Memory | %.1f GB |\n", sys.MemoryTotalGB)
	}

	if sys.OS != "" {
		fmt.Fprintf(sb, "| OS | %s |\n", sys.OS)
	}

	if sys.Platform != "" {
		platform := sys.Platform
		if sys.PlatformVersion != "" {
			platform += " " + sys.PlatformVersion
		}

		fmt.Fprintf(sb, "| Platform | %s |\n", platform)
	}

	if sys.Arch != "" {
		fmt.Fprintf(sb, "| Arch | %s |\n", sys.Arch)
	}

	if sys.KernelVersion != "" {
		fmt.Fprintf(sb, "| Kernel | %s |\n", sys.KernelVersion)
	}

	sb.WriteByte('\n')
}

func writeResourceLimits(sb *strings.Builder, inst *markdownInstance) {
	if inst == nil || inst.ResourceLimits == nil {
		return
	}

	rl := inst.ResourceLimits

	// Check if there's anything to show.
	if rl.CpusetCpus == "" && rl.Memory == "" &&
		rl.CPUFreqKHz == nil && rl.CPUTurboBoost == nil &&
		rl.CPUGovernor == "" {
		return
	}

	sb.WriteString("## Resource Limits\n\n")
	sb.WriteString("| Field | Value |\n")
	sb.WriteString("|---|---|\n")

	if rl.CpusetCpus != "" {
		fmt.Fprintf(sb, "| CPU Set | %s |\n", rl.CpusetCpus)
	}

	if rl.Memory != "" {
		fmt.Fprintf(sb, "| Memory | %s |\n", rl.Memory)
	}

	if rl.CPUFreqKHz != nil {
		mhz := float64(*rl.CPUFreqKHz) / 1000.0
		fmt.Fprintf(sb, "| CPU Frequency | %.1f MHz |\n", mhz)
	}

	if rl.CPUTurboBoost != nil {
		val := "disabled"
		if *rl.CPUTurboBoost {
			val = "enabled"
		}

		fmt.Fprintf(sb, "| Turbo Boost | %s |\n", val)
	}

	if rl.CPUGovernor != "" {
		fmt.Fprintf(sb, "| CPU Governor | %s |\n", rl.CPUGovernor)
	}

	sb.WriteByte('\n')
}

func writeMetadata(sb *strings.Builder, md *markdownMetadata) {
	if md == nil || len(md.Labels) == 0 {
		return
	}

	sb.WriteString("## Metadata\n\n")
	sb.WriteString("| Label | Value |\n")
	sb.WriteString("|---|---|\n")

	// Sort keys for deterministic output.
	keys := make([]string, 0, len(md.Labels))
	for k := range md.Labels {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, k := range keys {
		fmt.Fprintf(sb, "| %s | %s |\n", k, md.Labels[k])
	}

	sb.WriteByte('\n')
}

func writeStartBlock(sb *strings.Builder, block *markdownStartBlock) {
	if block == nil {
		return
	}

	sb.WriteString("## Start Block\n\n")
	sb.WriteString("| Field | Value |\n")
	sb.WriteString("|---|---|\n")
	fmt.Fprintf(sb, "| Number | %d |\n", block.Number)

	if block.Hash != "" {
		fmt.Fprintf(sb, "| Hash | `%s` |\n", block.Hash)
	}

	if block.StateRoot != "" {
		fmt.Fprintf(sb, "| State Root | `%s` |\n", block.StateRoot)
	}

	sb.WriteByte('\n')
}

func writeTestResults(
	sb *strings.Builder,
	total, passed, failed int,
) {
	sb.WriteString("## Test Results\n\n")
	sb.WriteString("| Total | Passed | Failed |\n")
	sb.WriteString("|---|---|---|\n")
	fmt.Fprintf(sb, "| %d | %d | %d |\n\n", total, passed, failed)
}

func writeStepStats(sb *strings.Builder, steps *IndexStepsStats) {
	if steps == nil {
		return
	}

	// Check if there's any data to show.
	if steps.Setup == nil && steps.Test == nil && steps.Cleanup == nil {
		return
	}

	sb.WriteString("## Aggregated Step Stats\n\n")
	sb.WriteString("| Step | Duration | Gas Used | MGas/s " +
		"| Success | Fail |\n")
	sb.WriteString("|---|---|---|---|---|---|\n")

	writeStepRow(sb, "Setup", steps.Setup)
	writeStepRow(sb, "Test", steps.Test)
	writeStepRow(sb, "Cleanup", steps.Cleanup)

	sb.WriteByte('\n')
}

func writeStepRow(sb *strings.Builder, name string, s *IndexStepStats) {
	if s == nil {
		return
	}

	fmt.Fprintf(sb, "| %s | %s | %s | %s | %d | %d |\n",
		name,
		formatDurationNs(s.Duration),
		formatGas(s.GasUsed),
		formatMGasPerSec(s.GasUsed, s.GasUsedDuration),
		s.Success,
		s.Fail,
	)
}

func writeFailedTests(
	sb *strings.Builder,
	failed []failedTestInfo,
	maxChars int,
) {
	if len(failed) == 0 {
		return
	}

	sb.WriteString("## Failed Tests\n\n")
	sb.WriteString("| Test | Failed Steps |\n")
	sb.WriteString("|---|---|\n")

	// Reserve space for the truncation message.
	const reserveChars = 100

	for i, ft := range failed {
		row := fmt.Sprintf("| %s | %s |\n",
			ft.Name, strings.Join(ft.FailedSteps, ", "))

		// Check if adding this row would exceed maxChars.
		if maxChars > 0 && sb.Len()+len(row)+reserveChars > maxChars {
			remaining := len(failed) - i
			fmt.Fprintf(sb,
				"\n*%d more failed test(s) not shown "+
					"(output truncated at %d chars)*\n",
				remaining, maxChars)

			return
		}

		sb.WriteString(row)
	}
}

// collectFailedTests iterates the result and returns a sorted list of
// tests that have at least one failed step.
func collectFailedTests(result *RunResult) []failedTestInfo {
	if result == nil {
		return nil
	}

	failed := make([]failedTestInfo, 0)

	for name, test := range result.Tests {
		if test.Steps == nil {
			continue
		}

		var failedSteps []string

		if test.Steps.Setup != nil &&
			test.Steps.Setup.Aggregated != nil &&
			test.Steps.Setup.Aggregated.Failed > 0 {
			failedSteps = append(failedSteps, "setup")
		}

		if test.Steps.Test != nil &&
			test.Steps.Test.Aggregated != nil &&
			test.Steps.Test.Aggregated.Failed > 0 {
			failedSteps = append(failedSteps, "test")
		}

		if test.Steps.Cleanup != nil &&
			test.Steps.Cleanup.Aggregated != nil &&
			test.Steps.Cleanup.Aggregated.Failed > 0 {
			failedSteps = append(failedSteps, "cleanup")
		}

		if len(failedSteps) > 0 {
			failed = append(failed, failedTestInfo{
				Name:        name,
				FailedSteps: failedSteps,
			})
		}
	}

	sort.Slice(failed, func(i, j int) bool {
		return failed[i].Name < failed[j].Name
	})

	return failed
}

// formatDuration formats a time.Duration as a human-readable string.
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return d.String()
	}

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}

	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}

	return fmt.Sprintf("%ds", seconds)
}

// formatDurationNs formats nanoseconds as a human-readable duration.
func formatDurationNs(ns int64) string {
	return formatDuration(time.Duration(ns))
}

// formatGas formats a gas value with comma separators.
func formatGas(gas uint64) string {
	if gas == 0 {
		return "0"
	}

	s := fmt.Sprintf("%d", gas)
	n := len(s)

	if n <= 3 {
		return s
	}

	// Insert commas from the right.
	var b strings.Builder

	b.Grow(n + (n-1)/3)

	for i, ch := range s {
		if i > 0 && (n-i)%3 == 0 {
			b.WriteByte(',')
		}

		b.WriteRune(ch)
	}

	return b.String()
}

// formatMGasPerSec computes and formats MGas/s from gas and duration in ns.
func formatMGasPerSec(gas uint64, durationNs int64) string {
	if gas == 0 || durationNs <= 0 {
		return "-"
	}

	mgasPerSec := float64(gas) / 1_000_000.0 /
		(float64(durationNs) / 1_000_000_000.0)

	return fmt.Sprintf("%.2f", mgasPerSec)
}
