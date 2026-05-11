package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inlineProvider satisfies the StepProvider interface for tests.
type inlineProvider struct {
	lines []string
}

func (p *inlineProvider) Lines() []string { return p.lines }
func (p *inlineProvider) Content() []byte { return []byte(joinLines(p.lines)) }

func joinLines(ls []string) string {
	return strings.Join(ls, "\n") + "\n"
}

func TestCreateSuiteOutput_WritesPayloadSizes(t *testing.T) {
	tmp := t.TempDir()
	// One test step with one engine_newPayloadV3 line.
	testLine := minimalDenebRequest(t)
	prepared := &PreparedSource{
		Tests: []*TestWithSteps{
			{
				Name: "test_payload_sizes",
				Test: &StepFile{
					Name:     "test_payload_sizes",
					Provider: &inlineProvider{lines: []string{testLine}},
				},
			},
		},
	}
	info := &SuiteInfo{
		Hash: "deadbeef",
	}
	log := logrus.New()
	err := CreateSuiteOutput(log, tmp, "deadbeef", info, prepared, nil)
	require.NoError(t, err)

	summaryPath := filepath.Join(tmp, "suites", "deadbeef", "summary.json")
	data, err := os.ReadFile(summaryPath)
	require.NoError(t, err)

	var parsed SuiteInfo
	require.NoError(t, json.Unmarshal(data, &parsed))
	require.Len(t, parsed.Tests, 1)
	assert.Greater(t, parsed.Tests[0].PayloadSizeBytes, uint64(100))
	assert.Greater(t, parsed.Tests[0].PayloadSizeBytesSnappy, uint64(0))
	assert.LessOrEqual(t, parsed.Tests[0].PayloadSizeBytesSnappy, parsed.Tests[0].PayloadSizeBytes)
}

func TestSuiteInfo_BackwardCompat_LoadsOldSummary(t *testing.T) {
	// Construct an old-style summary.json (no payload-size fields) and verify it parses.
	old := `{
		"hash": "f00d",
		"tests": [{"name": "test_old", "opcode_count": {"PUSH1": 3}}]
	}`
	var parsed SuiteInfo
	require.NoError(t, json.Unmarshal([]byte(old), &parsed))
	require.Len(t, parsed.Tests, 1)
	assert.Equal(t, "test_old", parsed.Tests[0].Name)
	assert.Equal(t, uint64(0), parsed.Tests[0].PayloadSizeBytes)
}

func TestCreateSuiteOutput_MergesPayloadSizesOnSecondRun(t *testing.T) {
	tmp := t.TempDir()
	testLine := minimalDenebRequest(t)
	prepared := &PreparedSource{
		Tests: []*TestWithSteps{
			{
				Name: "test_payload_merge",
				Test: &StepFile{
					Name:     "test_payload_merge",
					Provider: &inlineProvider{lines: []string{testLine}},
				},
			},
		},
	}

	// First run — creates the suite and writes initial sizes.
	log := logrus.New()
	info1 := &SuiteInfo{Hash: "cafef00d"}
	require.NoError(t, CreateSuiteOutput(log, tmp, "cafef00d", info1, prepared, nil))

	// Simulate a legacy summary: rewrite the file with the payload-size fields zeroed.
	summaryPath := filepath.Join(tmp, "suites", "cafef00d", "summary.json")
	data, err := os.ReadFile(summaryPath)
	require.NoError(t, err)
	var legacy SuiteInfo
	require.NoError(t, json.Unmarshal(data, &legacy))
	for i := range legacy.Tests {
		legacy.Tests[i].PayloadSizeBytes = 0
		legacy.Tests[i].PayloadSizeBytesSnappy = 0
		legacy.Tests[i].BALSizeBytes = 0
	}
	zeroed, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(summaryPath, zeroed, 0644))

	// Second run — should detect suite exists, read on-disk test.request, and merge.
	info2 := &SuiteInfo{Hash: "cafef00d"}
	require.NoError(t, CreateSuiteOutput(log, tmp, "cafef00d", info2, prepared, nil))

	final, err := os.ReadFile(summaryPath)
	require.NoError(t, err)
	var parsed SuiteInfo
	require.NoError(t, json.Unmarshal(final, &parsed))
	require.Len(t, parsed.Tests, 1)
	assert.Greater(t, parsed.Tests[0].PayloadSizeBytes, uint64(100), "merge path should backfill sizes")
}
