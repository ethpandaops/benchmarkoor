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

// newPayloadWithTxCount builds an engine_newPayloadV3 line carrying the
// given number of fake transaction hex strings, so the only thing the
// tx-count computation can see is the slice length.
func newPayloadWithTxCount(t *testing.T, txCount int) string {
	t.Helper()

	txs := make([]string, txCount)
	for i := range txs {
		txs[i] = "0xdeadbeef"
	}

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
		Transactions:  txs,
		Withdrawals:   []*eest.Withdrawal{},
	}

	epJSON, err := json.Marshal(ep)
	require.NoError(t, err)

	return fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_newPayloadV3","params":[%s],"id":1}`, epJSON)
}

func TestComputeTxCountsForStep_MultipleBlocks(t *testing.T) {
	lines := []string{
		newPayloadWithTxCount(t, 3),
		newPayloadWithTxCount(t, 7),
		newPayloadWithTxCount(t, 0),
	}

	got := ComputeTxCountsForStep(lines)
	assert.Equal(t, []uint64{3, 7, 0}, got)
}

func TestComputeTxCountsForStep_NoNewPayloadLines(t *testing.T) {
	got := ComputeTxCountsForStep([]string{
		`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3","params":[],"id":1}`,
	})
	assert.Empty(t, got)
}

func TestComputeTxCountsForStep_NoLines(t *testing.T) {
	got := ComputeTxCountsForStep(nil)
	assert.Empty(t, got)
}

func TestMergeTxCounts_FillsMissing(t *testing.T) {
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
			return []string{newPayloadWithTxCount(t, 5)}
		default:
			return nil
		}
	}

	MergeTxCounts(logrus.New(), existing, lineProvider)

	require.NotNil(t, existing[0].TxCounts)
	assert.Equal(t, []uint64{5}, existing[0].TxCounts.Test)
	assert.Nil(t, existing[1].TxCounts)
}

func TestMergeTxCounts_SkipsAlreadyPopulated(t *testing.T) {
	preExisting := &TxCounts{Test: []uint64{999}}
	existing := []SuiteTest{{Name: "test_a", TxCounts: preExisting}}

	called := false
	lineProvider := func(string, StepKind) []string {
		called = true

		return []string{newPayloadWithTxCount(t, 3)}
	}

	MergeTxCounts(logrus.New(), existing, lineProvider)
	assert.False(t, called, "should not invoke provider for already-populated entries")
	assert.Equal(t, []uint64{999}, existing[0].TxCounts.Test)
}

func TestMergeTxCounts_PerStep(t *testing.T) {
	existing := []SuiteTest{{Name: "test_a"}}

	lineProvider := func(_ string, step StepKind) []string {
		switch step {
		case StepKindSetup:
			return []string{newPayloadWithTxCount(t, 1)}
		case StepKindTest:
			return []string{newPayloadWithTxCount(t, 2), newPayloadWithTxCount(t, 4)}
		default:
			return nil
		}
	}

	MergeTxCounts(logrus.New(), existing, lineProvider)

	require.NotNil(t, existing[0].TxCounts)
	assert.Equal(t, []uint64{1}, existing[0].TxCounts.Setup)
	assert.Equal(t, []uint64{2, 4}, existing[0].TxCounts.Test)
	assert.Nil(t, existing[0].TxCounts.Cleanup)
}

func TestTxCounts_HasData(t *testing.T) {
	var nilCounts *TxCounts
	assert.False(t, nilCounts.HasData())
	assert.False(t, (&TxCounts{}).HasData())
	assert.True(t, (&TxCounts{Setup: []uint64{0}}).HasData())
	assert.True(t, (&TxCounts{Test: []uint64{42}}).HasData())
	assert.True(t, (&TxCounts{Cleanup: []uint64{1, 2, 3}}).HasData())
}
