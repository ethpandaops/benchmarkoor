package builder

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
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

	// preFork and activationTS drive per-block fork selection when building a
	// chain that crosses a fork boundary (e.g. deploy contracts on osaka, then
	// gas-bump/fill on amsterdam). A block whose timestamp is < activationTS is
	// built as preFork (V4, no slotNumber); at/after activationTS it is fork.
	// activationTS == 0 disables crossing: every block is built as fork.
	preFork      string
	activationTS uint64

	// recorded accumulates the engine_newPayload requests built by buildBlock, in
	// build order, when recording is enabled (see enableRecording). Used to export
	// a replayable bundle for non-filler clients.
	recording bool
	recorded  []recordedPayload

	// targetGasLimit, when non-zero, is sent as payload attribute targetGasLimit
	// on Amsterdam blocks: the gas limit the CL asks the block to move toward.
	// A ramp down sets it and leaves it set, so the blocks this client builds
	// after the ramp (the funding block) hold the limit instead of climbing back
	// toward the filler's miner ceiling.
	targetGasLimit uint64
}

// recordedPayload is one engine_newPayload request captured for replay: the
// method (version) and the verbatim params (execution payload incl. any
// blockAccessList, blob hashes, parentBeaconBlockRoot, executionRequests).
type recordedPayload struct {
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

// enableRecording makes buildBlock capture each newPayload request it sends.
func (c *engineClient) enableRecording() {
	c.recording = true
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

// withCrossing configures per-block fork crossing: blocks timestamped before
// activationTS are built as preFork, and at/after it as c.fork. It returns c for
// chaining. activationTS == 0 (or an empty preFork) leaves crossing disabled.
func (c *engineClient) withCrossing(preFork string, activationTS uint64) *engineClient {
	c.preFork = preFork
	c.activationTS = activationTS

	return c
}

// forkAt returns the fork a block timestamped ts is built under: preFork below
// the activation timestamp, c.fork at/after it (or always c.fork when crossing
// is disabled).
func (c *engineClient) forkAt(ts uint64) string {
	if c.activationTS > 0 && c.preFork != "" && ts < c.activationTS {
		return c.preFork
	}

	return c.fork
}

// newPayloadMethod returns the engine_newPayload version for c.fork.
func (c *engineClient) newPayloadMethod() string {
	return newPayloadMethodFor(c.fork)
}

// newPayloadMethodFor returns the engine_newPayload version for the fork
// (amsterdam → V5, otherwise V4), mirroring fill-stateful/gas-benchmarks.
func newPayloadMethodFor(fork string) string {
	if strings.EqualFold(fork, "amsterdam") {
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

// chainHead is the filler's current head, as eth_getBlockByNumber reports it.
type chainHead struct {
	hash      string
	timestamp uint64
	gasLimit  uint64
	// slotNumber is the EIP-7843 slot number; nil on a pre-Amsterdam block.
	slotNumber *uint64
}

// latestBlock returns the current head.
func (c *engineClient) latestBlock(ctx context.Context) (chainHead, error) {
	res, err := c.call(ctx, c.rpcURL, false, "eth_getBlockByNumber", []any{"latest", false})
	if err != nil {
		return chainHead{}, err
	}

	var block struct {
		Hash       string  `json:"hash"`
		Timestamp  string  `json:"timestamp"`
		GasLimit   string  `json:"gasLimit"`
		SlotNumber *string `json:"slotNumber"`
	}
	if err := json.Unmarshal(res, &block); err != nil {
		return chainHead{}, fmt.Errorf("parsing latest block: %w", err)
	}

	if block.Hash == "" {
		return chainHead{}, fmt.Errorf("latest block has no hash (client not ready?)")
	}

	h := chainHead{hash: block.Hash}

	if h.timestamp, err = hexToUint64(block.Timestamp); err != nil {
		return chainHead{}, fmt.Errorf("parsing block timestamp: %w", err)
	}

	if h.gasLimit, err = hexToUint64(block.GasLimit); err != nil {
		return chainHead{}, fmt.Errorf("parsing block gasLimit: %w", err)
	}

	if block.SlotNumber != nil {
		slot, err := hexToUint64(*block.SlotNumber)
		if err != nil {
			return chainHead{}, fmt.Errorf("parsing block slotNumber: %w", err)
		}

		h.slotNumber = &slot
	}

	return h, nil
}

// chainID returns the filler's chain id via eth_chainId, for signing deploy txs.
func (c *engineClient) chainID(ctx context.Context) (*big.Int, error) {
	res, err := c.call(ctx, c.rpcURL, false, "eth_chainId", []any{})
	if err != nil {
		return nil, err
	}

	var s string
	if err := json.Unmarshal(res, &s); err != nil {
		return nil, fmt.Errorf("parsing eth_chainId: %w", err)
	}

	id, ok := new(big.Int).SetString(strings.TrimPrefix(s, "0x"), 16)
	if !ok {
		return nil, fmt.Errorf("parsing chain id %q", s)
	}

	return id, nil
}

// latestBaseFee returns the current head's base fee per gas (0 if the block has
// none), used to price deploy transactions above the base fee.
func (c *engineClient) latestBaseFee(ctx context.Context) (*big.Int, error) {
	res, err := c.call(ctx, c.rpcURL, false, "eth_getBlockByNumber", []any{"latest", false})
	if err != nil {
		return nil, err
	}

	var block struct {
		BaseFeePerGas string `json:"baseFeePerGas"`
	}
	if err := json.Unmarshal(res, &block); err != nil {
		return nil, fmt.Errorf("parsing latest block base fee: %w", err)
	}

	if block.BaseFeePerGas == "" {
		return big.NewInt(0), nil
	}

	fee, ok := new(big.Int).SetString(strings.TrimPrefix(block.BaseFeePerGas, "0x"), 16)
	if !ok {
		return nil, fmt.Errorf("parsing base fee %q", block.BaseFeePerGas)
	}

	return fee, nil
}

// nonce returns addr's transaction count on the current head via
// eth_getTransactionCount. A deployer on an existing-network snapshot is rarely
// a fresh account, so its deploy txs have to continue from the nonce the chain
// already recorded rather than from zero.
func (c *engineClient) nonce(ctx context.Context, addr string) (uint64, error) {
	res, err := c.call(ctx, c.rpcURL, false, "eth_getTransactionCount", []any{addr, "latest"})
	if err != nil {
		return 0, err
	}

	var s string
	if err := json.Unmarshal(res, &s); err != nil {
		return 0, fmt.Errorf("parsing eth_getTransactionCount: %w", err)
	}

	n, err := strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing nonce %q: %w", s, err)
	}

	return n, nil
}

// code returns the deployed bytecode at addr on the current head via eth_getCode.
func (c *engineClient) code(ctx context.Context, addr string) ([]byte, error) {
	res, err := c.call(ctx, c.rpcURL, false, "eth_getCode", []any{addr, "latest"})
	if err != nil {
		return nil, err
	}

	var s string
	if err := json.Unmarshal(res, &s); err != nil {
		return nil, fmt.Errorf("parsing eth_getCode: %w", err)
	}

	return hex.DecodeString(strings.TrimPrefix(s, "0x"))
}

// buildBlock builds one block on top of the current head via testing_buildBlockV1,
// submits it with engine_newPayload, and makes it canonical with
// engine_forkchoiceUpdatedV3. withdrawals may be nil (no withdrawals) and rawTxs
// may be nil (an empty block); rawTxs are raw (network-encoded) transactions the
// block includes in order. The block's fork (payload version, slotNumber) is
// selected from its timestamp via forkAt, so a crossing chain builds pre-fork
// blocks (deploy) and post-fork blocks (fill) with one client. It returns the
// new head's block hash and gas limit.
func (c *engineClient) buildBlock(ctx context.Context, withdrawals []withdrawal, rawTxs [][]byte) (blockHash string, gasLimit uint64, err error) {
	parent, err := c.latestBlock(ctx)
	if err != nil {
		return "", 0, err
	}

	parentHash, parentTS := parent.hash, parent.timestamp

	// An Amsterdam parent's slot number continues; otherwise count from this
	// client's first block. The two agree on every chain this client built from a
	// pre-Amsterdam snapshot, and the first keeps a chain that is already past
	// Amsterdam (a pre-run on top of another) from restarting at slot 1.
	c.slot++
	if parent.slotNumber != nil {
		c.slot = *parent.slotNumber + 1
	}

	if withdrawals == nil {
		withdrawals = []withdrawal{}
	}

	blockTS := parentTS + 1
	blockFork := c.forkAt(blockTS)

	attrs := map[string]any{
		"timestamp":             uintToHex(blockTS),
		"prevRandao":            parentHash,
		"suggestedFeeRecipient": "0x0000000000000000000000000000000000000000",
		"withdrawals":           withdrawals,
		"parentBeaconBlockRoot": parentHash,
	}
	if strings.EqualFold(blockFork, "amsterdam") {
		attrs["slotNumber"] = uintToHex(c.slot)

		if c.targetGasLimit != 0 {
			attrs["targetGasLimit"] = uintToHex(c.targetGasLimit)
		}
	}

	// testing_buildBlockV1's transactions param is a list of hex-encoded raw
	// (network-encoded) transactions.
	txs := make([]any, 0, len(rawTxs))
	for _, raw := range rawTxs {
		txs = append(txs, "0x"+hex.EncodeToString(raw))
	}

	// testing_buildBlockV1 lives in the `testing` namespace, which every filler
	// exposes on its (unauthenticated) HTTP RPC port — geth's authrpc/engine port
	// does not serve it. So call it on the RPC URL without a JWT; only the Engine
	// API calls below (newPayload / forkchoiceUpdated) go to the engine port.
	built, err := c.call(ctx, c.rpcURL, false, "testing_buildBlockV1",
		[]any{parentHash, attrs, txs, engineExtraData})
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

	// Gas-bump, funding, and deploy blocks carry no blob transactions, so the
	// blob versioned hashes are always empty.
	npMethod := newPayloadMethodFor(blockFork)
	npParams := []any{execPayload, []string{}, parentHash, execRequests}

	if _, err := c.call(ctx, c.engineURL, true, npMethod, npParams); err != nil {
		return "", 0, err
	}

	if c.recording {
		rec, recErr := toRecordedPayload(npMethod, npParams)
		if recErr != nil {
			return "", 0, recErr
		}

		c.recorded = append(c.recorded, rec)
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

// replayBundleLogEvery controls how often replayBundleFile logs progress.
const replayBundleLogEvery = 500

// replaySyncingRetries is how many times replayBundleFile re-sends a payload that
// returns SYNCING/ACCEPTED before giving up (some clients apply blocks async).
const replaySyncingRetries = 60

// replayBundleFile streams a .request bundle (an engine_newPayload +
// forkchoiceUpdated pair per block, in order) to the engine port, asserting each
// returns VALID, and returns how many requests it sent. It advances the booted
// client's datadir to the bundle's head. It reads line by line: a release's
// pre-run bundle is over 12 GB.
func (c *engineClient) replayBundleFile(ctx context.Context, path string, log logrus.FieldLogger) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()

	r := bufio.NewReaderSize(f, 1<<20)
	n := 0

	for {
		line, readErr := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var req struct {
				Method string            `json:"method"`
				Params []json.RawMessage `json:"params"`
			}
			if err := json.Unmarshal(line, &req); err != nil {
				return n, fmt.Errorf("parsing line %d: %w", n, err)
			}

			params := make([]any, len(req.Params))
			for j := range req.Params {
				params[j] = req.Params[j]
			}

			if err := c.replayCall(ctx, req.Method, params); err != nil {
				return n, fmt.Errorf("line %d (%s): %w", n, req.Method, err)
			}

			n++
			if log != nil && n%replayBundleLogEvery == 0 {
				log.WithField("lines", n).Info("Replaying bundle")
			}
		}

		if errors.Is(readErr, io.EOF) {
			return n, nil
		}

		if readErr != nil {
			return n, fmt.Errorf("reading line %d: %w", n, readErr)
		}
	}
}

// replayCall sends one engine_newPayload / forkchoiceUpdated request and asserts
// the returned payload status is VALID, retrying on SYNCING/ACCEPTED.
func (c *engineClient) replayCall(ctx context.Context, method string, params []any) error {
	for attempt := 0; ; attempt++ {
		res, err := c.call(ctx, c.engineURL, true, method, params)
		if err != nil {
			return err
		}

		status, err := payloadStatusFromResult(method, res)
		if err != nil {
			return err
		}

		switch status {
		case "VALID":
			return nil
		case "SYNCING", "ACCEPTED":
			if attempt >= replaySyncingRetries {
				return fmt.Errorf("payload still %s after %d retries", status, attempt)
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		default:
			return fmt.Errorf("payload rejected with status %q", status)
		}
	}
}

// payloadStatusFromResult extracts the payload status from a newPayload
// (top-level status) or forkchoiceUpdated (payloadStatus.status) result.
func payloadStatusFromResult(method string, result json.RawMessage) (string, error) {
	if strings.HasPrefix(method, "engine_forkchoiceUpdated") {
		var r struct {
			PayloadStatus struct {
				Status string `json:"status"`
			} `json:"payloadStatus"`
		}
		if err := json.Unmarshal(result, &r); err != nil {
			return "", fmt.Errorf("parsing forkchoiceUpdated result: %w", err)
		}

		return r.PayloadStatus.Status, nil
	}

	var r struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return "", fmt.Errorf("parsing newPayload result: %w", err)
	}

	return r.Status, nil
}

// bumpGasLimit builds empty blocks until the head's gas limit reaches target or
// maxBlocks blocks have been built. Each block can move the limit by at most
// 1/1024, so a ramp takes many blocks. Returns the number of blocks built.
//
// Up, the filler follows its own (very high) miner gas ceiling and the ramp
// stops once the limit passes target. Down, each block carries payload
// attribute targetGasLimit=target, the CL's gas limit target since Amsterdam: a
// pre-Amsterdam block has no such attribute and keeps rising, so a ramp down is
// refused unless the very first block comes back lower.
func (c *engineClient) bumpGasLimit(ctx context.Context, target uint64, maxBlocks int, log logrus.FieldLogger) (int, error) {
	head, err := c.latestBlock(ctx)
	if err != nil {
		return 0, err
	}

	gasLimit := head.gasLimit
	down := gasLimit > target

	if gasLimit == target || (!down && gasLimit >= target) {
		log.WithFields(logrus.Fields{"gas_limit": gasLimit, "target": target}).
			Info("Head gas limit already at target; skipping gas bump")

		return 0, nil
	}

	reached := func(gl uint64) bool {
		if down {
			return gl <= target
		}

		return gl >= target
	}

	if down {
		c.targetGasLimit = target
	}

	log.WithFields(logrus.Fields{"from": gasLimit, "target": target, "max_blocks": maxBlocks, "down": down}).
		Info("Ramping block gas limit")

	built := 0
	lastLog := time.Now()

	for built < maxBlocks {
		select {
		case <-ctx.Done():
			return built, ctx.Err()
		default:
		}

		_, gl, buildErr := c.buildBlock(ctx, nil, nil)
		if buildErr != nil {
			return built, fmt.Errorf("building gas-bump block %d: %w", built+1, buildErr)
		}

		if down && built == 0 && gl >= gasLimit {
			return built + 1, fmt.Errorf(
				"gas limit did not fall (%d -> %d): lowering it needs an Amsterdam block, "+
					"whose payload attributes carry targetGasLimit", gasLimit, gl,
			)
		}

		built++
		gasLimit = gl

		if reached(gasLimit) {
			break
		}

		if time.Since(lastLog) >= 5*time.Second {
			log.WithFields(logrus.Fields{"blocks": built, "gas_limit": gasLimit, "target": target}).
				Info("Gas bump in progress")

			lastLog = time.Now()
		}
	}

	if !reached(gasLimit) {
		return built, fmt.Errorf(
			"gas limit reached %d after %d blocks, not yet at target %d "+
				"(raise gas_bump_max_blocks)",
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

	blockHash, _, err := c.buildBlock(ctx, withdrawals, nil)
	if err != nil {
		return "", fmt.Errorf("building funding block: %w", err)
	}

	return blockHash, nil
}

// toRecordedPayload marshals a newPayload method + params into a recordedPayload
// (each param as raw JSON), for the replay bundle.
func toRecordedPayload(method string, params []any) (recordedPayload, error) {
	raw := make([]json.RawMessage, 0, len(params))

	for i, p := range params {
		b, err := json.Marshal(p)
		if err != nil {
			return recordedPayload{}, fmt.Errorf("marshaling %s param %d: %w", method, i, err)
		}

		raw = append(raw, b)
	}

	return recordedPayload{Method: method, Params: raw}, nil
}

// payloadBlockHash extracts the executionPayload.blockHash from a newPayload
// param[0].
func payloadBlockHash(execPayload json.RawMessage) (string, error) {
	var p struct {
		BlockHash string `json:"blockHash"`
	}
	if err := json.Unmarshal(execPayload, &p); err != nil {
		return "", fmt.Errorf("parsing execution payload: %w", err)
	}

	if p.BlockHash == "" {
		return "", fmt.Errorf("execution payload has no blockHash")
	}

	return p.BlockHash, nil
}

// hexToUint64 parses a 0x-prefixed (or bare) hex string to uint64.
func hexToUint64(s string) (uint64, error) {
	return strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 64)
}

// uintToHex formats v as a 0x-prefixed hex string (Engine API quantity form).
func uintToHex(v uint64) string {
	return "0x" + strconv.FormatUint(v, 16)
}
