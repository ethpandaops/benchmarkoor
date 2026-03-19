package generate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/sha3"
)

// TxMetadata contains the transaction metadata parsed from the JSON-RPC id field.
type TxMetadata struct {
	TestID  string `json:"testId"`
	Phase   string `json:"phase"`
	TxIndex int    `json:"txIndex"`
}

// Proxy is a Go reverse proxy that intercepts eth_sendRawTransaction calls,
// builds blocks via the Engine API, and captures the resulting payloads.
type Proxy struct {
	log           logrus.FieldLogger
	listenAddr    string
	clientRPCURL  string
	engine        *EngineClient
	fixtureWriter *FixtureWriter

	server *http.Server
	port   int

	mu           sync.Mutex
	currentGroup *txGroup
}

// txGroup tracks buffered transactions for a single (testId, phase) group.
type txGroup struct {
	testID string
	phase  string
	rawTxs []string
}

// NewProxy creates a new reverse proxy.
func NewProxy(
	log logrus.FieldLogger,
	listenAddr string,
	clientRPCURL string,
	engine *EngineClient,
	fixtureWriter *FixtureWriter,
) *Proxy {
	return &Proxy{
		log:           log,
		listenAddr:    listenAddr,
		clientRPCURL:  clientRPCURL,
		engine:        engine,
		fixtureWriter: fixtureWriter,
	}
}

// Start begins listening for JSON-RPC requests.
func (p *Proxy) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", p.handleRequest)

	listener, err := net.Listen("tcp", p.listenAddr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", p.listenAddr, err)
	}

	p.port = listener.Addr().(*net.TCPAddr).Port

	p.server = &http.Server{
		Handler: mux,
	}

	go func() {
		if err := p.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			p.log.WithError(err).Error("Proxy server error")
		}
	}()

	p.log.WithField("addr", listener.Addr().String()).Info("Proxy started")

	return nil
}

// Port returns the port the proxy is listening on.
func (p *Proxy) Port() int {
	return p.port
}

// Stop gracefully stops the proxy and flushes any remaining buffered transactions.
func (p *Proxy) Stop(ctx context.Context) error {
	// Flush remaining buffer.
	if err := p.flushCurrentGroup(ctx); err != nil {
		p.log.WithError(err).Warn("Failed to flush remaining transactions")
	}

	if p.server != nil {
		return p.server.Shutdown(ctx)
	}

	return nil
}

