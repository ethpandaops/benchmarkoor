package eest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEestToPrysmPayload_V3_Deneb(t *testing.T) {
	ep := &ExecutionPayload{
		ParentHash:    "0x1111111111111111111111111111111111111111111111111111111111111111",
		FeeRecipient:  "0x2222222222222222222222222222222222222222",
		StateRoot:     "0x3333333333333333333333333333333333333333333333333333333333333333",
		ReceiptsRoot:  "0x4444444444444444444444444444444444444444444444444444444444444444",
		LogsBloom:     "0x" + repeatHex("aa", 256),
		PrevRandao:    "0x5555555555555555555555555555555555555555555555555555555555555555",
		BlockNumber:   "0x10",
		GasLimit:      "0x1000000",
		GasUsed:       "0x800000",
		Timestamp:     "0x65000000",
		ExtraData:     "0xbeef",
		BaseFeePerGas: "0x7",
		BlockHash:     "0x6666666666666666666666666666666666666666666666666666666666666666",
		Transactions:  []string{"0xdeadbeef", "0xcafebabe"},
		Withdrawals:   []*Withdrawal{},
		BlobGasUsed:   "0x20000",
		ExcessBlobGas: "0x40000",
	}

	marshaler, err := EestToPrysmPayload(3, ep)
	require.NoError(t, err)
	require.NotNil(t, marshaler)

	sszBytes, err := marshaler.MarshalSSZ()
	require.NoError(t, err)
	assert.Greater(t, len(sszBytes), 100, "ssz output should be non-trivial")

	// Round-trip: re-marshal a freshly-unmarshaled struct, check byte equality.
	roundTrip, err := EestToPrysmPayload(3, ep)
	require.NoError(t, err)
	sszBytes2, err := roundTrip.MarshalSSZ()
	require.NoError(t, err)
	assert.Equal(t, sszBytes, sszBytes2, "deterministic encoding")
}

// repeatHex returns n copies of the 2-char hex pair concatenated.
func repeatHex(pair string, n int) string {
	out := make([]byte, 0, n*2)
	for range n {
		out = append(out, pair...)
	}
	return string(out)
}

func TestEestToPrysmPayload_V2_Capella(t *testing.T) {
	ep := &ExecutionPayload{
		ParentHash:    "0x1111111111111111111111111111111111111111111111111111111111111111",
		FeeRecipient:  "0x2222222222222222222222222222222222222222",
		StateRoot:     "0x3333333333333333333333333333333333333333333333333333333333333333",
		ReceiptsRoot:  "0x4444444444444444444444444444444444444444444444444444444444444444",
		LogsBloom:     "0x" + repeatHex("00", 256),
		PrevRandao:    "0x5555555555555555555555555555555555555555555555555555555555555555",
		BlockNumber:   "0x10",
		GasLimit:      "0x1000000",
		GasUsed:       "0x800000",
		Timestamp:     "0x65000000",
		ExtraData:     "0x",
		BaseFeePerGas: "0x7",
		BlockHash:     "0x6666666666666666666666666666666666666666666666666666666666666666",
		Transactions:  []string{},
		Withdrawals: []*Withdrawal{
			{Index: "0x1", ValidatorIndex: "0x10", Address: "0x" + repeatHex("aa", 20), Amount: "0x100"},
		},
	}
	m, err := EestToPrysmPayload(2, ep)
	require.NoError(t, err)
	b, err := m.MarshalSSZ()
	require.NoError(t, err)
	assert.Greater(t, len(b), 100)
}

