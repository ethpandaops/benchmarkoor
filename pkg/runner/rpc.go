package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/ethpandaops/benchmarkoor/pkg/jsonrpc"
	"github.com/sirupsen/logrus"
)

// waitForRPC waits for the RPC endpoint to be ready and returns the client version.
func (r *runner) waitForRPC(ctx context.Context, host string, port int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.cfg.ReadyTimeout)
	defer cancel()

	url := fmt.Sprintf("http://%s:%d", host, port)

	ticker := time.NewTicker(DefaultHealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("timeout waiting for RPC: %w", ctx.Err())
		case <-ticker.C:
			if version, ok := r.checkRPCHealth(ctx, url); ok {
				return version, nil
			}
		}
	}
}

// checkRPCHealth performs a single RPC health check and returns the client version on success.
func (r *runner) checkRPCHealth(ctx context.Context, url string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	body := `{"jsonrpc":"2.0","method":"web3_clientVersion","params":[],"id":1}`

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return "", false
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", false
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false
	}

	var rpcResp struct {
		Result string `json:"result"`
	}

	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return "", false
	}

	return rpcResp.Result, true
}

// getLatestBlock fetches the latest block number, hash, and state root from the RPC endpoint.
func (r *runner) getLatestBlock(ctx context.Context, host string, port int) (uint64, string, string, error) {
	return r.getBlockByTag(ctx, host, port, "latest")
}

// getBlockByTag fetches a block by tag, passed to eth_getBlockByNumber verbatim
// ("latest", "finalized", "safe" or a hex number). A tag with no block behind it
// yields a zero hash and no error, so callers can treat "not set" as ordinary.
func (r *runner) getBlockByTag(
	ctx context.Context,
	host string,
	port int,
	tag string,
) (uint64, string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	url := fmt.Sprintf("http://%s:%d", host, port)
	body := fmt.Sprintf(
		`{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":[%q,false],"id":1}`, tag)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return 0, "", "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", "", fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, "", "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", "", fmt.Errorf("reading response: %w", err)
	}

	var rpcResp struct {
		Result struct {
			Number    string `json:"number"`
			Hash      string `json:"hash"`
			StateRoot string `json:"stateRoot"`
		} `json:"result"`
	}

	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return 0, "", "", fmt.Errorf("parsing response: %w", err)
	}

	// A tag with no block behind it comes back as null, not an error.
	if rpcResp.Result.Hash == "" {
		return 0, "", "", nil
	}

	// Parse hex block number.
	blockNum, err := strconv.ParseUint(strings.TrimPrefix(rpcResp.Result.Number, "0x"), 16, 64)
	if err != nil {
		return 0, "", "", fmt.Errorf("parsing block number: %w", err)
	}

	return blockNum, rpcResp.Result.Hash, rpcResp.Result.StateRoot, nil
}

// resolveRootAnchor picks the block safe/finalized point at for the whole run:
// a configured hash, else the client's own finalized block when it already sits
// below the head, else the head's parent.
//
// It must sit strictly below the block the fixtures replay from — the datadir
// head at bootstrap. geth will not move its head to a block at or below the one
// it considers finalized; it answers VALID and does nothing, so the anchor
// becomes unreachable. The zero hash is no help: clients read it as "no update",
// and the engine API only permits it "unless transition block is finalized".
func (r *runner) resolveRootAnchor(
	ctx context.Context,
	log logrus.FieldLogger,
	host string,
	rpcPort int,
	configured string,
) (string, error) {
	if configured != "" {
		log.WithField("root_anchor", configured).Info(
			"Using configured root anchor for safe/finalized")

		return configured, nil
	}

	headNum, _, _, err := r.getBlockByTag(ctx, host, rpcPort, "latest")
	if err != nil {
		return "", fmt.Errorf("fetching the head for root anchor resolution: %w", err)
	}

	if finNum, finHash, _, err := r.getBlockByTag(ctx, host, rpcPort, "finalized"); err == nil &&
		finHash != "" && finNum < headNum {
		log.WithFields(logrus.Fields{"root_anchor": finHash, "block": finNum}).Info(
			"Reusing the client's finalized block as the root anchor")

		return finHash, nil
	}

	if headNum == 0 {
		return "", fmt.Errorf("head is the genesis block; no room for a root anchor below it")
	}

	parentNum := headNum - 1

	_, parentHash, _, err := r.getBlockByTag(ctx, host, rpcPort, fmt.Sprintf("0x%x", parentNum))
	if err != nil {
		return "", fmt.Errorf("fetching block %d for the root anchor: %w", parentNum, err)
	}

	if parentHash == "" {
		return "", fmt.Errorf("block %d not available for the root anchor", parentNum)
	}

	log.WithFields(logrus.Fields{"root_anchor": parentHash, "block": parentNum}).Info(
		"Using the head's parent as the root anchor")

	return parentHash, nil
}