// handleRequest routes incoming JSON-RPC requests.
func (p *Proxy) handleRequest(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer func() { _ = r.Body.Close() }()

	// Parse the JSON-RPC request.
	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
		ID      json.RawMessage `json:"id"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON-RPC request", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	if req.Method == "eth_sendRawTransaction" {
		p.handleSendRawTransaction(ctx, w, req.ID, req.Params)
		return
	}

	// Forward all other requests transparently.
	p.forwardRequest(ctx, w, body)
}

// handleSendRawTransaction intercepts eth_sendRawTransaction,
// buffers the raw transaction, and returns a success response.
func (p *Proxy) handleSendRawTransaction(
	ctx context.Context,
	w http.ResponseWriter,
	id json.RawMessage,
	params json.RawMessage,
) {
	// Parse metadata from the id field.
	meta, err := parseTxMetadata(id)
	if err != nil {
		p.log.WithError(err).Warn("Failed to parse tx metadata from id, using defaults")

		meta = &TxMetadata{
			TestID: "unknown",
			Phase:  "testing",
		}
	}

	// Parse the raw transaction from params.
	var paramList []string
	if err := json.Unmarshal(params, &paramList); err != nil || len(paramList) == 0 {
		writeJSONRPCError(w, id, -32602, "invalid params")
		return
	}

	rawTx := paramList[0]

	p.mu.Lock()

	// Check if group boundary crossed.
	if p.currentGroup != nil &&
		(p.currentGroup.testID != meta.TestID || p.currentGroup.phase != meta.Phase) {
		// Flush current group before starting new one.
		group := p.currentGroup
		p.currentGroup = nil
		p.mu.Unlock()

		if flushErr := p.flushGroup(ctx, group); flushErr != nil {
			p.log.WithError(flushErr).Error("Failed to flush transaction group")
			writeJSONRPCError(w, id, -32603, "block building failed")

			return
		}

		p.mu.Lock()
	}

	// Add to current group (or start new one).
	if p.currentGroup == nil {
		p.currentGroup = &txGroup{
			testID: meta.TestID,
			phase:  meta.Phase,
			rawTxs: make([]string, 0, 4),
		}
	}

	p.currentGroup.rawTxs = append(p.currentGroup.rawTxs, rawTx)
	p.mu.Unlock()

	// Compute transaction hash and return success.
	txHash := computeTxHash(rawTx)

	p.log.WithFields(logrus.Fields{
		"test_id":  meta.TestID,
		"phase":    meta.Phase,
		"tx_index": meta.TxIndex,
		"tx_hash":  txHash,
	}).Debug("Buffered transaction")

	writeJSONRPCResult(w, id, fmt.Sprintf("%q", txHash))
}

// forwardRequest forwards a JSON-RPC request to the client and returns the response.
func (p *Proxy) forwardRequest(
	ctx context.Context,
	w http.ResponseWriter,
	body []byte,
) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, p.clientRPCURL, bytes.NewReader(body),
	)
	if err != nil {
		http.Error(w, "failed to create upstream request", http.StatusInternalServerError)
		return
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)

	_, _ = io.Copy(w, resp.Body)
}

// flushCurrentGroup flushes the current transaction group buffer.
func (p *Proxy) flushCurrentGroup(ctx context.Context) error {
	p.mu.Lock()
	group := p.currentGroup
	p.currentGroup = nil
	p.mu.Unlock()

	if group == nil {
		return nil
	}

	return p.flushGroup(ctx, group)
}

// flushGroup builds a block from the buffered transactions and writes the fixture.
func (p *Proxy) flushGroup(ctx context.Context, group *txGroup) error {
	p.log.WithFields(logrus.Fields{
		"test_id": group.testID,
		"phase":   group.phase,
		"txns":    len(group.rawTxs),
	}).Info("Building block for transaction group")

	block, err := p.engine.BuildBlockWithTxns(ctx, group.rawTxs, nil, "")
	if err != nil {
		return fmt.Errorf("building block: %w", err)
	}

	if err := p.fixtureWriter.WriteTestBlock(group.testID, group.phase, block); err != nil {
		return fmt.Errorf("writing fixture: %w", err)
	}

	return nil
}

// parseTxMetadata parses the transaction metadata from the JSON-RPC id field.
// Expected format: {"testId": "...", "phase": "...", "txIndex": N}
func parseTxMetadata(id json.RawMessage) (*TxMetadata, error) {
	var meta TxMetadata
	if err := json.Unmarshal(id, &meta); err != nil {
		return nil, fmt.Errorf("parsing metadata: %w", err)
	}

	if meta.TestID == "" {
		return nil, fmt.Errorf("testId is empty")
	}

	return &meta, nil
}

// computeTxHash computes the keccak256 hash of a raw transaction.
func computeTxHash(rawTx string) string {
	rawTx = strings.TrimPrefix(rawTx, "0x")

	txBytes := make([]byte, len(rawTx)/2)

	for i := 0; i < len(rawTx); i += 2 {
		txBytes[i/2] = hexToByte(rawTx[i])<<4 | hexToByte(rawTx[i+1])
	}

	hash := sha3.NewLegacyKeccak256()
	hash.Write(txBytes)

	result := hash.Sum(nil)

	return "0x" + hexEncode(result)
}

// writeJSONRPCResult writes a successful JSON-RPC response.
func writeJSONRPCResult(w http.ResponseWriter, id json.RawMessage, result string) {
	w.Header().Set("Content-Type", "application/json")

	resp := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":%s,"result":%s}`,
		string(id), result,
	)
	_, _ = w.Write([]byte(resp))
}

// writeJSONRPCError writes a JSON-RPC error response.
func writeJSONRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	w.Header().Set("Content-Type", "application/json")

	resp := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":%s,"error":{"code":%d,"message":"%s"}}`,
		string(id), code, message,
	)
	_, _ = w.Write([]byte(resp))
}

// hexToByte converts a single hex character to its byte value.
func hexToByte(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	default:
		return 0
	}
}

// hexEncode encodes bytes to a hex string.
func hexEncode(b []byte) string {
	const hex = "0123456789abcdef"

	result := make([]byte, len(b)*2)

	for i, v := range b {
		result[i*2] = hex[v>>4]
		result[i*2+1] = hex[v&0x0f]
	}

	return string(result)
}
