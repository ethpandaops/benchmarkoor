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
