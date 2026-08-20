package executor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTempoSuiteSourcePrepare(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "genesis.json"), []byte(`{"config":{}}`), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "blocks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "blocks", "1.rlp"), []byte("0xc0"), 0o644))

	manifest := `
format: tempo-engine-suite/v1
name: tempo-aa
description: deterministic AA block
origin:
  kind: tempo-native
  revision: abc123
  seed: "7"
chain:
  name: tempo
  chain_id: 42431
  hardfork: presto
  genesis: genesis.json
defaults:
  wait_for_persistence: true
  wait_for_caches: false
tests:
  - name: aa/direct
    description: direct signature
    tags: [aa, auth-direct]
    metadata:
      preset: tip20
    test:
      - rlp_file: blocks/1.rlp
        block_number: 1
        block_hash: "0x01"
        gas_used: 42000000
        transaction_count: 2
      - method: reth_forkchoiceUpdated
        params:
          - headBlockHash: "0x01"
            safeBlockHash: "0x01"
            finalizedBlockHash: "0x01"
`
	manifestPath := filepath.Join(dir, "manifest.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifest), 0o644))

	filter, err := CompileFilter("auth-direct")
	require.NoError(t, err)
	source := NewTempoSuiteSource(logrus.New(), &config.TempoSuiteSource{Manifest: manifestPath}, filter)

	prepared, err := source.Prepare(context.Background())
	require.NoError(t, err)
	require.Len(t, prepared.Tests, 1)
	assert.Equal(t, []string{"aa", "auth-direct"}, prepared.Tests[0].Tags)
	assert.Equal(t, "tip20", prepared.Tests[0].Metadata["preset"])
	require.NotNil(t, prepared.Tests[0].Test)

	provider := prepared.Tests[0].Test.Provider
	require.Len(t, provider.Lines(), 2)
	var request struct {
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	require.NoError(t, json.Unmarshal([]byte(provider.Lines()[0]), &request))
	assert.Equal(t, "reth_newPayload", request.Method)
	require.Len(t, request.Params, 3)
	assert.JSONEq(t, `"0xc0"`, string(request.Params[0]))
	assert.JSONEq(t, `true`, string(request.Params[1]))
	assert.JSONEq(t, `false`, string(request.Params[2]))

	metadata := provider.(RequestMetadataProvider).RequestMetadata()
	require.Len(t, metadata, 2)
	require.NotNil(t, metadata[0].GasUsed)
	assert.Equal(t, uint64(42_000_000), *metadata[0].GasUsed)
	assert.Equal(t, "VALID", metadata[0].ExpectedStatus)

	genesisProvider := source.(GenesisProvider)
	assert.Equal(t, filepath.Join(dir, "genesis.json"), genesisProvider.GetGenesisPath("tempo"))
	info, err := source.GetSourceInfo()
	require.NoError(t, err)
	require.NotNil(t, info.Tempo)
	assert.Equal(t, "abc123", info.Tempo.Origin.Revision)
	assert.NotEmpty(t, prepared.IdentityContent)
}

func TestTempoSuiteSourceRejectsUnknownField(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(`
format: tempo-engine-suite/v1
name: invalid
unknown: true
chain:
  genesis: genesis.json
tests:
  - name: x
    test:
      - method: reth_forkchoiceUpdated
        params: []
`), 0o644))

	filter, err := CompileFilter("")
	require.NoError(t, err)
	source := NewTempoSuiteSource(logrus.New(), &config.TempoSuiteSource{Manifest: manifestPath}, filter)
	_, err = source.Prepare(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "field unknown not found")
}

func TestTempoSuiteSourceRejectsRawOnlyOptionsOnGenericCall(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "genesis.json"), []byte(`{}`), 0o644))
	manifestPath := filepath.Join(dir, "manifest.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(`
format: tempo-engine-suite/v1
name: invalid-call
chain:
  genesis: genesis.json
tests:
  - name: x
    test:
      - method: reth_forkchoiceUpdated
        params: []
        bal: "0xc0"
`), 0o644))

	filter, err := CompileFilter("")
	require.NoError(t, err)
	source := NewTempoSuiteSource(logrus.New(), &config.TempoSuiteSource{Manifest: manifestPath}, filter)
	_, err = source.Prepare(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bal/bal_file requires rlp or rlp_file")
}