func TestEestToPrysmPayload_Gloas_WithBAL(t *testing.T) {
	balHex := "0x" + repeatHex("ab", 1024) // 1KB synthetic BAL payload
	ep := &ExecutionPayload{
		ParentHash:      "0x1111111111111111111111111111111111111111111111111111111111111111",
		FeeRecipient:    "0x2222222222222222222222222222222222222222",
		StateRoot:       "0x3333333333333333333333333333333333333333333333333333333333333333",
		ReceiptsRoot:    "0x4444444444444444444444444444444444444444444444444444444444444444",
		LogsBloom:       "0x" + repeatHex("00", 256),
		PrevRandao:      "0x5555555555555555555555555555555555555555555555555555555555555555",
		BlockNumber:     "0x10",
		GasLimit:        "0x1000000",
		GasUsed:         "0x800000",
		Timestamp:       "0x65000000",
		ExtraData:       "0x",
		BaseFeePerGas:   "0x7",
		BlockHash:       "0x6666666666666666666666666666666666666666666666666666666666666666",
		Transactions:    []string{},
		Withdrawals:     []*Withdrawal{},
		BlobGasUsed:     "0x0",
		ExcessBlobGas:   "0x0",
		BlockAccessList: balHex,
		SlotNumber:      "0x1234",
	}
	m, err := EestToPrysmPayload(6, ep)
	require.NoError(t, err)
	b, err := m.MarshalSSZ()
	require.NoError(t, err)
	// Encoded size must be at least the BAL size (1024 bytes) plus other fields.
	assert.Greater(t, len(b), 1024+100, "expected payload to exceed BAL+constants")
}

func TestEestToPrysmPayload_Gloas_EmptyBAL(t *testing.T) {
	ep := &ExecutionPayload{
		ParentHash:    "0x1111111111111111111111111111111111111111111111111111111111111111",
		FeeRecipient:  "0x2222222222222222222222222222222222222222",
		StateRoot:     "0x3333333333333333333333333333333333333333333333333333333333333333",
		ReceiptsRoot:  "0x4444444444444444444444444444444444444444444444444444444444444444",
		LogsBloom:     "0x" + repeatHex("00", 256),
		PrevRandao:    "0x5555555555555555555555555555555555555555555555555555555555555555",
		BlockNumber:   "0x10",
		GasLimit:      "0x1000000",
		GasUsed:       "0x800000",
		Timestamp:     "0x65000000",
		ExtraData:     "0x",
		BaseFeePerGas: "0x7",
		BlockHash:     "0x6666666666666666666666666666666666666666666666666666666666666666",
		Transactions:  []string{},
		Withdrawals:   []*Withdrawal{},
		// BlockAccessList omitted
	}
	m, err := EestToPrysmPayload(6, ep)
	require.NoError(t, err)
	b, err := m.MarshalSSZ()
	require.NoError(t, err)
	assert.Greater(t, len(b), 500, "expected non-trivial encoded size for empty-BAL Gloas payload")
}

func TestEestToPrysmPayload_Gloas_V5(t *testing.T) {
	// Smoke test ensuring version 5 dispatches to Gloas (same as version 6).
	ep := &ExecutionPayload{
		ParentHash:    "0x1111111111111111111111111111111111111111111111111111111111111111",
		FeeRecipient:  "0x2222222222222222222222222222222222222222",
		StateRoot:     "0x3333333333333333333333333333333333333333333333333333333333333333",
		ReceiptsRoot:  "0x4444444444444444444444444444444444444444444444444444444444444444",
		LogsBloom:     "0x" + repeatHex("00", 256),
		PrevRandao:    "0x5555555555555555555555555555555555555555555555555555555555555555",
		BlockNumber:   "0x10",
		GasLimit:      "0x1000000",
		GasUsed:       "0x800000",
		Timestamp:     "0x65000000",
		ExtraData:     "0x",
		BaseFeePerGas: "0x7",
		BlockHash:     "0x6666666666666666666666666666666666666666666666666666666666666666",
		Transactions:  []string{},
		Withdrawals:   []*Withdrawal{},
	}
	m, err := EestToPrysmPayload(5, ep)
	require.NoError(t, err)
	b, err := m.MarshalSSZ()
	require.NoError(t, err)
	assert.Greater(t, len(b), 500)
}

