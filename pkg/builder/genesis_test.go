package builder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeGenesisFixture(t *testing.T, timestamp string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "geth-genesis.json")
	body := `{"timestamp":"` + timestamp + `","config":{"chainId":1337,"osakaTime":0}}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

	return path
}

func TestForkOverrideActivationFlag(t *testing.T) {
	tests := []struct {
		name      string
		timestamp string
		fork      string
		want      string
	}{
		{name: "zero timestamp", timestamp: "0x0", fork: "amsterdam", want: "--override.amsterdam=1"},
		{name: "hex timestamp", timestamp: "0x10", fork: "amsterdam", want: "--override.amsterdam=17"},
		{name: "fork lowercased", timestamp: "0x0", fork: "Amsterdam", want: "--override.amsterdam=1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			flag, err := forkOverrideActivationFlag(writeGenesisFixture(t, tc.timestamp), tc.fork)
			require.NoError(t, err)
			assert.Equal(t, tc.want, flag)
		})
	}
}

func TestForkOverrideActivationFlag_Errors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		_, err := forkOverrideActivationFlag(filepath.Join(t.TempDir(), "nope.json"), "amsterdam")
		require.Error(t, err)
	})

	t.Run("missing timestamp", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "g.json")
		require.NoError(t, os.WriteFile(p, []byte(`{"config":{}}`), 0o644))
		_, err := forkOverrideActivationFlag(p, "amsterdam")
		require.ErrorContains(t, err, "timestamp")
	})
}

func TestGenesisFileTimestamp(t *testing.T) {
	assert := assert.New(t)

	hexTS, err := genesisFileTimestamp(writeGenesisFixture(t, "0xff"))
	require.NoError(t, err)
	assert.EqualValues(255, hexTS)

	decPath := filepath.Join(t.TempDir(), "g.json")
	require.NoError(t, os.WriteFile(decPath, []byte(`{"timestamp":"42","config":{}}`), 0o644))
	decTS, err := genesisFileTimestamp(decPath)
	require.NoError(t, err)
	assert.EqualValues(42, decTS)
}
