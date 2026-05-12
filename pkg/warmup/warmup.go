// Package warmup generates "warmup" engine_newPayload* requests by taking
// the new-payload calls from a test step, mutating one header field per
// iteration, and recomputing the blockHash so the EL client accepts the
// payload header and proceeds to execute it. Two mutation strategies are
// supported: replacing the stateRoot with a deterministic placeholder
// ("invalid-stateroot") or subtracting 1+i from gasUsed
// ("invalid-gasused"). Either way the point of warmup is to populate
// caches before the real test runs.
package warmup

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

// Fork identifies an Ethereum hardfork. We currently only support Osaka.
type Fork string

const (
	// ForkOsaka is the only supported fork at the moment. Its header layout
	// includes withdrawalsRoot, blobGasUsed, excessBlobGas, parentBeaconRoot,
	// and requestsHash (EIP-7685).
	ForkOsaka Fork = "osaka"
)

// Method selects how each warmup iteration mutates an engine_newPayload*
// header field before the blockHash is recomputed.
type Method string

const (
	// MethodInvalidStateRoot rewrites stateRoot to a deterministic
	// per-iteration value derived from a salt and the iteration index.
	MethodInvalidStateRoot Method = "invalid-stateroot"
	// MethodInvalidGasUsed subtracts (1+i) from the original gasUsed for
	// iteration i (so iteration 0 = original-1, iteration 1 = original-2,
	// and so on). stateRoot and the other fields are left untouched.
	MethodInvalidGasUsed Method = "invalid-gasused"
)

// OsakaWarmupSalt is the 32-byte salt mixed with the iteration index to
// derive the warmup stateRoot. Picked as an arbitrary non-zero constant so
// the resulting roots are non-empty and deterministic across runs.
const OsakaWarmupSalt = "0xe8d3a308a0d3fdaeed6c196f78aad4f9620b571da6dd5b886e7fa5eba07c83e0"

// IsValidFork returns true if the given fork identifier is supported.
func IsValidFork(fork string) bool {
	return Fork(fork) == ForkOsaka
}

// IsValidMethod returns true if the given method identifier is supported.
func IsValidMethod(method string) bool {
	m := Method(method)

	return m == MethodInvalidStateRoot || m == MethodInvalidGasUsed
}

// Generator transforms engine_newPayload* JSON-RPC lines into warmup
// equivalents. Per-iteration mutation is selected by Method; the
// blockHash is always recomputed afterwards. Each engine_newPayload* line
// expands to Count variants. Lines whose method is not engine_newPayload*
// are returned unchanged (a single copy regardless of Count).
type Generator struct {
	fork   Fork
	method Method
	count  int
	salt   []byte
}

// NewGenerator returns a Generator for the given fork and method.
// Currently only ForkOsaka is accepted. Method must be one of
// MethodInvalidStateRoot or MethodInvalidGasUsed. Count <= 0 is treated
// as 1.
func NewGenerator(fork Fork, method Method, count int) (*Generator, error) {
	if fork != ForkOsaka {
		return nil, fmt.Errorf("unsupported fork %q (only %q is supported)", fork, ForkOsaka)
	}

	if !IsValidMethod(string(method)) {
		return nil, fmt.Errorf(
			"unsupported method %q (supported: %q, %q)",
			method, MethodInvalidStateRoot, MethodInvalidGasUsed,
		)
	}

	if count <= 0 {
		count = 1
	}

	salt := common.FromHex(OsakaWarmupSalt)

	return &Generator{
		fork:   fork,
		method: method,
		count:  count,
		salt:   salt,
	}, nil
}

// Count returns the configured number of warmup iterations per
// engine_newPayload* line.
func (g *Generator) Count() int {
	return g.count
}

// Method returns the configured per-iteration mutation method.
func (g *Generator) Method() Method {
	return g.method
}

// StateRootForIteration returns the deterministic stateRoot used for
// warmup iteration i when the method is MethodInvalidStateRoot. It is
// exported primarily for tests.
func (g *Generator) StateRootForIteration(i int) common.Hash {
	buf := make([]byte, 0, len(g.salt)+8)
	buf = append(buf, g.salt...)

	var ibe [8]byte
	binary.BigEndian.PutUint64(ibe[:], uint64(i)) //nolint:gosec // i is non-negative.
	buf = append(buf, ibe[:]...)

	return common.BytesToHash(crypto.Keccak256(buf))
}

// GasUsedForIteration returns the gasUsed value used for warmup iteration
// i when the method is MethodInvalidGasUsed: original - (i+1). Returns
// an error if the subtraction would underflow (i.e. original gasUsed is
// smaller than the iteration count requires).
func (g *Generator) GasUsedForIteration(original uint64, i int) (uint64, error) {
	delta := uint64(i + 1) //nolint:gosec // i is non-negative.
	if delta > original {
		return 0, fmt.Errorf(
			"cannot subtract %d from gasUsed %d (would underflow)", delta, original,
		)
	}

	return original - delta, nil
}

// applyMutation rewrites a single header field on the payload to make
// iteration i distinct from the original (and from other iterations). The
// blockHash is recomputed by the caller after this returns.
func (g *Generator) applyMutation(data *engine.ExecutableData, i int) error {
	switch g.method {
	case MethodInvalidStateRoot:
		data.StateRoot = g.StateRootForIteration(i)

		return nil
	case MethodInvalidGasUsed:
		gas, err := g.GasUsedForIteration(data.GasUsed, i)
		if err != nil {
			return err
		}

		data.GasUsed = gas

		return nil
	default:
		return fmt.Errorf("unsupported method %q", g.method)
	}
}

