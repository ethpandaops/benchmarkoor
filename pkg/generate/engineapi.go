package generate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/sirupsen/logrus"
)

const (
	requestTimeout = 30 * time.Second
	zeroHash       = "0x0000000000000000000000000000000000000000000000000000000000000000"
)

// Withdrawal represents an Engine API withdrawal.
type Withdrawal struct {
	Index          string `json:"index"`
	ValidatorIndex string `json:"validatorIndex"`
	Address        string `json:"address"`
	Amount         string `json:"amount"`
}

// CapturedBlock holds the JSON-RPC bodies captured during block building.
type CapturedBlock struct {
	NewPayloadRequest string
	FCURequest        string
	BlockHash         string
	BlockNumber       uint64
}

// EngineClient handles Engine API interactions for block building.
type EngineClient struct {
	log       logrus.FieldLogger
	engineURL string // Authenticated Engine API endpoint.
	rpcURL    string // Unauthenticated RPC endpoint (also serves testing_buildBlockV1).
	jwt       string
	fork      string
	headHash  string
	timestamp uint64
}

// NewEngineClient creates a new Engine API client.
func NewEngineClient(
	log logrus.FieldLogger,
	engineURL, rpcURL, jwt, fork, headHash string,
	timestamp uint64,
) *EngineClient {
	return &EngineClient{
		log:       log,
		engineURL: engineURL,
		rpcURL:    rpcURL,
		jwt:       jwt,
		fork:      fork,
		headHash:  headHash,
		timestamp: timestamp,
	}
}

// HeadHash returns the current chain head hash.
func (c *EngineClient) HeadHash() string {
	return c.headHash
}

// BuildBlockWithTxns builds a block containing the given raw transactions
// using testing_buildBlockV1, then imports and finalizes it.
// Returns the captured newPayload + forkchoiceUpdated JSON-RPC bodies.
func (c *EngineClient) BuildBlockWithTxns(
	ctx context.Context,
	rawTxns []string,
	withdrawals []Withdrawal,
	extraData string,
) (*CapturedBlock, error) {
	return c.buildBlock(ctx, rawTxns, withdrawals, extraData)
}

// BuildEmptyBlock builds an empty block (for gas bumps).
func (c *EngineClient) BuildEmptyBlock(ctx context.Context) error {
	_, err := c.buildBlock(ctx, nil, nil, "")
	return err
}

// BuildFundingBlock builds a block with a withdrawal to fund an address.
func (c *EngineClient) BuildFundingBlock(
	ctx context.Context,
	address string,
	amount string,
) error {
	withdrawals := []Withdrawal{
		{
			Index:          "0x0",
			ValidatorIndex: "0x0",
			Address:        address,
			Amount:         amount,
		},
	}

	_, err := c.buildBlock(ctx, nil, withdrawals, "")

	return err
}

// newPayloadVersion returns the Engine API newPayload version for the fork.
func (c *EngineClient) newPayloadVersion() int {
	switch strings.ToLower(c.fork) {
	case "prague":
		return 4
	default:
		// Amsterdam/Osaka and future forks.
		return 5
	}
}

// fcuVersion returns the Engine API forkchoiceUpdated version for the fork.
func (c *EngineClient) fcuVersion() int {
	return 3
}

