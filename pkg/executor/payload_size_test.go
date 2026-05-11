package executor

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/eest"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
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

func TestComputePayloadSizes_SingleNewPayload(t *testing.T) {
	log := logrus.New()
	lines := []string{minimalDenebRequest(t)}
	sizes := ComputePayloadSizes(log, "test_x", lines)
	assert.Greater(t, sizes.PayloadBytes, uint64(100))
	assert.Greater(t, sizes.SnappyBytes, uint64(0))
	assert.LessOrEqual(t, sizes.SnappyBytes, sizes.PayloadBytes)
	assert.Equal(t, uint64(0), sizes.BALBytes) // Deneb has no BAL
}

func TestComputePayloadSizes_NoNewPayloadLines(t *testing.T) {
	log := logrus.New()
	sizes := ComputePayloadSizes(log, "test_x", []string{
		`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3","params":[],"id":1}`,
	})
	assert.Equal(t, uint64(0), sizes.PayloadBytes)
	assert.Equal(t, uint64(0), sizes.SnappyBytes)
	assert.Equal(t, uint64(0), sizes.BALBytes)
}

func TestComputePayloadSizes_BAL(t *testing.T) {
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
	line := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_newPayloadV6","params":[%s],"id":1}`, epJSON)
	sizes := ComputePayloadSizes(log, "test_x", []string{line})
	assert.Equal(t, uint64(1024), sizes.BALBytes)
	assert.Greater(t, sizes.PayloadBytes, uint64(1024))
}

func TestComputePayloadSizes_SumsAcrossMultiple(t *testing.T) {
	log := logrus.New()
	line := minimalDenebRequest(t)
	single := ComputePayloadSizes(log, "test_x", []string{line})
	double := ComputePayloadSizes(log, "test_x", []string{line, line})
	assert.Equal(t, single.PayloadBytes*2, double.PayloadBytes)
	assert.Equal(t, single.SnappyBytes*2, double.SnappyBytes)
}
