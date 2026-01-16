package jsonrpc

import (
	"encoding/json"
	"fmt"
)

// Response represents a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Error represents a JSON-RPC 2.0 error object.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewPayloadResult represents the result of an engine_newPayload call.
type NewPayloadResult struct {
	Status          string `json:"status"`
	LatestValidHash string `json:"latestValidHash,omitempty"`
	ValidationError string `json:"validationError,omitempty"`
}

// PayloadStatus represents the payload status in forkchoiceUpdated responses.
type PayloadStatus struct {
	Status          string `json:"status"`
	LatestValidHash string `json:"latestValidHash,omitempty"`
	ValidationError string `json:"validationError,omitempty"`
}

// ForkchoiceUpdatedResult represents the result of an engine_forkchoiceUpdated call.
type ForkchoiceUpdatedResult struct {
	PayloadStatus PayloadStatus `json:"payloadStatus"`
	PayloadID     string        `json:"payloadId,omitempty"`
}

// Parse parses a JSON-RPC response from a string.
func Parse(data string) (*Response, error) {
	var resp Response
	if err := json.Unmarshal([]byte(data), &resp); err != nil {
		return nil, fmt.Errorf("parsing JSON-RPC response: %w", err)
	}

	return &resp, nil
}

// ParseResult parses the result field into the provided type.
func (r *Response) ParseResult(v any) error {
	if r.Result == nil {
		return fmt.Errorf("response has no result field")
	}

	if err := json.Unmarshal(r.Result, v); err != nil {
		return fmt.Errorf("parsing result: %w", err)
	}

	return nil
}