// buildBlock executes the full block building flow:
// 1. testing_buildBlockV1 → get execution payload
// 2. engine_newPayloadV{N} → import block
// 3. engine_forkchoiceUpdatedV{N} → finalize block
func (c *EngineClient) buildBlock(
	ctx context.Context,
	rawTxns []string,
	withdrawals []Withdrawal,
	extraData string,
) (*CapturedBlock, error) {
	c.timestamp++

	// Build the payload attributes.
	attrs := map[string]any{
		"timestamp":             fmt.Sprintf("0x%x", c.timestamp),
		"prevRandao":            zeroHash,
		"suggestedFeeRecipient": "0x0000000000000000000000000000000000000000",
		"parentBeaconBlockRoot": zeroHash,
	}

	if len(withdrawals) > 0 {
		attrs["withdrawals"] = withdrawals
	} else {
		attrs["withdrawals"] = []Withdrawal{}
	}

	// Build transactions array.
	txns := rawTxns
	if txns == nil {
		txns = []string{}
	}

	// Build extra data.
	if extraData == "" {
		extraData = "0x"
	}

	params := []any{c.headHash, attrs, txns, extraData}

	buildReq := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "testing_buildBlockV1",
		Params:  params,
		ID:      1,
	}

	// Call testing_buildBlockV1 on the unauthenticated RPC port.
	buildResp, err := c.doRPCCall(ctx, c.rpcURL, buildReq, false)
	if err != nil {
		return nil, fmt.Errorf("testing_buildBlockV1: %w", err)
	}

	if buildResp.Error != nil {
		return nil, fmt.Errorf(
			"testing_buildBlockV1 error (code %d): %s",
			buildResp.Error.Code, buildResp.Error.Message,
		)
	}

	// Parse the build response to extract execution payload.
	var buildResult struct {
		ExecutionPayload  json.RawMessage `json:"executionPayload"`
		BlobsBundle       json.RawMessage `json:"blobsBundle"`
		ExecutionRequests json.RawMessage `json:"executionRequests"`
	}

	if err := json.Unmarshal(buildResp.Result, &buildResult); err != nil {
		return nil, fmt.Errorf("parsing build result: %w", err)
	}

	// Extract block hash and number from the execution payload.
	var payloadHeader struct {
		BlockHash   string `json:"blockHash"`
		BlockNumber string `json:"blockNumber"`
	}

	if err := json.Unmarshal(buildResult.ExecutionPayload, &payloadHeader); err != nil {
		return nil, fmt.Errorf("parsing payload header: %w", err)
	}

	// Build newPayload request.
	npVersion := c.newPayloadVersion()
	npMethod := fmt.Sprintf("engine_newPayloadV%d", npVersion)

	npParams := []json.RawMessage{buildResult.ExecutionPayload}

	// V3+ includes blobsBundle versioned hashes and parent beacon block root.
	// For simplicity, extract expected_blob_versioned_hashes from blobsBundle if present.
	var blobHashes []string

	if buildResult.BlobsBundle != nil {
		var bundle struct {
			Commitments []string `json:"commitments"`
		}

		if err := json.Unmarshal(buildResult.BlobsBundle, &bundle); err == nil {
			blobHashes = make([]string, 0, len(bundle.Commitments))
			// Empty is fine - just needs to be present.
		}
	}

	if blobHashes == nil {
		blobHashes = []string{}
	}

	blobHashesJSON, _ := json.Marshal(blobHashes)
	npParams = append(npParams, blobHashesJSON)

	// Parent beacon block root.
	parentBeaconRoot, _ := json.Marshal(zeroHash)
	npParams = append(npParams, parentBeaconRoot)

	// V4+ includes execution requests.
	if npVersion >= 4 {
		execRequests := buildResult.ExecutionRequests
		if execRequests == nil {
			execRequests = json.RawMessage(`[]`)
		}

		npParams = append(npParams, execRequests)
	}

	newPayloadReq := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  npMethod,
		Params:  npParams,
		ID:      1,
	}

	// Marshal the newPayload request for capture before sending.
	newPayloadBody, err := json.Marshal(newPayloadReq)
	if err != nil {
		return nil, fmt.Errorf("marshaling newPayload request: %w", err)
	}

	// Send newPayload (authenticated, Engine API port).
	npResp, err := c.doRPCCall(ctx, c.engineURL, newPayloadReq, true)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", npMethod, err)
	}

	if npResp.Error != nil {
		return nil, fmt.Errorf(
			"%s error (code %d): %s",
			npMethod, npResp.Error.Code, npResp.Error.Message,
		)
	}

	// Verify the newPayload status is VALID.
	var npResult struct {
		Status string `json:"status"`
	}

	if err := json.Unmarshal(npResp.Result, &npResult); err != nil {
		return nil, fmt.Errorf("parsing newPayload result: %w", err)
	}

	if npResult.Status != "VALID" {
		return nil, fmt.Errorf("%s returned status %q, expected VALID", npMethod, npResult.Status)
	}

	// Build forkchoiceUpdated request.
	fcuVersion := c.fcuVersion()
	fcuMethod := fmt.Sprintf("engine_forkchoiceUpdatedV%d", fcuVersion)

	fcuState := map[string]string{
		"headBlockHash":      payloadHeader.BlockHash,
		"safeBlockHash":      zeroHash,
		"finalizedBlockHash": zeroHash,
	}

	fcuReq := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  fcuMethod,
		Params:  []any{fcuState, nil},
		ID:      1,
	}

	fcuBody, err := json.Marshal(fcuReq)
	if err != nil {
		return nil, fmt.Errorf("marshaling FCU request: %w", err)
	}

	// Send forkchoiceUpdated (authenticated, Engine API port).
	fcuResp, err := c.doRPCCall(ctx, c.engineURL, fcuReq, true)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", fcuMethod, err)
	}

	if fcuResp.Error != nil {
		return nil, fmt.Errorf(
			"%s error (code %d): %s",
			fcuMethod, fcuResp.Error.Code, fcuResp.Error.Message,
		)
	}

	// Update head.
	c.headHash = payloadHeader.BlockHash

	blockNum := parseHexUint64(payloadHeader.BlockNumber)

	c.log.WithFields(logrus.Fields{
		"block_hash":   payloadHeader.BlockHash,
		"block_number": blockNum,
		"txns":         len(rawTxns),
	}).Debug("Block built and imported")

	return &CapturedBlock{
		NewPayloadRequest: string(newPayloadBody),
		FCURequest:        string(fcuBody),
		BlockHash:         payloadHeader.BlockHash,
		BlockNumber:       blockNum,
	}, nil
}

// jsonRPCRequest represents a JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
	ID      any    `json:"id"`
}

// jsonRPCResponse represents a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
	ID      any             `json:"id"`
}

// jsonRPCError represents a JSON-RPC 2.0 error.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// doRPCCall executes a JSON-RPC call and returns the parsed response.
func (c *EngineClient) doRPCCall(
	ctx context.Context,
	url string,
	request jsonRPCRequest,
	authenticated bool,
) (*jsonRPCResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, url, strings.NewReader(string(body)),
	)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if authenticated {
		token, tokenErr := executor.GenerateJWTToken(c.jwt)
		if tokenErr != nil {
			return nil, fmt.Errorf("generating JWT: %w", tokenErr)
		}

		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)

		return nil, fmt.Errorf(
			"unexpected status %d: %s", resp.StatusCode, string(respBody),
		)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	return &rpcResp, nil
}

// parseHexUint64 parses a 0x-prefixed hex string to uint64.
func parseHexUint64(s string) uint64 {
	s = strings.TrimPrefix(s, "0x")

	var val uint64

	for _, c := range s {
		val <<= 4

		switch {
		case c >= '0' && c <= '9':
			val |= uint64(c - '0')
		case c >= 'a' && c <= 'f':
			val |= uint64(c-'a') + 10
		case c >= 'A' && c <= 'F':
			val |= uint64(c-'A') + 10
		}
	}

	return val
}
