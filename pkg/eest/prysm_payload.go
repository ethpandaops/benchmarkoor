package eest

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	enginev1 "github.com/OffchainLabs/prysm/v6/proto/engine/v1"
	primitives "github.com/OffchainLabs/prysm/v6/consensus-types/primitives"
)

// PrysmPayloadMarshaler is the minimum interface we need from Prysm's
// fork-specific ExecutionPayload structs.
type PrysmPayloadMarshaler interface {
	MarshalSSZ() ([]byte, error)
	SizeSSZ() int
}

// EestToPrysmPayload converts a JSON-parsed eest.ExecutionPayload into the
// matching Prysm fork-specific struct. The returned value can be SSZ-marshaled
// to obtain the on-the-wire payload bytes.
//
// Supported versions: 1 (Bellatrix), 2 (Capella), 3 (Deneb), 4 (Electra),
// 5/6 (Gloas, BAL-bearing). See plan tasks for each fork.
func EestToPrysmPayload(version int, ep *ExecutionPayload) (PrysmPayloadMarshaler, error) {
	switch version {
	case 1:
		return toPrysmBellatrix(ep)
	case 3:
		return toPrysmDeneb(ep)
	default:
		return nil, fmt.Errorf("unsupported newPayload version: %d", version)
	}
}

func toPrysmBellatrix(ep *ExecutionPayload) (*enginev1.ExecutionPayload, error) {
	parentHash, err := decodeHash32(ep.ParentHash, "parentHash")
	if err != nil {
		return nil, err
	}
	feeRecipient, err := decodeHash20(ep.FeeRecipient, "feeRecipient")
	if err != nil {
		return nil, err
	}
	stateRoot, err := decodeHash32(ep.StateRoot, "stateRoot")
	if err != nil {
		return nil, err
	}
	receiptsRoot, err := decodeHash32(ep.ReceiptsRoot, "receiptsRoot")
	if err != nil {
		return nil, err
	}
	logsBloom, err := decodeBytesN(ep.LogsBloom, 256, "logsBloom")
	if err != nil {
		return nil, err
	}
	prevRandao, err := decodeHash32(ep.PrevRandao, "prevRandao")
	if err != nil {
		return nil, err
	}
	blockNumber, err := decodeUint64(ep.BlockNumber, "blockNumber")
	if err != nil {
		return nil, err
	}
	gasLimit, err := decodeUint64(ep.GasLimit, "gasLimit")
	if err != nil {
		return nil, err
	}
	gasUsed, err := decodeUint64(ep.GasUsed, "gasUsed")
	if err != nil {
		return nil, err
	}
	timestamp, err := decodeUint64(ep.Timestamp, "timestamp")
	if err != nil {
		return nil, err
	}
	extraData, err := decodeBytes(ep.ExtraData, "extraData")
	if err != nil {
		return nil, err
	}
	baseFee, err := decodeUint256LE(ep.BaseFeePerGas, "baseFeePerGas")
	if err != nil {
		return nil, err
	}
	blockHash, err := decodeHash32(ep.BlockHash, "blockHash")
	if err != nil {
		return nil, err
	}
	transactions, err := decodeHexList(ep.Transactions, "transactions")
	if err != nil {
		return nil, err
	}

	return &enginev1.ExecutionPayload{
		ParentHash:    parentHash,
		FeeRecipient:  feeRecipient,
		StateRoot:     stateRoot,
		ReceiptsRoot:  receiptsRoot,
		LogsBloom:     logsBloom,
		PrevRandao:    prevRandao,
		BlockNumber:   blockNumber,
		GasLimit:      gasLimit,
		GasUsed:       gasUsed,
		Timestamp:     timestamp,
		ExtraData:     extraData,
		BaseFeePerGas: baseFee,
		BlockHash:     blockHash,
		Transactions:  transactions,
	}, nil
}

func toPrysmDeneb(ep *ExecutionPayload) (*enginev1.ExecutionPayloadDeneb, error) {
	parentHash, err := decodeHash32(ep.ParentHash, "parentHash")
	if err != nil {
		return nil, err
	}
	feeRecipient, err := decodeHash20(ep.FeeRecipient, "feeRecipient")
	if err != nil {
		return nil, err
	}
	stateRoot, err := decodeHash32(ep.StateRoot, "stateRoot")
	if err != nil {
		return nil, err
	}
	receiptsRoot, err := decodeHash32(ep.ReceiptsRoot, "receiptsRoot")
	if err != nil {
		return nil, err
	}
	logsBloom, err := decodeBytesN(ep.LogsBloom, 256, "logsBloom")
	if err != nil {
		return nil, err
	}
	prevRandao, err := decodeHash32(ep.PrevRandao, "prevRandao")
	if err != nil {
		return nil, err
	}
	blockNumber, err := decodeUint64(ep.BlockNumber, "blockNumber")
	if err != nil {
		return nil, err
	}
	gasLimit, err := decodeUint64(ep.GasLimit, "gasLimit")
	if err != nil {
		return nil, err
	}
	gasUsed, err := decodeUint64(ep.GasUsed, "gasUsed")
	if err != nil {
		return nil, err
	}
	timestamp, err := decodeUint64(ep.Timestamp, "timestamp")
	if err != nil {
		return nil, err
	}
	extraData, err := decodeBytes(ep.ExtraData, "extraData")
	if err != nil {
		return nil, err
	}
	baseFeeBytes32, err := decodeUint256LE(ep.BaseFeePerGas, "baseFeePerGas")
	if err != nil {
		return nil, err
	}
	blockHash, err := decodeHash32(ep.BlockHash, "blockHash")
	if err != nil {
		return nil, err
	}
	transactions, err := decodeHexList(ep.Transactions, "transactions")
	if err != nil {
		return nil, err
	}
	withdrawals, err := convertWithdrawals(ep.Withdrawals)
	if err != nil {
		return nil, err
	}
	blobGasUsed, err := decodeUint64(ep.BlobGasUsed, "blobGasUsed")
	if err != nil {
		return nil, err
	}
	excessBlobGas, err := decodeUint64(ep.ExcessBlobGas, "excessBlobGas")
	if err != nil {
		return nil, err
	}

	return &enginev1.ExecutionPayloadDeneb{
		ParentHash:    parentHash,
		FeeRecipient:  feeRecipient,
		StateRoot:     stateRoot,
		ReceiptsRoot:  receiptsRoot,
		LogsBloom:     logsBloom,
		PrevRandao:    prevRandao,
		BlockNumber:   blockNumber,
		GasLimit:      gasLimit,
		GasUsed:       gasUsed,
		Timestamp:     timestamp,
		ExtraData:     extraData,
		BaseFeePerGas: baseFeeBytes32,
		BlockHash:     blockHash,
		Transactions:  transactions,
		Withdrawals:   withdrawals,
		BlobGasUsed:   blobGasUsed,
		ExcessBlobGas: excessBlobGas,
	}, nil
}