// Transform rewrites a single JSON-RPC line. Non-engine_newPayload* lines
// pass through unchanged (a single-element slice). For engine_newPayload*
// lines, returns Count variants, each with its iteration's derived
// stateRoot and a recomputed blockHash. Empty/whitespace lines pass
// through unchanged.
func (g *Generator) Transform(line string) ([]string, error) {
	if strings.TrimSpace(line) == "" {
		return []string{line}, nil
	}

	var raw struct {
		JSONRPC string            `json:"jsonrpc"`
		ID      json.RawMessage   `json:"id"`
		Method  string            `json:"method"`
		Params  []json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil, fmt.Errorf("parse jsonrpc line: %w", err)
	}

	if !strings.HasPrefix(raw.Method, "engine_newPayload") {
		return []string{line}, nil
	}

	if len(raw.Params) < 1 {
		return nil, fmt.Errorf("%s: expected at least 1 param, got %d", raw.Method, len(raw.Params))
	}

	var data engine.ExecutableData
	if err := json.Unmarshal(raw.Params[0], &data); err != nil {
		return nil, fmt.Errorf("parse executionPayload: %w", err)
	}

	// engine_newPayloadV3+ carry blobVersionedHashes (params[1]),
	// parentBeaconBlockRoot (params[2]) and (V4+) executionRequests (params[3]).
	versionedHashes, beaconRoot, requests, err := decodeExtraParams(raw.Method, raw.Params)
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, g.count)

	for i := range g.count {
		// Mutate one header field per iteration and recompute blockHash.
		// ExecutableDataToBlockNoHash builds a block with all derived roots
		// (txRoot, withdrawalsRoot, requestsHash) without verifying the
		// supplied blockHash matches.
		variant := data
		if err := g.applyMutation(&variant, i); err != nil {
			return nil, fmt.Errorf("iteration %d: %w", i, err)
		}

		block, err := engine.ExecutableDataToBlockNoHash(variant, versionedHashes, beaconRoot, requests)
		if err != nil {
			return nil, fmt.Errorf("iteration %d: build block from payload: %w", i, err)
		}

		variant.BlockHash = block.Hash()

		newPayload, err := json.Marshal(&variant)
		if err != nil {
			return nil, fmt.Errorf("iteration %d: marshal warmup payload: %w", i, err)
		}

		// Clone params so each line gets its own params[0]. The other
		// params (blobVersionedHashes, beaconRoot, requests) are shared
		// raw bytes — safe to alias.
		params := make([]json.RawMessage, len(raw.Params))
		copy(params, raw.Params)
		params[0] = newPayload

		envelope := struct {
			JSONRPC string            `json:"jsonrpc"`
			ID      json.RawMessage   `json:"id"`
			Method  string            `json:"method"`
			Params  []json.RawMessage `json:"params"`
		}{
			JSONRPC: raw.JSONRPC,
			ID:      raw.ID,
			Method:  raw.Method,
			Params:  params,
		}

		encoded, err := json.Marshal(&envelope)
		if err != nil {
			return nil, fmt.Errorf("iteration %d: marshal jsonrpc line: %w", i, err)
		}

		out = append(out, string(encoded))
	}

	return out, nil
}

// TransformLines applies Transform to every line in the input slice. The
// returned slice may be longer than the input: each engine_newPayload*
// line expands to Count variants while non-newPayload lines pass through
// once.
func (g *Generator) TransformLines(lines []string) ([]string, error) {
	out := make([]string, 0, len(lines)*g.count)

	for i, line := range lines {
		transformed, err := g.Transform(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}

		out = append(out, transformed...)
	}

	return out, nil
}

// decodeExtraParams pulls versionedHashes, parentBeaconBlockRoot, and
// executionRequests out of an engine_newPayload* params array. It tolerates
// older payload versions that omit later params.
func decodeExtraParams(
	method string,
	params []json.RawMessage,
) (versionedHashes []common.Hash, beaconRoot *common.Hash, requests [][]byte, err error) {
	if len(params) >= 2 {
		var hashes []common.Hash
		if err := json.Unmarshal(params[1], &hashes); err != nil {
			return nil, nil, nil, fmt.Errorf("%s: parse blobVersionedHashes: %w", method, err)
		}

		versionedHashes = hashes
	}

	if len(params) >= 3 {
		var root common.Hash
		if err := json.Unmarshal(params[2], &root); err != nil {
			return nil, nil, nil, fmt.Errorf("%s: parse parentBeaconBlockRoot: %w", method, err)
		}

		beaconRoot = &root
	}

	if len(params) >= 4 {
		var hexes []hexutil.Bytes
		if err := json.Unmarshal(params[3], &hexes); err != nil {
			return nil, nil, nil, fmt.Errorf("%s: parse executionRequests: %w", method, err)
		}

		reqs := make([][]byte, len(hexes))
		for i, h := range hexes {
			reqs[i] = h
		}

		requests = reqs
	}

	return versionedHashes, beaconRoot, requests, nil
}
