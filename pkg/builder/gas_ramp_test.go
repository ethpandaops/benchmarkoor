package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeFiller is a minimal geth for bumpGasLimit: one chain, blocks built by
// testing_buildBlockV1 with geth's gas limit rule, canonical once submitted.
type fakeFiller struct {
	mu         sync.Mutex
	number     uint64
	gasLimit   uint64
	slot       *uint64 // nil: the head is pre-Amsterdam
	amsterdam  bool    // build Amsterdam blocks (honour targetGasLimit)
	minerCeil  uint64
	built      map[string]uint64 // blockHash -> gasLimit
	attrs      []map[string]any
	nextSlotOf map[string]*uint64
}

// calcGasLimit is geth's core.CalcGasLimit.
func calcGasLimit(parent, desired uint64) uint64 {
	delta := parent/1024 - 1
	if parent < desired {
		return min(parent+delta, desired)
	}

	if parent > desired {
		return max(parent-delta, desired)
	}

	return parent
}

func (f *fakeFiller) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	var req struct {
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	_ = json.Unmarshal(body, &req)

	f.mu.Lock()
	defer f.mu.Unlock()

	var result any

	switch req.Method {
	case "eth_getBlockByNumber":
		head := map[string]any{
			"hash":      fmt.Sprintf("0x%064x", f.number),
			"timestamp": uintToHex(1000 + f.number),
			"gasLimit":  uintToHex(f.gasLimit),
		}
		if f.slot != nil {
			head["slotNumber"] = uintToHex(*f.slot)
		}

		result = head
	case "testing_buildBlockV1":
		var attrs map[string]any
		_ = json.Unmarshal(req.Params[1], &attrs)
		f.attrs = append(f.attrs, attrs)

		desired := f.minerCeil
		if t, ok := attrs["targetGasLimit"].(string); ok && f.amsterdam {
			desired, _ = hexToUint64(t)
		}

		gl := calcGasLimit(f.gasLimit, desired)
		hash := fmt.Sprintf("0x%064x", f.number+1)
		f.built[hash] = gl

		var slot *uint64
		if s, ok := attrs["slotNumber"].(string); ok {
			v, _ := hexToUint64(s)
			slot = &v
		}

		f.nextSlotOf[hash] = slot
		result = map[string]any{
			"executionPayload":  map[string]any{"blockHash": hash, "gasLimit": uintToHex(gl)},
			"executionRequests": []string{},
		}
	case "engine_newPayloadV4", "engine_newPayloadV5":
		result = map[string]any{"status": "VALID"}
	case "engine_forkchoiceUpdatedV3":
		var fc struct {
			Head string `json:"headBlockHash"`
		}
		_ = json.Unmarshal(req.Params[0], &fc)
		f.number++
		f.gasLimit = f.built[fc.Head]
		f.slot = f.nextSlotOf[fc.Head]
		result = map[string]any{"payloadStatus": map[string]any{"status": "VALID"}}
	default:
		http.Error(w, "unexpected method "+req.Method, http.StatusBadRequest)

		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
}

func newFakeFillerClient(t *testing.T, f *fakeFiller, fork string) *engineClient {
	t.Helper()

	f.built, f.nextSlotOf = map[string]uint64{}, map[string]*uint64{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)

	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)

	c, err := newEngineClient(u.Hostname(), port, port, "0x"+fmt.Sprintf("%064x", 1), fork)
	require.NoError(t, err)

	return c
}

func TestBumpGasLimit_RampsDownOnAmsterdam(t *testing.T) {
	slot := uint64(41_000)
	f := &fakeFiller{number: 100, gasLimit: 2_000_000_000, slot: &slot, amsterdam: true, minerCeil: 1_000_000_000_000}
	c := newFakeFillerClient(t, f, "amsterdam")

	built, err := c.bumpGasLimit(context.Background(), 1_000_000_000, 10_000, logrus.New())
	require.NoError(t, err)

	assert.Equal(t, uint64(1_000_000_000), f.gasLimit, "lands exactly on the target")
	assert.Greater(t, built, 600, "1/1024 per block: ~710 blocks to halve")

	for i, a := range f.attrs {
		require.Equal(t, uintToHex(1_000_000_000), a["targetGasLimit"], "block %d carries the CL target", i)
		require.Equal(t, uintToHex(41_001+uint64(i)), a["slotNumber"], "block %d continues the head's slot", i)
	}

}

