package eest

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// SupportedFixtureFormat is the fixture format we support.
const SupportedFixtureFormat = "blockchain_test_engine_x"

// Fixture represents a single EEST test fixture.
type Fixture struct {
	Info               *FixtureInfo        `json:"_info"`
	Network            string              `json:"network"`
	GenesisBlockHeader *BlockHeader        `json:"genesisBlockHeader"`
	EngineNewPayloads  []*EngineNewPayload `json:"engineNewPayloads"`
}

// FixtureInfo contains metadata about the fixture.
type FixtureInfo struct {
	FixtureFormat         string         `json:"fixture-format"`
	Hash                  string         `json:"hash,omitempty"`
	OpcodeCount           map[string]int `json:"opcode_count,omitempty"`
	Comment               string         `json:"comment,omitempty"`
	FillingTransitionTool string         `json:"filling-transition-tool,omitempty"`
	Description           string         `json:"description,omitempty"`
	URL                   string         `json:"url,omitempty"`
}

// IsSupportedFormat returns true if the fixture has a supported format.
func (f *Fixture) IsSupportedFormat() bool {
	return f.Info != nil && f.Info.FixtureFormat == SupportedFixtureFormat
}

// BlockHeader represents an Ethereum block header.
type BlockHeader struct {
	ParentHash            string `json:"parentHash"`
	UncleHash             string `json:"uncleHash"`
	Coinbase              string `json:"coinbase"`
	StateRoot             string `json:"stateRoot"`
	TransactionsTrie      string `json:"transactionsTrie"`
	ReceiptTrie           string `json:"receiptTrie"`
	Bloom                 string `json:"bloom"`
	Difficulty            string `json:"difficulty"`
	Number                string `json:"number"`
	GasLimit              string `json:"gasLimit"`
	GasUsed               string `json:"gasUsed"`
	Timestamp             string `json:"timestamp"`
	ExtraData             string `json:"extraData"`
	MixDigest             string `json:"mixDigest"`
	Nonce                 string `json:"nonce"`
	BaseFeePerGas         string `json:"baseFeePerGas,omitempty"`
	WithdrawalsRoot       string `json:"withdrawalsRoot,omitempty"`
	BlobGasUsed           string `json:"blobGasUsed,omitempty"`
	ExcessBlobGas         string `json:"excessBlobGas,omitempty"`
	ParentBeaconBlockRoot string `json:"parentBeaconBlockRoot,omitempty"`
	RequestsHash          string `json:"requestsHash,omitempty"`
	Hash                  string `json:"hash"`
}

// EngineNewPayloadRaw represents the raw JSON structure for engine_newPayload.
type EngineNewPayloadRaw struct {
	NewPayloadVersion        string            `json:"newPayloadVersion"`
	ForkchoiceUpdatedVersion string            `json:"forkchoiceUpdatedVersion"`
	Params                   []json.RawMessage `json:"params"`
	ValidationError          *ValidationError  `json:"validationError,omitempty"`
	ErrorCode                *int              `json:"errorCode,omitempty"`
}

// EngineNewPayload represents a parsed engine_newPayload RPC call entry.
type EngineNewPayload struct {
	ExecutionPayload         *ExecutionPayload
	BlobVersionedHashes      []string
	ParentBeaconBlockRoot    string
	ExecutionRequests        []string
	NewPayloadVersion        int
	ForkchoiceUpdatedVersion int
	ValidationError          *ValidationError
	ErrorCode                *int
}

// ValidationError represents an expected validation error.
type ValidationError struct {
	Message string `json:"message,omitempty"`
}

