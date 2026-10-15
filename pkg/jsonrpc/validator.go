package jsonrpc

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNewPayloadSyncing is returned when engine_newPayload returns SYNCING status.
var ErrNewPayloadSyncing = errors.New("newPayload status is SYNCING")

// IsSyncingError checks if the error is a SYNCING status error.
func IsSyncingError(err error) bool {
	return errors.Is(err, ErrNewPayloadSyncing)
}

// Validator validates JSON-RPC responses.
type Validator interface {
	Validate(method string, resp *Response) error
}

// ErrorValidator fails if the response contains an error field.
type ErrorValidator struct{}

// Validate checks if the response has a JSON-RPC error.
func (v *ErrorValidator) Validate(_ string, resp *Response) error {
	if resp.Error != nil {
		return fmt.Errorf("JSON-RPC error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	return nil
}

// NewPayloadValidator fails if engine_newPayload* responses don't have VALID status.
type NewPayloadValidator struct{}

// Validate checks if engine_newPayload responses have VALID status.
func (v *NewPayloadValidator) Validate(method string, resp *Response) error {
	if !strings.HasPrefix(method, "engine_newPayload") {
		return nil
	}

	var result NewPayloadResult
	if err := resp.ParseResult(&result); err != nil {
		return fmt.Errorf("parsing newPayload result: %w", err)
	}

	if result.Status == "SYNCING" {
		return fmt.Errorf("%w", ErrNewPayloadSyncing)
	}

	if result.Status != "VALID" {
		errMsg := fmt.Sprintf("newPayload status is %s, expected VALID", result.Status)
		if result.ValidationError != "" {
			errMsg = fmt.Sprintf("%s: %s", errMsg, result.ValidationError)
		}

		return fmt.Errorf("%s", errMsg)
	}

	return nil
}

// ErrPayloadAccepted is returned when a client accepts a payload that it must
// reject. It points at a client that skips a validity check.
var ErrPayloadAccepted = errors.New("client accepted a payload that it must reject")

// ValidateRejectedNewPayload checks the response to an engine_newPayload call
// whose payload the client must reject. With errorCode nil the client must
// answer status INVALID. With errorCode set it must answer that JSON-RPC
// error.
//
// Some INVALID answers do not come from the block itself, and the call then
// measures nothing, so they fail too:
//   - INVALID_BLOCK_HASH: the client never executed the block.
//   - a latestValidHash other than parentHash: an ancestor is invalid, so the
//     client rejected the block without executing it. An empty
//     latestValidHash or parentHash skips this check.
func ValidateRejectedNewPayload(resp *Response, errorCode *int, parentHash string) error {
	if errorCode != nil {
		if resp.Error == nil {
			return fmt.Errorf("expected JSON-RPC error %d, got a result", *errorCode)
		}

		if resp.Error.Code != *errorCode {
			return fmt.Errorf("expected JSON-RPC error %d, got %d: %s",
				*errorCode, resp.Error.Code, resp.Error.Message)
		}

		return nil
	}

	if resp.Error != nil {
		return fmt.Errorf("JSON-RPC error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	var result NewPayloadResult
	if err := resp.ParseResult(&result); err != nil {
		return fmt.Errorf("parsing newPayload result: %w", err)
	}

	switch result.Status {
	case "INVALID":
		if result.LatestValidHash != "" && parentHash != "" &&
			!strings.EqualFold(result.LatestValidHash, parentHash) {
			return fmt.Errorf("newPayload status is INVALID with latestValidHash %s, "+
				"not the parent %s: the client rejected an invalid ancestor, "+
				"not this block", result.LatestValidHash, parentHash)
		}

		return nil
	case "VALID":
		return fmt.Errorf("%w: newPayload status is VALID", ErrPayloadAccepted)
	case "INVALID_BLOCK_HASH":
		return fmt.Errorf("newPayload status is INVALID_BLOCK_HASH, expected INVALID: " +
			"the client rejected the block hash without executing the block")
	default:
		return fmt.Errorf("newPayload status is %s, expected INVALID", result.Status)
	}
}

// ForkchoiceUpdatedValidator fails if engine_forkchoiceUpdated* responses don't have VALID status.
type ForkchoiceUpdatedValidator struct{}

// Validate checks if engine_forkchoiceUpdated responses have VALID status.
func (v *ForkchoiceUpdatedValidator) Validate(method string, resp *Response) error {
	if !strings.HasPrefix(method, "engine_forkchoiceUpdated") {
		return nil
	}

	var result ForkchoiceUpdatedResult
	if err := resp.ParseResult(&result); err != nil {
		return fmt.Errorf("parsing forkchoiceUpdated result: %w", err)
	}

	if result.PayloadStatus.Status != "VALID" {
		errMsg := fmt.Sprintf("forkchoiceUpdated status is %s, expected VALID",
			result.PayloadStatus.Status)
		if result.PayloadStatus.ValidationError != "" {
			errMsg = fmt.Sprintf("%s: %s", errMsg, result.PayloadStatus.ValidationError)
		}

		return fmt.Errorf("%s", errMsg)
	}

	return nil
}

// ComposedValidator runs multiple validators in sequence.
type ComposedValidator struct {
	validators []Validator
}

// NewComposedValidator creates a validator that runs multiple validators in sequence.
func NewComposedValidator(validators ...Validator) *ComposedValidator {
	return &ComposedValidator{
		validators: validators,
	}
}

// Validate runs all validators in sequence, returning the first error encountered.
func (v *ComposedValidator) Validate(method string, resp *Response) error {
	for _, validator := range v.validators {
		if err := validator.Validate(method, resp); err != nil {
			return err
		}
	}

	return nil
}

// DefaultValidator returns a composed validator with ErrorValidator, NewPayloadValidator,
// and ForkchoiceUpdatedValidator.
func DefaultValidator() Validator {
	return NewComposedValidator(
		&ErrorValidator{},
		&NewPayloadValidator{},
		&ForkchoiceUpdatedValidator{},
	)
}
