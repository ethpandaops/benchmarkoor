package executor

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/eest"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractNewPayloadLines(t *testing.T) {
	lines := []string{
		`{"jsonrpc":"2.0","method":"engine_newPayloadV3","params":[{"blockNumber":"0x1"}],"id":1}`,
		`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3","params":[{},null],"id":2}`,
		`{"jsonrpc":"2.0","method":"engine_newPayloadV4","params":[{"blockNumber":"0x2"}],"id":3}`,
	}
	got := ExtractNewPayloadLines(lines)
	assert.Len(t, got, 2)
	assert.Equal(t, 3, got[0].Version)
	assert.Equal(t, 4, got[1].Version)
}

func TestExtractNewPayloadLines_IgnoresMalformed(t *testing.T) {
	lines := []string{
		`{"jsonrpc":"2.0","method":"engine_newPayloadV3","params":[{}],"id":1}`,
		`not even json`,
		`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3","params":[],"id":2}`,
		`{"jsonrpc":"2.0","method":"engine_newPayloadVabc","params":[{}],"id":3}`,
		`{"jsonrpc":"2.0","method":"engine_newPayloadV0","params":[{}],"id":4}`,
	}
	got := ExtractNewPayloadLines(lines)
	assert.Len(t, got, 1)
}

func TestExtractNewPayloadLines_NoLines(t *testing.T) {
	got := ExtractNewPayloadLines(nil)
	assert.Empty(t, got)
}

// minimalDenebRequest returns a JSON-RPC line for engine_newPayloadV3 with
// the given BAL (post-decode bytes) and otherwise minimal fields.
func minimalDenebRequest(t *testing.T) string {
	t.Helper()
	ep := eest.ExecutionPayload{
		ParentHash:    "0x" + hexN("11", 32),
		FeeRecipient:  "0x" + hexN("22", 20),
		StateRoot:     "0x" + hexN("33", 32),
		ReceiptsRoot:  "0x" + hexN("44", 32),
		LogsBloom:     "0x" + hexN("00", 256),
		PrevRandao:    "0x" + hexN("55", 32),
		BlockNumber:   "0x10",
		GasLimit:      "0x1000000",
		GasUsed:       "0x800000",
		Timestamp:     "0x65000000",
		ExtraData:     "0x",
		BaseFeePerGas: "0x7",
		BlockHash:     "0x" + hexN("66", 32),
		Transactions:  []string{},
		Withdrawals:   []*eest.Withdrawal{},
	}
	epJSON, err := json.Marshal(ep)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_newPayloadV3","params":[%s],"id":1}`, epJSON)
}

func hexN(pair string, n int) string {
	out := make([]byte, n*2)
	for i := range n {
		out[2*i], out[2*i+1] = pair[0], pair[1]
	}
	return string(out)
}

func TestComputePayloadSizeBuckets_SingleNewPayload(t *testing.T) {
	log := logrus.New()
	lines := []string{minimalDenebRequest(t)}
	sizes := ComputePayloadSizeBuckets(log, "test_x", lines)
	require.Len(t, sizes.SSZFull, 1)
	require.Len(t, sizes.SSZFullSnappy, 1)
	require.Len(t, sizes.SSZBAL, 1)
	require.Len(t, sizes.JSONFull, 1)
	require.Len(t, sizes.JSONBAL, 1)
	assert.Greater(t, sizes.SSZFull[0], uint64(100))
	assert.Greater(t, sizes.SSZFullSnappy[0], uint64(0))
	assert.LessOrEqual(t, sizes.SSZFullSnappy[0], sizes.SSZFull[0])
	assert.Equal(t, uint64(0), sizes.SSZBAL[0])  // Deneb has no BAL
	assert.Equal(t, uint64(0), sizes.JSONBAL[0]) // Deneb has no BAL
	// JSON is verbose vs SSZ — same payload should be strictly larger in JSON.
	assert.Greater(t, sizes.JSONFull[0], sizes.SSZFull[0])
}

func TestComputePayloadSizeBuckets_NoNewPayloadLines(t *testing.T) {
	log := logrus.New()
	sizes := ComputePayloadSizeBuckets(log, "test_x", []string{
		`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3","params":[],"id":1}`,
	})
	assert.Empty(t, sizes.SSZFull)
	assert.Empty(t, sizes.SSZFullSnappy)
	assert.Empty(t, sizes.SSZBAL)
	assert.Empty(t, sizes.JSONFull)
	assert.Empty(t, sizes.JSONBAL)
	assert.False(t, sizes.HasData())
}

func TestComputePayloadSizeBuckets_BAL(t *testing.T) {
	log := logrus.New()
	balContent := hexN("ab", 1024) // 1024 bytes when decoded
	balHex := "0x" + balContent
	ep := eest.ExecutionPayload{
		ParentHash:      "0x" + hexN("11", 32),
		FeeRecipient:    "0x" + hexN("22", 20),
		StateRoot:       "0x" + hexN("33", 32),
		ReceiptsRoot:    "0x" + hexN("44", 32),
		LogsBloom:       "0x" + hexN("00", 256),
		PrevRandao:      "0x" + hexN("55", 32),
		BlockNumber:     "0x10",
		GasLimit:        "0x1000000",
		GasUsed:         "0x800000",
		Timestamp:       "0x65000000",
		ExtraData:       "0x",
		BaseFeePerGas:   "0x7",
		BlockHash:       "0x" + hexN("66", 32),
		Transactions:    []string{},
		Withdrawals:     []*eest.Withdrawal{},
		BlockAccessList: balHex,
	}
	epJSON, _ := json.Marshal(ep)
	// Use V6 specifically — BAL was introduced in Gloas (V5/V6 in our converter).
	line := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_newPayloadV6","params":[%s],"id":1}`, epJSON)
	sizes := ComputePayloadSizeBuckets(log, "test_x", []string{line})
	require.Len(t, sizes.SSZBAL, 1)
	require.Len(t, sizes.SSZFull, 1)
	require.Len(t, sizes.JSONBAL, 1)
	assert.Equal(t, uint64(1024), sizes.SSZBAL[0])
	assert.Greater(t, sizes.SSZFull[0], uint64(1024))
	// JSONBAL = hex chars: 2 per byte + "0x" prefix = 2*1024 + 2 = 2050.
	assert.Equal(t, uint64(2050), sizes.JSONBAL[0])
}