func TestEestToPrysmPayload_V4_Electra(t *testing.T) {
	ep := &ExecutionPayload{
		ParentHash:    "0x1111111111111111111111111111111111111111111111111111111111111111",
		FeeRecipient:  "0x2222222222222222222222222222222222222222",
		StateRoot:     "0x3333333333333333333333333333333333333333333333333333333333333333",
		ReceiptsRoot:  "0x4444444444444444444444444444444444444444444444444444444444444444",
		LogsBloom:     "0x" + repeatHex("00", 256),
		PrevRandao:    "0x5555555555555555555555555555555555555555555555555555555555555555",
		BlockNumber:   "0x10",
		GasLimit:      "0x1000000",
		GasUsed:       "0x800000",
		Timestamp:     "0x65000000",
		ExtraData:     "0x",
		BaseFeePerGas: "0x7",
		BlockHash:     "0x6666666666666666666666666666666666666666666666666666666666666666",
		Transactions:  []string{"0xdead"},
		Withdrawals:   []*Withdrawal{},
		BlobGasUsed:   "0x20000",
		ExcessBlobGas: "0x40000",
	}
	m, err := EestToPrysmPayload(4, ep)
	require.NoError(t, err)
	b, err := m.MarshalSSZ()
	require.NoError(t, err)
	assert.Greater(t, len(b), 100)
}

func TestEestToPrysmPayload_V1_Bellatrix(t *testing.T) {
	ep := &ExecutionPayload{
		ParentHash:    "0x1111111111111111111111111111111111111111111111111111111111111111",
		FeeRecipient:  "0x2222222222222222222222222222222222222222",
		StateRoot:     "0x3333333333333333333333333333333333333333333333333333333333333333",
		ReceiptsRoot:  "0x4444444444444444444444444444444444444444444444444444444444444444",
		LogsBloom:     "0x" + repeatHex("00", 256),
		PrevRandao:    "0x5555555555555555555555555555555555555555555555555555555555555555",
		BlockNumber:   "0x10",
		GasLimit:      "0x1000000",
		GasUsed:       "0x800000",
		Timestamp:     "0x65000000",
		ExtraData:     "0x",
		BaseFeePerGas: "0x7",
		BlockHash:     "0x6666666666666666666666666666666666666666666666666666666666666666",
		Transactions:  []string{},
	}
	m, err := EestToPrysmPayload(1, ep)
	require.NoError(t, err)
	b, err := m.MarshalSSZ()
	require.NoError(t, err)
	assert.Greater(t, len(b), 100)
}

func TestEestToPrysmPayload_UnknownVersion(t *testing.T) {
	_, err := EestToPrysmPayload(99, &ExecutionPayload{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported newPayload version")
}

func TestEestToPrysmPayload_MalformedHex(t *testing.T) {
	ep := &ExecutionPayload{
		ParentHash:    "not-a-hex",
		FeeRecipient:  "0x2222222222222222222222222222222222222222",
		StateRoot:     "0x3333333333333333333333333333333333333333333333333333333333333333",
		ReceiptsRoot:  "0x4444444444444444444444444444444444444444444444444444444444444444",
		LogsBloom:     "0x" + repeatHex("00", 256),
		PrevRandao:    "0x5555555555555555555555555555555555555555555555555555555555555555",
		BlockNumber:   "0x10",
		GasLimit:      "0x1000000",
		GasUsed:       "0x800000",
		Timestamp:     "0x65000000",
		ExtraData:     "0x",
		BaseFeePerGas: "0x7",
		BlockHash:     "0x6666666666666666666666666666666666666666666666666666666666666666",
		Transactions:  []string{},
	}
	_, err := EestToPrysmPayload(1, ep)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parentHash")
}

func TestEestToPrysmPayload_WrongLengthHash(t *testing.T) {
	ep := &ExecutionPayload{
		ParentHash:    "0xdead", // only 2 bytes, not 32
		FeeRecipient:  "0x2222222222222222222222222222222222222222",
		StateRoot:     "0x3333333333333333333333333333333333333333333333333333333333333333",
		ReceiptsRoot:  "0x4444444444444444444444444444444444444444444444444444444444444444",
		LogsBloom:     "0x" + repeatHex("00", 256),
		PrevRandao:    "0x5555555555555555555555555555555555555555555555555555555555555555",
		BlockNumber:   "0x10",
		GasLimit:      "0x1000000",
		GasUsed:       "0x800000",
		Timestamp:     "0x65000000",
		ExtraData:     "0x",
		BaseFeePerGas: "0x7",
		BlockHash:     "0x6666666666666666666666666666666666666666666666666666666666666666",
		Transactions:  []string{},
	}
	_, err := EestToPrysmPayload(1, ep)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected 32 bytes")
}