// --- Helpers below ---

// decodeHash32 expects a 0x-prefixed 32-byte hex string and returns its bytes.
func decodeHash32(s, field string) ([]byte, error) {
	return decodeBytesN(s, 32, field)
}

// decodeHash20 expects a 0x-prefixed 20-byte hex string and returns its bytes.
func decodeHash20(s, field string) ([]byte, error) {
	return decodeBytesN(s, 20, field)
}

// decodeBytesN expects a 0x-prefixed hex string of exactly n bytes.
func decodeBytesN(s string, n int, field string) ([]byte, error) {
	b, err := decodeBytes(s, field)
	if err != nil {
		return nil, err
	}
	if len(b) != n {
		return nil, fmt.Errorf("%s: expected %d bytes, got %d", field, n, len(b))
	}
	return b, nil
}

// decodeBytes expects a 0x-prefixed hex string and returns the decoded bytes.
// An empty string returns a nil slice (treated as zero-length value).
func decodeBytes(s, field string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	if !strings.HasPrefix(s, "0x") {
		return nil, fmt.Errorf("%s: missing 0x prefix", field)
	}
	hexStr := s[2:]
	if hexStr == "" {
		return []byte{}, nil
	}
	b, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, fmt.Errorf("%s: decoding hex: %w", field, err)
	}
	return b, nil
}

// decodeUint64 expects a 0x-prefixed hex integer (e.g. "0x1234").
func decodeUint64(s, field string) (uint64, error) {
	if s == "" {
		return 0, nil
	}
	if !strings.HasPrefix(s, "0x") {
		return 0, fmt.Errorf("%s: missing 0x prefix", field)
	}
	v, err := strconv.ParseUint(s[2:], 16, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: parsing as uint64: %w", field, err)
	}
	return v, nil
}

// decodeUint256LE expects a 0x-prefixed hex integer and returns it as a
// little-endian 32-byte slice (SSZ uint256 wire format).
func decodeUint256LE(s, field string) ([]byte, error) {
	if !strings.HasPrefix(s, "0x") {
		return nil, fmt.Errorf("%s: missing 0x prefix", field)
	}
	hexStr := s[2:]
	if len(hexStr)%2 == 1 {
		hexStr = "0" + hexStr
	}
	be, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, fmt.Errorf("%s: decoding hex: %w", field, err)
	}
	if len(be) > 32 {
		return nil, fmt.Errorf("%s: value exceeds 32 bytes", field)
	}
	padded := make([]byte, 32)
	copy(padded[32-len(be):], be)
	le := make([]byte, 32)
	for i := range 32 {
		le[i] = padded[31-i]
	}
	return le, nil
}

// decodeHexList decodes each 0x-hex string into its byte slice.
func decodeHexList(items []string, field string) ([][]byte, error) {
	out := make([][]byte, len(items))
	for i, s := range items {
		b, err := decodeBytes(s, fmt.Sprintf("%s[%d]", field, i))
		if err != nil {
			return nil, err
		}
		out[i] = b
	}
	return out, nil
}

// convertWithdrawals maps EEST withdrawals to Prysm withdrawals.
func convertWithdrawals(ws []*Withdrawal) ([]*enginev1.Withdrawal, error) {
	out := make([]*enginev1.Withdrawal, len(ws))
	for i, w := range ws {
		idx, err := decodeUint64(w.Index, fmt.Sprintf("withdrawals[%d].index", i))
		if err != nil {
			return nil, err
		}
		validatorIdx, err := decodeUint64(w.ValidatorIndex, fmt.Sprintf("withdrawals[%d].validatorIndex", i))
		if err != nil {
			return nil, err
		}
		addr, err := decodeHash20(w.Address, fmt.Sprintf("withdrawals[%d].address", i))
		if err != nil {
			return nil, err
		}
		amount, err := decodeUint64(w.Amount, fmt.Sprintf("withdrawals[%d].amount", i))
		if err != nil {
			return nil, err
		}
		out[i] = &enginev1.Withdrawal{
			Index:          idx,
			ValidatorIndex: primitives.ValidatorIndex(validatorIdx),
			Address:        addr,
			Amount:         amount,
		}
	}
	return out, nil
}
