package mempool

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/params"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

// genesisFile is one client-specific genesis file.
type genesisFile struct {
	name string
	data []byte
}

// clientGenesisFiles returns the genesis of the mempool chain in the format
// of each client: geth genesis.json, besu genesis.json, nethermind
// chainspec.json. All three hold the same chain.
func clientGenesisFiles(m config.MempoolConfig) (map[string]genesisFile, error) {
	gethJSON, err := genesisJSON(m)
	if err != nil {
		return nil, err
	}

	g := buildGenesis(m)

	besuJSON, err := json.MarshalIndent(besuGenesis(g), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding the besu genesis: %w", err)
	}

	nmJSON, err := json.MarshalIndent(nethermindChainspec(g), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding the nethermind chainspec: %w", err)
	}

	return map[string]genesisFile{
		"geth":       {name: "genesis.geth.json", data: gethJSON},
		"besu":       {name: "genesis.besu.json", data: besuJSON},
		"nethermind": {name: "chainspec.nethermind.json", data: nmJSON},
	}, nil
}

// allocJSON is the alloc as hex quantities, code and storage, which both
// besu and nethermind read.
func allocJSON(g *gethGenesis) map[string]any {
	out := make(map[string]any, len(g.Alloc))

	for addr, acc := range g.Alloc {
		entry := map[string]any{
			"balance": (*hexutil.Big)(acc.Balance).String(),
			"nonce":   hexutil.Uint64(acc.Nonce).String(),
		}

		if len(acc.Code) > 0 {
			entry["code"] = hexutil.Bytes(acc.Code).String()
		}

		if len(acc.Storage) > 0 {
			storage := make(map[string]string, len(acc.Storage))
			for k, v := range acc.Storage {
				storage[k.Hex()] = v.Hex()
			}

			entry["storage"] = storage
		}

		out[addr.Hex()] = entry
	}

	return out
}

// besuGenesis follows the besu genesis.json of the EEST releases: a geth-like
// file with the request contract addresses and an empty ethash section.
func besuGenesis(g *gethGenesis) map[string]any {
	c := g.Config
	blobs := c.BlobScheduleConfig

	blobCfg := func(b *params.BlobConfig) map[string]any {
		return map[string]any{"target": b.Target, "max": b.Max, "baseFeeUpdateFraction": b.UpdateFraction}
	}

	return map[string]any{
		"config": map[string]any{
			"ethash":                              map[string]any{},
			"chainID":                             c.ChainID.Uint64(),
			"homesteadBlock":                      0,
			"eip150Block":                         0,
			"eip155Block":                         0,
			"eip158Block":                         0,
			"byzantiumBlock":                      0,
			"constantinopleBlock":                 0,
			"constantinopleFixBlock":              0,
			"istanbulBlock":                       0,
			"berlinBlock":                         0,
			"londonBlock":                         0,
			"terminalTotalDifficulty":             0,
			"shanghaiTime":                        0,
			"cancunTime":                          0,
			"pragueTime":                          0,
			"osakaTime":                           0,
			"depositContractAddress":              c.DepositContractAddress.Hex(),
			"withdrawalRequestContractAddress":    params.WithdrawalQueueAddress.Hex(),
			"consolidationRequestContractAddress": params.ConsolidationQueueAddress.Hex(),
			"blobSchedule": map[string]any{
				"cancun": blobCfg(blobs.Cancun),
				"prague": blobCfg(blobs.Prague),
				"osaka":  blobCfg(blobs.Osaka),
			},
		},
		"nonce":         "0x0",
		"timestamp":     hexutil.Uint64(g.Timestamp).String(),
		"extraData":     g.ExtraData.String(),
		"gasLimit":      g.GasLimit.String(),
		"difficulty":    "0x0",
		"mixHash":       g.MixHash.Hex(),
		"coinbase":      g.Coinbase.Hex(),
		"baseFeePerGas": g.BaseFee.String(),
		"alloc":         allocJSON(g),
	}
}