// resetForkchoiceAnchor points safe/finalized at a block strictly below the
// datadir head, once per instance before any test runs.
//
// Prestates are built by advancing a snapshot with forkchoiceUpdated calls
// setting head = safe = finalized on every block, so the block every fixture
// replays from arrives already finalized — and geth will not move its head
// back to it. That only bites once a test leaves the head far away
// (test_blockhash builds 258 blocks): the anchor's state is no longer retained
// and the call that would rebuild it is refused, stranding every later test.
//
// Unconditional, not part of bootstrap_fcu, which most configs leave unset.
func (r *runner) resetForkchoiceAnchor(
	ctx context.Context,
	log logrus.FieldLogger,
	host string,
	enginePort int,
	rpcPort int,
	headBlockHash string,
	configured string,
) error {
	anchor, err := r.resolveRootAnchor(ctx, log, host, rpcPort, configured)
	if err != nil {
		return fmt.Errorf("resolving root anchor: %w", err)
	}

	payload := fmt.Sprintf(
		`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3",`+
			`"params":[{"headBlockHash":"%s","safeBlockHash":"%s",`+
			`"finalizedBlockHash":"%s"},null],"id":1}`,
		headBlockHash, anchor, anchor,
	)

	url := fmt.Sprintf("http://%s:%d", host, enginePort)
	if err := r.doBootstrapFCURequest(ctx, url, payload); err != nil {
		return fmt.Errorf("sending forkchoice anchor reset: %w", err)
	}

	log.WithFields(logrus.Fields{
		"head":        headBlockHash,
		"root_anchor": anchor,
	}).Info("Reset safe/finalized to the root anchor")

	return nil
}

// sendBootstrapFCU sends an engine_forkchoiceUpdatedV3 call to confirm the
// client is fully synced and ready for test execution. The call is retried
// up to cfg.MaxRetries times with cfg.Backoff between attempts — some clients
// (e.g., Erigon) may still be performing internal initialization after RPC
// becomes available. A VALID response confirms the client is ready.
//
// rootAnchorBlockHash is the block safe/finalized point at; "" keeps the
// previous behaviour of sending the zero hash.
func (r *runner) sendBootstrapFCU(
	ctx context.Context,
	log logrus.FieldLogger,
	host string,
	enginePort int,
	headBlockHash string,
	rootAnchorBlockHash string,
	cfg *config.BootstrapFCUConfig,
) error {
	const zeroHash = "0x0000000000000000000000000000000000000000000000000000000000000000"

	backoff, err := time.ParseDuration(cfg.Backoff)
	if err != nil {
		return fmt.Errorf("parsing backoff duration: %w", err)
	}

	// The zero hash cannot move a stale marker down: clients read it as
	// "no update".
	anchor := rootAnchorBlockHash
	if anchor == "" {
		anchor = zeroHash
	}

	// Build the forkchoiceUpdatedV3 payload.
	payload := fmt.Sprintf(
		`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3",`+
			`"params":[{"headBlockHash":"%s","safeBlockHash":"%s",`+
			`"finalizedBlockHash":"%s"},null],"id":1}`,
		headBlockHash, anchor, anchor,
	)

	url := fmt.Sprintf("http://%s:%d", host, enginePort)

	log.WithFields(logrus.Fields{
		"max_retries": cfg.MaxRetries,
		"backoff":     cfg.Backoff,
		"payload":     payload,
	}).Info("Sending bootstrap FCU")

	var lastErr error

	for attempt := 1; attempt <= cfg.MaxRetries; attempt++ {
		lastErr = r.doBootstrapFCURequest(ctx, url, payload)
		if lastErr == nil {
			log.WithField("head_block_hash", headBlockHash).Info(
				"Bootstrap FCU sent successfully",
			)

			return nil
		}

		log.WithFields(logrus.Fields{
			"attempt": attempt,
			"max":     cfg.MaxRetries,
			"error":   lastErr.Error(),
		}).Warn("Bootstrap FCU attempt failed, retrying")

		if attempt < cfg.MaxRetries {
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return fmt.Errorf("context cancelled during retry: %w", ctx.Err())
			}
		}
	}

	return fmt.Errorf("bootstrap FCU failed after %d attempts: %w", cfg.MaxRetries, lastErr)
}

// doBootstrapFCURequest performs a single bootstrap FCU HTTP request.
func (r *runner) doBootstrapFCURequest(
	ctx context.Context,
	url string,
	payload string,
) error {
	const requestTimeout = 30 * time.Second

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	token, err := executor.GenerateJWTToken(r.cfg.JWT)
	if err != nil {
		return fmt.Errorf("generating JWT: %w", err)
	}

	req, err := http.NewRequestWithContext(
		reqCtx, http.MethodPost, url, strings.NewReader(payload),
	)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	rpcResp, err := jsonrpc.Parse(string(body))
	if err != nil {
		return fmt.Errorf("parsing response: %w", err)
	}

	// Validate the response using the FCU validator.
	validator := &jsonrpc.ForkchoiceUpdatedValidator{}
	if err := validator.Validate("engine_forkchoiceUpdatedV3", rpcResp); err != nil {
		return fmt.Errorf("validating response: %w", err)
	}

	return nil
}
