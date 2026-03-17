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
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	url := fmt.Sprintf("http://%s:%d", host, port)
	body := `{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest",false],"id":1}`

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

	// Parse hex block number.
	blockNum, err := strconv.ParseUint(strings.TrimPrefix(rpcResp.Result.Number, "0x"), 16, 64)
	if err != nil {
		return 0, "", "", fmt.Errorf("parsing block number: %w", err)
	}

	return blockNum, rpcResp.Result.Hash, rpcResp.Result.StateRoot, nil
}

// sendBootstrapFCU sends an engine_forkchoiceUpdatedV3 call to confirm the
// client is fully synced and ready for test execution. The call is retried
// up to cfg.MaxRetries times with cfg.Backoff between attempts — some clients
// (e.g., Erigon) may still be performing internal initialization after RPC
// becomes available. A VALID response confirms the client is ready.
func (r *runner) sendBootstrapFCU(
	ctx context.Context,
	log logrus.FieldLogger,
	host string,
	enginePort int,
	headBlockHash string,
	cfg *config.BootstrapFCUConfig,
) error {
	const zeroHash = "0x0000000000000000000000000000000000000000000000000000000000000000"

	backoff, err := time.ParseDuration(cfg.Backoff)
	if err != nil {
		return fmt.Errorf("parsing backoff duration: %w", err)
	}

	// Build the forkchoiceUpdatedV3 payload.
	payload := fmt.Sprintf(
		`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3",`+
			`"params":[{"headBlockHash":"%s","safeBlockHash":"%s",`+
			`"finalizedBlockHash":"%s"},null],"id":1}`,
		headBlockHash, zeroHash, zeroHash,
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