// nethermindTransitions are the chainspec params a merged chain up to Osaka
// sets from genesis, as in the EEST Osaka chainspec.
var nethermindTransitions = []string{
	"eip150Transition", "eip160Transition", "eip161abcTransition", "eip161dTransition",
	"eip155Transition", "maxCodeSizeTransition", "eip140Transition", "eip211Transition",
	"eip214Transition", "eip658Transition", "eip145Transition", "eip1014Transition",
	"eip1052Transition", "eip1283Transition", "eip1283DisableTransition", "eip152Transition",
	"eip1108Transition", "eip1344Transition", "eip1884Transition", "eip2028Transition",
	"eip2200Transition", "eip2565Transition", "eip2718Transition", "eip2929Transition",
	"eip2930Transition", "eip1559Transition", "eip3238Transition", "eip3529Transition",
	"eip3541Transition", "eip3198Transition",
}

var nethermindTimestampTransitions = []string{
	"eip3651TransitionTimestamp", "eip3855TransitionTimestamp", "eip3860TransitionTimestamp",
	"eip4895TransitionTimestamp", "eip4844TransitionTimestamp", "eip4788TransitionTimestamp",
	"eip1153TransitionTimestamp", "eip5656TransitionTimestamp", "eip6780TransitionTimestamp",
	"eip2537TransitionTimestamp", "eip2935TransitionTimestamp", "eip6110TransitionTimestamp",
	"eip7002TransitionTimestamp", "eip7251TransitionTimestamp", "eip7685TransitionTimestamp",
	"eip7702TransitionTimestamp", "eip7623TransitionTimestamp", "eip7594TransitionTimestamp",
	"eip7823TransitionTimestamp", "eip7825TransitionTimestamp", "eip7883TransitionTimestamp",
	"eip7918TransitionTimestamp", "eip7951TransitionTimestamp", "eip7939TransitionTimestamp",
	"eip7934TransitionTimestamp",
}

// nethermindChainspec follows the nethermind chainspec.json of the EEST Osaka
// releases. The merge comes from --Merge.TerminalTotalDifficulty=0.
func nethermindChainspec(g *gethGenesis) map[string]any {
	c := g.Config
	osaka := c.BlobScheduleConfig.Osaka

	p := map[string]any{
		"maxCodeSize":            24576,
		"maximumExtraDataSize":   "0x400",
		"depositContractAddress": c.DepositContractAddress.Hex(),
		"eip7934MaxRlpBlockSize": "0x800000",
		"chainID":                hexutil.Uint64(c.ChainID.Uint64()).String(),
		"blobSchedule": []map[string]any{{
			"timestamp":             "0x0",
			"target":                hexutil.Uint64(osaka.Target).String(),
			"max":                   hexutil.Uint64(osaka.Max).String(),
			"baseFeeUpdateFraction": hexutil.Uint64(osaka.UpdateFraction).String(),
		}},
	}

	keys := append(append([]string{}, nethermindTransitions...), nethermindTimestampTransitions...)
	sort.Strings(keys)

	for _, k := range keys {
		p[k] = "0x0"
	}

	return map[string]any{
		"version": "1",
		"engine": map[string]any{
			"Ethash": map[string]any{"params": map[string]any{
				"minimumDifficulty":      "0x20000",
				"difficultyBoundDivisor": "0x800",
				"durationLimit":          "0x0d",
				"homesteadTransition":    "0x0",
				"eip100bTransition":      "0x0",
				"blockReward":            map[string]any{"0x0": "0x0"},
			}},
		},
		"params": p,
		"genesis": map[string]any{
			"seal": map[string]any{"ethereum": map[string]any{
				"nonce": "0x0000000000000000", "mixHash": g.MixHash.Hex(),
			}},
			"difficulty":            "0x00",
			"author":                g.Coinbase.Hex(),
			"timestamp":             hexutil.Uint64(g.Timestamp).String(),
			"parentHash":            "0x0000000000000000000000000000000000000000000000000000000000000000",
			"extraData":             g.ExtraData.String(),
			"gasLimit":              g.GasLimit.String(),
			"baseFeePerGas":         g.BaseFee.String(),
			"blobGasUsed":           "0x00",
			"excessBlobGas":         "0x00",
			"parentBeaconBlockRoot": "0x0000000000000000000000000000000000000000000000000000000000000000",
		},
		"accounts": allocJSON(g),
	}
}
