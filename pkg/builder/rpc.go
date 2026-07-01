package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// healthCheckInterval is how often waitForRPC polls the endpoint.
const healthCheckInterval = time.Second

// waitForRPC blocks until the EL client at host:port answers
// web3_clientVersion or ctx is cancelled, returning the client version.
// These helpers mirror pkg/runner/rpc.go but are standalone so the builder
// does not depend on the runner.
func waitForRPC(ctx context.Context, host string, port int) (string, error) {
	url := fmt.Sprintf("http://%s:%d", host, port)

	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("timeout waiting for RPC at %s: %w", url, ctx.Err())
		case <-ticker.C:
			if version, ok := checkRPCHealth(ctx, url); ok {
				return version, nil
			}
		}
	}
}

// getLatestBlockHash returns the hash of the latest block at host:port.
func getLatestBlockHash(ctx context.Context, host string, port int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	url := fmt.Sprintf("http://%s:%d", host, port)
	body := `{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest",false],"id":1}`

	resp, err := postJSON(ctx, url, body)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	var rpcResp struct {
		Result struct {
			Hash string `json:"hash"`
		} `json:"result"`
	}

	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	if rpcResp.Result.Hash == "" {
		return "", fmt.Errorf("latest block has no hash (client not ready?)")
	}

	return rpcResp.Result.Hash, nil
}

// checkRPCHealth performs a single web3_clientVersion call, returning the
// version on success.
func checkRPCHealth(ctx context.Context, url string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	body := `{"jsonrpc":"2.0","method":"web3_clientVersion","params":[],"id":1}`

	resp, err := postJSON(ctx, url, body)
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

// postJSON issues a JSON-RPC POST and returns the response.
func postJSON(ctx context.Context, url, body string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	return resp, nil
}
