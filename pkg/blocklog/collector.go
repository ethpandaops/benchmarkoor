package blocklog

import (
	"encoding/json"
	"io"
	"sync"
)

// Collector intercepts log streams, parses JSON payloads from client logs,
// and associates them with the currently executing test.
type Collector interface {
	// SetCurrentTest sets the test name for subsequent log entries.
	SetCurrentTest(testName string)

	// ClearCurrentTest clears the current test name.
	ClearCurrentTest()

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
		parser:     parser,
		downstream: downstream,
		blockLogs:  make(map[string]json.RawMessage, 64),
	}
}

type collector struct {
	parser     Parser
	downstream io.Writer

	mu          sync.RWMutex
	currentTest string
	blockLogs   map[string]json.RawMessage

	// Line buffering for the writer.
	bufMu   sync.Mutex
	lineBuf []byte
}

// Ensure interface compliance.
var _ Collector = (*collector)(nil)

// SetCurrentTest sets the test name for subsequent log entries.
func (c *collector) SetCurrentTest(testName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentTest = testName
}

// ClearCurrentTest clears the current test name.
func (c *collector) ClearCurrentTest() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentTest = ""
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
			w.collector.mu.Lock()
			if w.collector.currentTest != "" {
				// Store the payload for the current test.
				// Each test produces one block, so we overwrite if already present.
				w.collector.blockLogs[w.collector.currentTest] = payload
			}
			w.collector.mu.Unlock()
		}
	}

	w.collector.bufMu.Unlock()

	return n, nil
}
