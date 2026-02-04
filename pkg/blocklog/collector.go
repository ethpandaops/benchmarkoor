package blocklog

import (
	"encoding/json"
	"io"
	"sync"
)

// Collector intercepts log streams, parses JSON payloads from client logs,
// and associates them with tests using blockHash matching.
type Collector interface {
	// RegisterBlockHash registers a blockHash for a test name.
	// When a log line with this blockHash is seen, it will be associated with the test.
	// If the log already arrived (buffered in unmatched), it's immediately associated.
	RegisterBlockHash(testName, blockHash string)

	// GetBlockLogs returns all captured block logs mapped by test name.
	GetBlockLogs() map[string]json.RawMessage

	// Writer returns an io.Writer that intercepts log lines, parses them,
	// and passes them through to the downstream writer.
	Writer() io.Writer
}

// NewCollector creates a new block log collector with the given parser
// and downstream writer.
func NewCollector(parser Parser, downstream io.Writer) Collector {
	return &collector{
		parser:        parser,
		downstream:    downstream,
		pendingHashes: make(map[string]string, 64),
		blockLogs:     make(map[string]json.RawMessage, 64),
		unmatched:     make(map[string]json.RawMessage, 64),
	}
}

type collector struct {
	parser     Parser
	downstream io.Writer

	mu            sync.RWMutex
	pendingHashes map[string]string          // blockHash -> testName (awaiting log)
	blockLogs     map[string]json.RawMessage // testName -> payload (matched)
	unmatched     map[string]json.RawMessage // blockHash -> payload (logs before registration)

	// Line buffering for the writer.
	bufMu   sync.Mutex
	lineBuf []byte
}

// Ensure interface compliance.
var _ Collector = (*collector)(nil)

// RegisterBlockHash registers a blockHash for a test name.
// If a log with this hash was already seen, it's immediately matched.
func (c *collector) RegisterBlockHash(testName, blockHash string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if we already have a buffered log for this hash (late registration).
	if payload, ok := c.unmatched[blockHash]; ok {
		c.blockLogs[testName] = payload
		delete(c.unmatched, blockHash)

		return
	}

	// Otherwise, register and wait for the log to arrive.
	c.pendingHashes[blockHash] = testName
}

// GetBlockLogs returns all captured block logs.
func (c *collector) GetBlockLogs() map[string]json.RawMessage {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Return a copy to avoid concurrent modification.
	result := make(map[string]json.RawMessage, len(c.blockLogs))
	for k, v := range c.blockLogs {
		result[k] = v
	}

	return result
}

// Writer returns an io.Writer that intercepts and parses log lines.
func (c *collector) Writer() io.Writer {
	return &collectorWriter{collector: c}
}

// extractBlockHashFromPayload extracts the block hash from a parsed log payload.
// All clients use the same structure: { "block": { "hash": "0x..." } }.
func extractBlockHashFromPayload(payload json.RawMessage) (string, bool) {
	var bp struct {
		Block struct {
			Hash string `json:"hash"`
		} `json:"block"`
	}

	if err := json.Unmarshal(payload, &bp); err != nil || bp.Block.Hash == "" {
		return "", false
	}

	return bp.Block.Hash, true
}

// collectorWriter implements io.Writer and wraps the collector.
type collectorWriter struct {
	collector *collector
}

// Ensure interface compliance.
var _ io.Writer = (*collectorWriter)(nil)

// Write implements io.Writer.
func (w *collectorWriter) Write(p []byte) (n int, err error) {
	n = len(p)

	// First, write to downstream (always pass through).
	if w.collector.downstream != nil {
		if _, err := w.collector.downstream.Write(p); err != nil {
			return n, err
		}
	}

	// Buffer and process lines.
	w.collector.bufMu.Lock()
	w.collector.lineBuf = append(w.collector.lineBuf, p...)

	// Process complete lines.
	for {
		idx := -1
		for i, b := range w.collector.lineBuf {
			if b == '\n' {
				idx = i
				break
			}
		}

		if idx == -1 {
			break
		}

		// Extract the line (without newline).
		line := string(w.collector.lineBuf[:idx])
		w.collector.lineBuf = w.collector.lineBuf[idx+1:]

		// Try to parse JSON from this line.
		if payload, ok := w.collector.parser.ParseLine(line); ok {
			// Extract blockHash from the payload.
			if blockHash, hashOK := extractBlockHashFromPayload(payload); hashOK {
				w.collector.mu.Lock()
				// Check if we have a pending registration for this hash.
				if testName, pending := w.collector.pendingHashes[blockHash]; pending {
					// Match found: store payload and clean up.
					w.collector.blockLogs[testName] = payload
					delete(w.collector.pendingHashes, blockHash)
				} else {
					// No registration yet: buffer for late registration.
					w.collector.unmatched[blockHash] = payload
				}
				w.collector.mu.Unlock()
			}
		}
	}

	w.collector.bufMu.Unlock()

	return n, nil
}