func TestComputePayloadSizeBuckets_OneEntryPerNewPayload(t *testing.T) {
	log := logrus.New()
	line := minimalDenebRequest(t)
	single := ComputePayloadSizeBuckets(log, "test_x", []string{line})
	double := ComputePayloadSizeBuckets(log, "test_x", []string{line, line})
	require.Len(t, single.SSZFull, 1)
	require.Len(t, double.SSZFull, 2)
	// Identical input lines produce identical per-block values.
	assert.Equal(t, single.SSZFull[0], double.SSZFull[0])
	assert.Equal(t, single.SSZFull[0], double.SSZFull[1])
	assert.Equal(t, single.SSZFullSnappy[0], double.SSZFullSnappy[0])
	assert.Equal(t, single.SSZFullSnappy[0], double.SSZFullSnappy[1])
}

func TestMergePayloadSizes_FillsMissing(t *testing.T) {
	existing := []SuiteTest{
		{Name: "test_a"},
		{Name: "test_b"},
	}
	lineProvider := func(name string, step StepKind) []string {
		if step != StepKindTest {
			return nil
		}
		switch name {
		case "test_a":
			return []string{minimalDenebRequest(t)}
		default:
			return nil
		}
	}
	log := logrus.New()
	MergePayloadSizes(log, existing, lineProvider)
	require.NotNil(t, existing[0].PayloadSizes)
	require.NotNil(t, existing[0].PayloadSizes.Test)
	assert.Greater(t, existing[0].PayloadSizes.Test.SSZFull[0], uint64(0))
	assert.Nil(t, existing[1].PayloadSizes)
}

func TestMergePayloadSizes_SkipsAlreadyPopulated(t *testing.T) {
	preExisting := &PayloadSizes{
		Test: &PayloadSizeBuckets{SSZFull: []uint64{999}, SSZFullSnappy: []uint64{100}, SSZBAL: []uint64{50}},
	}
	existing := []SuiteTest{
		{Name: "test_a", PayloadSizes: preExisting},
	}
	called := false
	lineProvider := func(name string, step StepKind) []string {
		called = true
		return []string{minimalDenebRequest(t)}
	}
	log := logrus.New()
	MergePayloadSizes(log, existing, lineProvider)
	assert.False(t, called, "should not invoke provider for already-populated entries")
	assert.Equal(t, uint64(999), existing[0].PayloadSizes.Test.SSZFull[0])
}

func TestMergePayloadSizes_PerStep(t *testing.T) {
	existing := []SuiteTest{{Name: "test_a"}}
	lineProvider := func(name string, step StepKind) []string {
		switch step {
		case StepKindSetup:
			return []string{minimalDenebRequest(t)}
		case StepKindTest:
			return []string{minimalDenebRequest(t), minimalDenebRequest(t)}
		default:
			return nil
		}
	}
	log := logrus.New()
	MergePayloadSizes(log, existing, lineProvider)
	require.NotNil(t, existing[0].PayloadSizes)
	require.NotNil(t, existing[0].PayloadSizes.Setup)
	require.NotNil(t, existing[0].PayloadSizes.Test)
	assert.Nil(t, existing[0].PayloadSizes.Cleanup)
	assert.Len(t, existing[0].PayloadSizes.Setup.SSZFull, 1)
	assert.Len(t, existing[0].PayloadSizes.Test.SSZFull, 2)
}

func TestComputePayloadSizeBuckets_UnknownVersion(t *testing.T) {
	// Build a valid V99 engine_newPayload line that will fail dispatcher version check.
	ep := eest.ExecutionPayload{
		ParentHash:    "0x" + hexN("11", 32),
		FeeRecipient:  "0x" + hexN("22", 20),
		StateRoot:     "0x" + hexN("33", 32),
		ReceiptsRoot:  "0x" + hexN("44", 32),
		LogsBloom:     "0x" + hexN("00", 256),
		PrevRandao:    "0x" + hexN("55", 32),
		BlockNumber:   "0x10",
		GasLimit:      "0x1000000",
		GasUsed:       "0x800000",
		Timestamp:     "0x65000000",
		ExtraData:     "0x",
		BaseFeePerGas: "0x7",
		BlockHash:     "0x" + hexN("66", 32),
		Transactions:  []string{},
	}
	epJSON, _ := json.Marshal(ep)
	line := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_newPayloadV99","params":[%s],"id":1}`, epJSON)
	log := logrus.New()
	sizes := ComputePayloadSizeBuckets(log, "test_x", []string{line})
	// All three slices should be empty — the version is rejected by EestToPrysmPayload,
	// so no per-block entries are appended.
	assert.Empty(t, sizes.SSZFull)
	assert.Empty(t, sizes.SSZFullSnappy)
	assert.Empty(t, sizes.SSZBAL)
	assert.Empty(t, sizes.JSONFull)
	assert.Empty(t, sizes.JSONBAL)
}
