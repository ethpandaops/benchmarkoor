package builder

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// forkOverrideActivationFlag returns the geth --override.<fork>=<timestamp> flag
// that schedules fork at the snapshot genesis block's timestamp + 1, read from
// baseGenesisPath (the state-actor snapshot's geth-genesis.json). The filler
// boots at the snapshot head (block N, timestamp T) and builds block N+1 at
// timestamp T+1, so fork activates exactly on the first block it builds.
//
// state-actor bakes the snapshot state into the DB under an empty-alloc genesis,
// so --override.genesis can't be used (geth recomputes an empty-state genesis
// and rejects the hash mismatch). The per-fork --override.<fork> flag instead
// amends only the in-memory chain config at boot, leaving the genesis block
// untouched. Requires a geth build that registers the flag (e.g.
// ethpandaops/geth:bal-devnet-7-amsterdam-override for amsterdam).
func forkOverrideActivationFlag(baseGenesisPath, fork string) (string, error) {
	ts, err := genesisFileTimestamp(baseGenesisPath)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("--override.%s=%d", strings.ToLower(fork), ts+1), nil
}

// genesisFileTimestamp reads the genesis block timestamp from the top-level
// "timestamp" field of a geth genesis JSON file.
func genesisFileTimestamp(path string) (uint64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("reading base genesis %q: %w", path, err)
	}

	var genesis map[string]any
	if err := json.Unmarshal(raw, &genesis); err != nil {
		return 0, fmt.Errorf("parsing base genesis %q: %w", path, err)
	}

	ts, err := genesisBlockTimestamp(genesis)
	if err != nil {
		return 0, fmt.Errorf("base genesis %q: %w", path, err)
	}

	return ts, nil
}

// genesisBlockTimestamp reads the genesis block's timestamp from the top-level
// "timestamp" field, accepting a 0x-prefixed hex string (geth's encoding) or a
// JSON number.
func genesisBlockTimestamp(genesis map[string]any) (uint64, error) {
	v, ok := genesis["timestamp"]
	if !ok {
		return 0, fmt.Errorf("missing \"timestamp\" field")
	}

	switch t := v.(type) {
	case string:
		ts, err := parseGenesisUint(t)
		if err != nil {
			return 0, fmt.Errorf("parsing \"timestamp\" %q: %w", t, err)
		}

		return ts, nil
	case float64:
		return uint64(t), nil
	default:
		return 0, fmt.Errorf("unexpected \"timestamp\" type %T", v)
	}
}

// parseGenesisUint parses a uint from a genesis-encoded string: 0x-prefixed hex
// or plain decimal.
func parseGenesisUint(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if rest, ok := strings.CutPrefix(s, "0x"); ok {
		return strconv.ParseUint(rest, 16, 64)
	}

	if rest, ok := strings.CutPrefix(s, "0X"); ok {
		return strconv.ParseUint(rest, 16, 64)
	}

	return strconv.ParseUint(s, 10, 64)
}
