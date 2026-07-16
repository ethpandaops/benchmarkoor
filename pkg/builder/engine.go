package builder

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/sirupsen/logrus"
)

// engineExtraData is the block extraData benchmarkoor stamps on the gas-bump and
// funding blocks it builds via testing_buildBlockV1 ("benchmarkoor", <32 bytes).
const engineExtraData = "0x62656e63686d61726b6f6f72"

// engineCallTimeout bounds a single Engine/eth JSON-RPC call. Building a huge
// (near gas-limit) block can take a while on some clients, so it is generous.
const engineCallTimeout = 5 * time.Minute

// engineClient drives a filler's Engine API + eth RPC to build and apply blocks
// during the pre-run gas-bump and funding phases. It mirrors
// NethermindEth/gas-benchmarks' preparation_getpayload: for each block it calls
// testing_buildBlockV1 (JWT), then engine_newPayloadV{4,5} (JWT), then
// engine_forkchoiceUpdatedV3 (JWT) to make the built block canonical.
type engineClient struct {
	rpcURL    string
	engineURL string
	jwtSecret []byte
	fork      string
	slot      uint64
	http      *http.Client
}

// withdrawal is one beacon withdrawal in a funding block's payload attributes.
// All numeric fields are 0x-prefixed hex; Amount is in gwei.
type withdrawal struct {
	Index          string `json:"index"`
	ValidatorIndex string `json:"validatorIndex"`
	Address        string `json:"address"`
	Amount         string `json:"amount"`
}

// newEngineClient builds an engineClient for a filler reachable at ip. jwtHex is
// the shared JWT secret (with or without a 0x prefix); fork selects the
// engine_newPayload version.
func newEngineClient(ip string, rpcPort, enginePort int, jwtHex, fork string) (*engineClient, error) {
	secret, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(jwtHex), "0x"))
	if err != nil {
		return nil, fmt.Errorf("decoding JWT secret: %w", err)
	}

	if len(secret) == 0 {
		return nil, fmt.Errorf("JWT secret is empty")
	}

	return &engineClient{
		rpcURL:    fmt.Sprintf("http://%s:%d", ip, rpcPort),
		engineURL: fmt.Sprintf("http://%s:%d", ip, enginePort),
		jwtSecret: secret,
		fork:      fork,
		http:      &http.Client{},
	}, nil
}

// newPayloadMethod returns the engine_newPayload version for the fork
// (amsterdam → V5, otherwise V4), mirroring fill-stateful/gas-benchmarks.
func (c *engineClient) newPayloadMethod() string {
	if strings.EqualFold(c.fork, "amsterdam") {
		return "engine_newPayloadV5"
	}

	return "engine_newPayloadV4"
}

