package runner

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

// blobForkOrder lists the blob-bearing forks in activation order. When a fork
// override adds a fork the genesis blobSchedule doesn't yet cover, the new fork
// inherits the schedule of the latest preceding fork present here — geth-family
// clients reject an active fork that carries no blob schedule.
var blobForkOrder = []string{"cancun", "prague", "osaka", "amsterdam"}

// applyGenesisForkOverrides patches a geth-format genesis JSON so the given
// forks activate at the given timestamps. It is the genesis-file equivalent of
// geth's --override.<fork> flag, for clients that instead read their fork
// schedule from the genesis (besu, reth, ethrex). For each fork it sets
// config.<fork>Time and, when a blobSchedule is present but lacks the fork,
// inherits the latest preceding fork's blob parameters.
//
// Only the top-level "config" object is rewritten; every other field round-trips
// verbatim and existing numbers are preserved exactly (so the genesis block hash
// is unchanged). It returns an error if the genesis is not geth-format (has no
// top-level "config" object), since the patch shape is format-specific.
func applyGenesisForkOverrides(genesis []byte, overrides map[string]uint64) ([]byte, error) {
	if len(overrides) == 0 {
		return genesis, nil
	}

	// Decode only the top level so untouched fields round-trip byte-for-byte.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(genesis, &top); err != nil {
		return nil, fmt.Errorf("parsing genesis json: %w", err)
	}

	rawConfig, ok := top["config"]
	if !ok {
		return nil, fmt.Errorf(
			"genesis has no \"config\" object; genesis_fork_override only " +
				"supports geth-format genesis files",
		)
	}

	// UseNumber keeps existing numbers as json.Number so they re-encode exactly
	// rather than via lossy float64.
	dec := json.NewDecoder(bytes.NewReader(rawConfig))
	dec.UseNumber()

	cfg := make(map[string]any)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parsing genesis config: %w", err)
	}

	for fork, ts := range overrides {
		cfg[fork+"Time"] = ts
		inheritBlobSchedule(cfg, fork)
	}

	patchedConfig, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("encoding patched genesis config: %w", err)
	}

	top["config"] = patchedConfig

	patched, err := json.Marshal(top)
	if err != nil {
		return nil, fmt.Errorf("encoding patched genesis: %w", err)
	}

	return patched, nil
}

// applyGenesisEIPOverrides patches a parity/nethermind-format chainspec so the
// given EIPs activate at the override timestamp. It is the parity-format
// counterpart of applyGenesisForkOverrides: parity chainspecs schedule forks
// per-EIP (params.eip<N>TransitionTimestamp) rather than by fork name, so the
// devnet-specific EIP list comes from config.
//
// Only the "params" object is rewritten; every other field round-trips verbatim.
// It returns an error if the genesis is not parity-format (has no top-level
// "params" object).
func applyGenesisEIPOverrides(genesis []byte, override *config.GenesisEIPOverride) ([]byte, error) {
	if override == nil || len(override.EIPs) == 0 {
		return genesis, nil
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(genesis, &top); err != nil {
		return nil, fmt.Errorf("parsing genesis json: %w", err)
	}

	rawParams, ok := top["params"]
	if !ok {
		return nil, fmt.Errorf(
			"genesis has no \"params\" object; genesis_eip_override only " +
				"supports parity/nethermind-format chainspecs",
		)
	}

	dec := json.NewDecoder(bytes.NewReader(rawParams))
	dec.UseNumber()

	params := make(map[string]any)
	if err := dec.Decode(&params); err != nil {
		return nil, fmt.Errorf("parsing genesis params: %w", err)
	}

	// Parity transition timestamps are hex-encoded strings (e.g. "0x1").
	ts := fmt.Sprintf("0x%x", override.Timestamp)
	for _, eip := range override.EIPs {
		params[fmt.Sprintf("eip%dTransitionTimestamp", eip)] = ts
	}

	patchedParams, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("encoding patched genesis params: %w", err)
	}

	top["params"] = patchedParams

	patched, err := json.Marshal(top)
	if err != nil {
		return nil, fmt.Errorf("encoding patched genesis: %w", err)
	}

	return patched, nil
}

// inheritBlobSchedule ensures cfg.blobSchedule covers fork by copying the
// schedule of the latest preceding fork in blobForkOrder. It is a no-op when the
// genesis has no blobSchedule, the fork already has a schedule, the fork is
// unknown to blobForkOrder, or no preceding schedule exists.
func inheritBlobSchedule(cfg map[string]any, fork string) {
	schedule, ok := cfg["blobSchedule"].(map[string]any)
	if !ok {
		return
	}

	if _, exists := schedule[fork]; exists {
		return
	}

	forkIdx := indexOf(blobForkOrder, fork)
	if forkIdx < 0 {
		return
	}

	// Walk backwards to the nearest preceding fork that has a schedule.
	for i := forkIdx - 1; i >= 0; i-- {
		if prev, ok := schedule[blobForkOrder[i]]; ok {
			schedule[fork] = prev

			return
		}
	}
}

// indexOf returns the index of target in list, or -1 if absent.
func indexOf(list []string, target string) int {
	for i, v := range list {
		if v == target {
			return i
		}
	}

	return -1
}
