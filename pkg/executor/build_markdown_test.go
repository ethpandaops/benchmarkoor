package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeBuildSummary(t *testing.T, dir string, summary BuildSummary) string {
	t.Helper()

	data, err := json.Marshal(summary)
	require.NoError(t, err)

	p := filepath.Join(dir, "build-summary.json")
	require.NoError(t, os.WriteFile(p, data, 0o600))

	return p
}

func TestGenerateBuildMarkdown(t *testing.T) {
	dir := t.TempDir()
	saDir := filepath.Join(dir, "sa")
	eestDir := filepath.Join(dir, "eest")
	require.NoError(t, os.MkdirAll(saDir, 0o755))
	require.NoError(t, os.MkdirAll(eestDir, 0o755))

	manifest := `{"flags":{"client":"nethermind","fork":"osaka","seed":1234,"chain_id":1337,"gas_limit":300000000,"target_size":"256MB"},` +
		`"result":{"state_root":"0x120a6b331ed0fa9bec93ce22ab765a057b4e7c707d0fd97388fcd4316ed65498",` +
		`"accounts_created":301540,"contracts_created":5248,"storage_slots":1351914,"total_db_size_bytes":526000000,"elapsed_ms":4348}}`
	require.NoError(t, os.WriteFile(filepath.Join(saDir, stateActorManifestFile), []byte(manifest), 0o600))

	fill := `{"source_dir":"/schelk/state-actor/v1/nethermind","filler_client":"geth","filler_image":"ethpandaops/geth:master","eest_sha":"27174ca1b2c3deadbeef","fork":"osaka","filter":"bn128","size_bytes":78643200,"filled":36,"failed":0}`
	require.NoError(t, os.WriteFile(filepath.Join(eestDir, eestFillResultFile), []byte(fill), 0o600))

	p := writeBuildSummary(t, dir, BuildSummary{
		GeneratedAt: "2026-07-06T00:00:00Z",
		Targets: []BuildTargetSummary{
			{Builder: "state-actor", Name: "nethermind", Client: "nethermind", OutputDir: saDir, Status: "OK", ElapsedMs: 4348},
			{Builder: "eest-payloads", Name: "payload-generator-geth", Client: "geth", OutputDir: eestDir, Status: "OK", ElapsedMs: 30000},
		},
	})

	md, err := GenerateBuildMarkdown(p, 65000)
	require.NoError(t, err)

	// per-target sections, each a Field | Value table
	assert.Contains(t, md, "### ✅ nethermind — state-actor")
	assert.Contains(t, md, "### ✅ payload-generator-geth — eest-payloads")
	assert.Contains(t, md, "| Field | Value |")
	// state-actor manifest enrichment (counts use thousands separators)
	assert.Contains(t, md, "| Accounts created | 301,540 |")
	assert.Contains(t, md, "| Storage slots | 1,351,914 |")
	assert.Contains(t, md, "0x120a6b33") // short state root prefix
	// eest provenance + fill counts + newly added filler image / EEST commit
	assert.Contains(t, md, "| Source | /schelk/state-actor/v1/nethermind |")
	assert.Contains(t, md, "| Filler image | `ethpandaops/geth:master` |")
	assert.Contains(t, md, "| EEST commit | `27174ca1b2` |")
	assert.Contains(t, md, "| Filled | 36 |")
	assert.Contains(t, md, "| Failed | 0 |")
	assert.Contains(t, md, "| Fixtures size | 75.0 MiB |")
}

func TestGenerateBuildMarkdown_ErrTargetShowsError(t *testing.T) {
	dir := t.TempDir()

	p := writeBuildSummary(t, dir, BuildSummary{
		Targets: []BuildTargetSummary{
			{Builder: "eest-payloads", Name: "besu", OutputDir: filepath.Join(dir, "nope"), Status: "ERR", Error: "fill failed"},
		},
	})

	md, err := GenerateBuildMarkdown(p, 65000)
	require.NoError(t, err)
	assert.Contains(t, md, "### ❌ besu — eest-payloads")
	assert.Contains(t, md, "| Status | Failed |")
	assert.Contains(t, md, "> ❌ fill failed")
}
