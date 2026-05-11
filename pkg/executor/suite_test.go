package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	out := ""
	for _, l := range ls {
		out += l + "\n"
	}
	return out
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

func TestCreateSuiteOutput_BackwardCompat_LoadsOldSummary(t *testing.T) {
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