// makeJWT returns a signed HS256 JWT bearer token for the Engine API.
func (c *engineClient) makeJWT() (string, error) {
	b64 := base64.RawURLEncoding.EncodeToString

	header := b64([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims := b64(fmt.Appendf(nil, `{"iat":%d}`, time.Now().Unix()))
	unsigned := header + "." + claims

	mac := hmac.New(sha256.New, c.jwtSecret)
	if _, err := mac.Write([]byte(unsigned)); err != nil {
		return "", fmt.Errorf("signing JWT: %w", err)
	}

	return unsigned + "." + b64(mac.Sum(nil)), nil
}

// call issues a JSON-RPC request against url (JWT bearer added when useJWT) and
// returns the raw result. It fails on a JSON-RPC error object.
func (c *engineClient) call(ctx context.Context, url string, useJWT bool, method string, params []any) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, engineCallTimeout)
	defer cancel()

	reqBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, fmt.Errorf("marshaling %s request: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("creating %s request: %w", method, err)
	}

	req.Header.Set("Content-Type", "application/json")

	if useJWT {
		token, jwtErr := c.makeJWT()
		if jwtErr != nil {
			return nil, jwtErr
		}

		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing %s request: %w", method, err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s response: %w", method, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d: %s", method, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, fmt.Errorf("parsing %s response: %w", method, err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("%s: RPC error %d: %s", method, rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// latestBlock returns the current head's hash, timestamp, and gas limit.
func (c *engineClient) latestBlock(ctx context.Context) (hash string, timestamp, gasLimit uint64, err error) {
	res, err := c.call(ctx, c.rpcURL, false, "eth_getBlockByNumber", []any{"latest", false})
	if err != nil {
		return "", 0, 0, err
	}

	var block struct {
		Hash      string `json:"hash"`
		Timestamp string `json:"timestamp"`
		GasLimit  string `json:"gasLimit"`
	}
	if err := json.Unmarshal(res, &block); err != nil {
		return "", 0, 0, fmt.Errorf("parsing latest block: %w", err)
	}

	if block.Hash == "" {
		return "", 0, 0, fmt.Errorf("latest block has no hash (client not ready?)")
	}

	ts, err := hexToUint64(block.Timestamp)
	if err != nil {
		return "", 0, 0, fmt.Errorf("parsing block timestamp: %w", err)
	}

	gl, err := hexToUint64(block.GasLimit)
	if err != nil {
		return "", 0, 0, fmt.Errorf("parsing block gasLimit: %w", err)
	}

	return block.Hash, ts, gl, nil
}

// buildBlock builds one block on top of the current head via testing_buildBlockV1,
// submits it with engine_newPayload, and makes it canonical with
// engine_forkchoiceUpdatedV3. withdrawals may be nil (an empty block). It returns
// the new head's block hash and gas limit.
func (c *engineClient) buildBlock(ctx context.Context, withdrawals []withdrawal) (blockHash string, gasLimit uint64, err error) {
	parentHash, parentTS, _, err := c.latestBlock(ctx)
	if err != nil {
		return "", 0, err
	}

	c.slot++

	if withdrawals == nil {
		withdrawals = []withdrawal{}
	}

	attrs := map[string]any{
		"timestamp":             uintToHex(parentTS + 1),
		"prevRandao":            parentHash,
		"suggestedFeeRecipient": "0x0000000000000000000000000000000000000000",
		"withdrawals":           withdrawals,
		"parentBeaconBlockRoot": parentHash,
	}
	if strings.EqualFold(c.fork, "amsterdam") {
		attrs["slotNumber"] = uintToHex(c.slot)
	}

	built, err := c.call(ctx, c.engineURL, true, "testing_buildBlockV1",
		[]any{parentHash, attrs, []any{}, engineExtraData})
	if err != nil {
		return "", 0, err
	}

	var wrapper struct {
		ExecutionPayload  json.RawMessage `json:"executionPayload"`
		ExecutionRequests []string        `json:"executionRequests"`
	}
	if err := json.Unmarshal(built, &wrapper); err != nil {
		return "", 0, fmt.Errorf("parsing testing_buildBlockV1 result: %w", err)
	}

	// testing_buildBlockV1 may return the execution payload directly or wrapped
	// in {executionPayload, executionRequests}. Fall back to the raw result.
	execPayload := wrapper.ExecutionPayload
	if len(execPayload) == 0 {
		execPayload = built
	}

	var payloadFields struct {
		BlockHash string `json:"blockHash"`
		GasLimit  string `json:"gasLimit"`
	}
	if err := json.Unmarshal(execPayload, &payloadFields); err != nil {
		return "", 0, fmt.Errorf("parsing execution payload: %w", err)
	}

	if payloadFields.BlockHash == "" {
		return "", 0, fmt.Errorf("execution payload has no blockHash")
	}

	gl, err := hexToUint64(payloadFields.GasLimit)
	if err != nil {
		return "", 0, fmt.Errorf("parsing execution payload gasLimit: %w", err)
	}

	execRequests := wrapper.ExecutionRequests
	if execRequests == nil {
		execRequests = []string{}
	}

	// Gas-bump and funding blocks carry no blob transactions, so the blob
	// versioned hashes are always empty.
	if _, err := c.call(ctx, c.engineURL, true, c.newPayloadMethod(),
		[]any{execPayload, []string{}, parentHash, execRequests}); err != nil {
		return "", 0, err
	}

	fcs := map[string]any{
		"headBlockHash":      payloadFields.BlockHash,
		"safeBlockHash":      payloadFields.BlockHash,
		"finalizedBlockHash": payloadFields.BlockHash,
	}
	if _, err := c.call(ctx, c.engineURL, true, "engine_forkchoiceUpdatedV3",
		[]any{fcs, nil}); err != nil {
		return "", 0, err
	}

	return payloadFields.BlockHash, gl, nil
}

// bumpGasLimit builds empty blocks until the head's gas limit reaches target or
// maxBlocks blocks have been built. Each block can raise the limit by at most
// 1/1024, so a ramp from a small snapshot limit to the target takes many
// blocks. Returns the number of blocks built.
func (c *engineClient) bumpGasLimit(ctx context.Context, target uint64, maxBlocks int, log logrus.FieldLogger) (int, error) {
	_, _, gasLimit, err := c.latestBlock(ctx)
	if err != nil {
		return 0, err
	}

	if gasLimit >= target {
		log.WithFields(logrus.Fields{"gas_limit": gasLimit, "target": target}).
			Info("Head gas limit already at/above target; skipping gas bump")

		return 0, nil
	}

	log.WithFields(logrus.Fields{"from": gasLimit, "target": target, "max_blocks": maxBlocks}).
		Info("Bumping block gas limit")

	built := 0
	lastLog := time.Now()

	for built < maxBlocks {
		select {
		case <-ctx.Done():
			return built, ctx.Err()
		default:
		}

		_, gl, buildErr := c.buildBlock(ctx, nil)
		if buildErr != nil {
			return built, fmt.Errorf("building gas-bump block %d: %w", built+1, buildErr)
		}

		built++
		gasLimit = gl

		if gasLimit >= target {
			break
		}

		if time.Since(lastLog) >= 5*time.Second {
			log.WithFields(logrus.Fields{"blocks": built, "gas_limit": gasLimit, "target": target}).
				Info("Gas bump in progress")

			lastLog = time.Now()
		}
	}

	if gasLimit < target {
		return built, fmt.Errorf(
			"gas limit reached %d after %d blocks, still below target %d "+
				"(raise gas_bump_max_blocks or lower gas_limit)",
			gasLimit, built, target,
		)
	}

	log.WithFields(logrus.Fields{"blocks": built, "gas_limit": gasLimit}).
		Info("Gas bump complete")

	return built, nil
}

// fundingBlock builds one block that credits each account via a beacon
// withdrawal, then returns the new head's block hash. Returns "" when there are
// no accounts to fund (no block built).
func (c *engineClient) fundingBlock(ctx context.Context, accounts []config.PreRunFundingAccount, log logrus.FieldLogger) (string, error) {
	if len(accounts) == 0 {
		log.Info("No funding accounts configured; skipping funding block")

		return "", nil
	}

	withdrawals := make([]withdrawal, 0, len(accounts))
	for i, acct := range accounts {
		withdrawals = append(withdrawals, withdrawal{
			Index:          uintToHex(uint64(i) + 1),
			ValidatorIndex: uintToHex(uint64(i) + 1),
			Address:        acct.Address,
			Amount:         uintToHex(acct.ResolveAmountGwei()),
		})
	}

	log.WithField("accounts", len(accounts)).Info("Building funding block")

	blockHash, _, err := c.buildBlock(ctx, withdrawals)
	if err != nil {
		return "", fmt.Errorf("building funding block: %w", err)
	}

	return blockHash, nil
}

// hexToUint64 parses a 0x-prefixed (or bare) hex string to uint64.
func hexToUint64(s string) (uint64, error) {
	return strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 64)
}

// uintToHex formats v as a 0x-prefixed hex string (Engine API quantity form).
func uintToHex(v uint64) string {
	return "0x" + strconv.FormatUint(v, 16)
}
