package scenario

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEveryCheckIsExplained checks that every check name seen in a full run
// of all scenarios on all clients has an explanation. testdata/check_names.txt
// lists them; add new names there with their explanation.
func TestEveryCheckIsExplained(t *testing.T) {
	data, err := os.ReadFile("testdata/check_names.txt")
	require.NoError(t, err)

	for _, name := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		e := Explain(name)
		if assert.NotNil(t, e, name) {
			assert.NotEmpty(t, e.What, name)
			assert.NotEmpty(t, e.Why, name)
			assert.NotEmpty(t, e.OK, name)
		}
	}
}

// TestEveryObservationIsExplained does the same for observation keys.
func TestEveryObservationIsExplained(t *testing.T) {
	data, err := os.ReadFile("testdata/observation_keys.txt")
	require.NoError(t, err)

	for _, key := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		e := ExplainObservation(key)
		if assert.NotNil(t, e, key) {
			assert.NotEmpty(t, e.What, key)
			assert.NotEmpty(t, e.Why, key)
			assert.NotEmpty(t, e.OK, key)
		}
	}
}

// TestEveryMetricAndLatencyIsExplained does the same for metric and latency
// keys.
func TestEveryMetricAndLatencyIsExplained(t *testing.T) {
	for file, fn := range map[string]func(string) *Explanation{
		"testdata/metric_keys.txt":  ExplainMetric,
		"testdata/latency_keys.txt": ExplainLatency,
	} {
		data, err := os.ReadFile(file)
		require.NoError(t, err)

		for _, key := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			e := fn(key)
			if assert.NotNil(t, e, file+": "+key) {
				assert.NotEmpty(t, e.What, key)
				assert.NotEmpty(t, e.OK, key)
			}
		}
	}
}
