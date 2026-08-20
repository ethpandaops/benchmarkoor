package executor

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ethpandaops/benchmarkoor/pkg/jsonrpc"
)

func isNewPayloadMethod(method string) bool {
	return strings.HasPrefix(method, "engine_newPayload") || method == "reth_newPayload"
}

func isForkchoiceUpdatedMethod(method string) bool {
	return strings.HasPrefix(method, "engine_forkchoiceUpdated") ||
		method == "reth_forkchoiceUpdated"
}

// ServerTiming is the timing breakdown measured inside Reth/Tempo. Nanoseconds
// are used in result artifacts to match Benchmarkoor's existing duration unit.
type ServerTiming struct {
	ExecutionNS          int64  `json:"execution_ns"`
	PersistenceWaitNS    int64  `json:"persistence_wait_ns"`
	ExecutionCacheWaitNS *int64 `json:"execution_cache_wait_ns,omitempty"`
	SparseTrieWaitNS     *int64 `json:"sparse_trie_wait_ns,omitempty"`
}

func extractServerTiming(method, response string) *ServerTiming {
	if method != "reth_newPayload" || response == "" {
		return nil
	}

	var envelope struct {
		Result struct {
			LatencyUS            *uint64 `json:"latency_us"`
			PersistenceWaitUS    uint64  `json:"persistence_wait_us"`
			ExecutionCacheWaitUS *uint64 `json:"execution_cache_wait_us"`
			SparseTrieWaitUS     *uint64 `json:"sparse_trie_wait_us"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(response), &envelope); err != nil || envelope.Result.LatencyUS == nil {
		return nil
	}

	timing := &ServerTiming{
		ExecutionNS:       int64(*envelope.Result.LatencyUS) * 1_000,
		PersistenceWaitNS: int64(envelope.Result.PersistenceWaitUS) * 1_000,
	}
	if envelope.Result.ExecutionCacheWaitUS != nil {
		value := int64(*envelope.Result.ExecutionCacheWaitUS) * 1_000
		timing.ExecutionCacheWaitNS = &value
	}
	if envelope.Result.SparseTrieWaitUS != nil {
		value := int64(*envelope.Result.SparseTrieWaitUS) * 1_000
		timing.SparseTrieWaitNS = &value
	}

	return timing
}

func (e *executor) validateEngineResponse(
	method string,
	resp *jsonrpc.Response,
	metadata *RequestMetadata,
) error {
	if metadata == nil || (metadata.ExpectedStatus == "" &&
		metadata.ValidationErrorContains == "" && metadata.ExpectedRPCErrorCode == nil) {
		return e.validator.Validate(method, resp)
	}

	if metadata.ExpectedRPCErrorCode != nil {
		if resp.Error == nil {
			return fmt.Errorf("expected JSON-RPC error %d, got success", *metadata.ExpectedRPCErrorCode)
		}
		if resp.Error.Code != *metadata.ExpectedRPCErrorCode {
			return fmt.Errorf("JSON-RPC error code is %d, expected %d", resp.Error.Code, *metadata.ExpectedRPCErrorCode)
		}
		if metadata.ValidationErrorContains != "" &&
			!strings.Contains(resp.Error.Message, metadata.ValidationErrorContains) {
			return fmt.Errorf("JSON-RPC error %q does not contain %q", resp.Error.Message, metadata.ValidationErrorContains)
		}
		return nil
	}

	if resp.Error != nil {
		return fmt.Errorf("JSON-RPC error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	expected := metadata.ExpectedStatus
	if expected == "" {
		expected = "VALID"
	}

	var status, validationError string
	switch {
	case isNewPayloadMethod(method):
		var result jsonrpc.NewPayloadResult
		if err := resp.ParseResult(&result); err != nil {
			return fmt.Errorf("parsing newPayload result: %w", err)
		}
		status, validationError = result.Status, result.ValidationError
	case isForkchoiceUpdatedMethod(method):
		var result jsonrpc.ForkchoiceUpdatedResult
		if err := resp.ParseResult(&result); err != nil {
			return fmt.Errorf("parsing forkchoiceUpdated result: %w", err)
		}
		status = result.PayloadStatus.Status
		validationError = result.PayloadStatus.ValidationError
	default:
		return e.validator.Validate(method, resp)
	}

	if status != expected {
		return fmt.Errorf("payload status is %s, expected %s", status, expected)
	}
	if metadata.ValidationErrorContains != "" &&
		!strings.Contains(validationError, metadata.ValidationErrorContains) {
		return fmt.Errorf("validation error %q does not contain %q", validationError, metadata.ValidationErrorContains)
	}

	return nil
}

func requestMetadataForLine(metadata []*RequestMetadata, line int) *RequestMetadata {
	if line < 0 || line >= len(metadata) {
		return nil
	}
	return metadata[line]
}