// UnmarshalJSON implements custom unmarshaling for EngineNewPayload.
func (e *EngineNewPayload) UnmarshalJSON(data []byte) error {
	var raw EngineNewPayloadRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("unmarshaling raw payload: %w", err)
	}

	// Parse version numbers.
	npVersion, err := strconv.Atoi(raw.NewPayloadVersion)
	if err != nil {
		return fmt.Errorf("parsing newPayloadVersion: %w", err)
	}

	fcuVersion, err := strconv.Atoi(raw.ForkchoiceUpdatedVersion)
	if err != nil {
		return fmt.Errorf("parsing forkchoiceUpdatedVersion: %w", err)
	}

	e.NewPayloadVersion = npVersion
	e.ForkchoiceUpdatedVersion = fcuVersion
	e.ValidationError = raw.ValidationError
	e.ErrorCode = raw.ErrorCode

	// Parse params based on position.
	// params[0] = execution payload (always present)
	// params[1] = blobVersionedHashes (V3+)
	// params[2] = parentBeaconBlockRoot (V3+)
	// params[3] = executionRequests (V4+)
	if len(raw.Params) < 1 {
		return fmt.Errorf("params array is empty")
	}

	// Parse execution payload (params[0]).
	var ep ExecutionPayload
	if err := json.Unmarshal(raw.Params[0], &ep); err != nil {
		return fmt.Errorf("parsing execution payload: %w", err)
	}

	e.ExecutionPayload = &ep

	// Parse blobVersionedHashes (params[1]) if present.
	if len(raw.Params) > 1 {
		if err := json.Unmarshal(raw.Params[1], &e.BlobVersionedHashes); err != nil {
			return fmt.Errorf("parsing blobVersionedHashes: %w", err)
		}
	}

	// Parse parentBeaconBlockRoot (params[2]) if present.
	if len(raw.Params) > 2 {
		if err := json.Unmarshal(raw.Params[2], &e.ParentBeaconBlockRoot); err != nil {
			return fmt.Errorf("parsing parentBeaconBlockRoot: %w", err)
		}
	}

	// Parse executionRequests (params[3]) if present.
	if len(raw.Params) > 3 {
		if err := json.Unmarshal(raw.Params[3], &e.ExecutionRequests); err != nil {
			return fmt.Errorf("parsing executionRequests: %w", err)
		}
	}

	return nil
}

// ExecutionPayload represents the execution payload in an engine_newPayload call.
type ExecutionPayload struct {
	ParentHash            string         `json:"parentHash"`
	FeeRecipient          string         `json:"feeRecipient"`
	StateRoot             string         `json:"stateRoot"`
	ReceiptsRoot          string         `json:"receiptsRoot"`
	LogsBloom             string         `json:"logsBloom"`
	PrevRandao            string         `json:"prevRandao"`
	BlockNumber           string         `json:"blockNumber"`
	GasLimit              string         `json:"gasLimit"`
	GasUsed               string         `json:"gasUsed"`
	Timestamp             string         `json:"timestamp"`
	ExtraData             string         `json:"extraData"`
	BaseFeePerGas         string         `json:"baseFeePerGas"`
	BlockHash             string         `json:"blockHash"`
	Transactions          []string       `json:"transactions"`
	Withdrawals           []*Withdrawal  `json:"withdrawals,omitempty"`
	BlobGasUsed           string         `json:"blobGasUsed,omitempty"`
	ExcessBlobGas         string         `json:"excessBlobGas,omitempty"`
	DepositRequests       []*Deposit     `json:"depositRequests,omitempty"`
	WithdrawalRequests    []*WithdrawReq `json:"withdrawalRequests,omitempty"`
	ConsolidationRequests []*Consolidate `json:"consolidationRequests,omitempty"`
}

// Withdrawal represents a withdrawal in the execution payload.
type Withdrawal struct {
	Index          string `json:"index"`
	ValidatorIndex string `json:"validatorIndex"`
	Address        string `json:"address"`
	Amount         string `json:"amount"`
}

// Deposit represents a deposit request in the execution payload.
type Deposit struct {
	Pubkey                string `json:"pubkey"`
	WithdrawalCredentials string `json:"withdrawalCredentials"`
	Amount                string `json:"amount"`
	Signature             string `json:"signature"`
	Index                 string `json:"index"`
}

// WithdrawReq represents a withdrawal request in the execution payload.
type WithdrawReq struct {
	SourceAddress   string `json:"sourceAddress"`
	ValidatorPubkey string `json:"validatorPubkey"`
	Amount          string `json:"amount"`
}

// Consolidate represents a consolidation request in the execution payload.
type Consolidate struct {
	SourceAddress string `json:"sourceAddress"`
	SourcePubkey  string `json:"sourcePubkey"`
	TargetPubkey  string `json:"targetPubkey"`
}

// ParseFixtureFile parses a fixture JSON file.
// The file contains a map of test names to Fixture objects.
func ParseFixtureFile(data []byte) (map[string]*Fixture, error) {
	var fixtures map[string]*Fixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		return nil, err
	}

	return fixtures, nil
}