// The non-predeploy pre-run builds its funding block after the bump: it must not
// climb back toward the filler's miner ceiling.
func TestBumpGasLimit_RampDownHoldsForLaterBlocks(t *testing.T) {
	slot := uint64(7)
	f := &fakeFiller{number: 100, gasLimit: 400_000_000, slot: &slot, amsterdam: true, minerCeil: 1_000_000_000_000}
	c := newFakeFillerClient(t, f, "amsterdam")

	_, err := c.bumpGasLimit(context.Background(), 200_000_000, 10_000, logrus.New())
	require.NoError(t, err)

	_, gl, err := c.buildBlock(context.Background(), []withdrawal{{Index: "0x1", ValidatorIndex: "0x1",
		Address: "0x7e5f4552091a69125d5dfcb7b8c2659029395bdf", Amount: "0x1"}}, nil)
	require.NoError(t, err)
	assert.Equal(t, uint64(200_000_000), gl, "the funding block keeps the ramped-down limit")
}

func TestBumpGasLimit_RampDownRefusedBeforeAmsterdam(t *testing.T) {
	f := &fakeFiller{number: 100, gasLimit: 2_000_000_000, minerCeil: 1_000_000_000_000}
	c := newFakeFillerClient(t, f, "osaka")

	built, err := c.bumpGasLimit(context.Background(), 1_000_000_000, 10_000, logrus.New())
	require.ErrorContains(t, err, "needs an Amsterdam block")
	assert.Equal(t, 1, built, "fails on the first block, not after the cap")
}

func TestBumpGasLimit_UpIsUnchanged(t *testing.T) {
	f := &fakeFiller{number: 100, gasLimit: 60_000_000, minerCeil: 1_000_000_000_000}
	c := newFakeFillerClient(t, f, "osaka")

	_, err := c.bumpGasLimit(context.Background(), 70_000_000, 10_000, logrus.New())
	require.NoError(t, err)

	assert.GreaterOrEqual(t, f.gasLimit, uint64(70_000_000))

	for _, a := range f.attrs {
		assert.NotContains(t, a, "targetGasLimit", "a ramp up leaves the payload attributes as they were")
	}
}

func TestBumpGasLimit_AtTargetBuildsNothing(t *testing.T) {
	slot := uint64(5)
	f := &fakeFiller{number: 100, gasLimit: 200_000_000, slot: &slot, amsterdam: true, minerCeil: 1_000_000_000_000}
	c := newFakeFillerClient(t, f, "amsterdam")

	built, err := c.bumpGasLimit(context.Background(), 200_000_000, 10_000, logrus.New())
	require.NoError(t, err)
	assert.Zero(t, built)
	assert.Empty(t, f.attrs)
}

// A base_bundle is replayed before the bump, so the ramp starts from its head.
func TestReplayBaseBundle_RampContinuesFromItsHead(t *testing.T) {
	slot := uint64(9)
	f := &fakeFiller{number: 100, gasLimit: 60_000_000, slot: &slot, amsterdam: true, minerCeil: 1_000_000_000_000}
	c := newFakeFillerClient(t, f, "amsterdam")

	head := fmt.Sprintf("0x%064x", 0xbeef)
	f.built[head] = 400_000_000
	f.nextSlotOf[head] = &slot

	dir := t.TempDir()
	bundle := filepath.Join(dir, preRunBundleFile)
	require.NoError(t, os.WriteFile(bundle, []byte(
		`{"jsonrpc":"2.0","id":1,"method":"engine_newPayloadV5","params":[{"blockHash":"`+head+`"}]}`+"\n"+
			`{"jsonrpc":"2.0","id":2,"method":"engine_forkchoiceUpdatedV3","params":[{"headBlockHash":"`+head+`"}]}`+"\n"), 0o644))

	b := &PreRunsBuilder{cfg: &config.PreRunsConfig{}}
	require.NoError(t, b.replayBaseBundle(context.Background(), logrus.New(), &bootedFiller{ec: c}, dir))
	assert.Equal(t, uint64(400_000_000), f.gasLimit, "the bundle's head is the chain head")
	assert.Empty(t, c.recorded, "replayed blocks are not re-recorded")

	_, err := c.bumpGasLimit(context.Background(), 200_000_000, 10_000, logrus.New())
	require.NoError(t, err)
	assert.Equal(t, uint64(200_000_000), f.gasLimit)

	require.NoError(t, os.WriteFile(bundle, nil, 0o644))
	require.ErrorContains(t, b.replayBaseBundle(context.Background(), logrus.New(), &bootedFiller{ec: c}, dir), "is empty")
}
