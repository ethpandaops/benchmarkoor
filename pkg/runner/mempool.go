package runner

import (
	"github.com/ethpandaops/benchmarkoor/pkg/client"
	"github.com/ethpandaops/benchmarkoor/pkg/config"
)

// mempoolSettings maps the mempool config to the client profile inputs.
func mempoolSettings(cfg *config.Config) client.MempoolSettings {
	m := cfg.Runner.Benchmark.GetMempool()

	return client.MempoolSettings{
		Limits: client.PoolLimits{
			AccountSlots:  m.PoolLimits.AccountSlots,
			GlobalSlots:   m.PoolLimits.GlobalSlots,
			AccountQueue:  m.PoolLimits.AccountQueue,
			GlobalQueue:   m.PoolLimits.GlobalQueue,
			PriceBump:     m.PoolLimits.PriceBump,
			BlobPriceBump: m.PoolLimits.BlobPriceBump,
		},
		GasLimit: m.GasLimit,
		P2PPort:  client.DefaultMempoolP2PPort,
	}
}

// executorMode returns the mode to record in config.json. It is empty for the
// replay mode, so the replay config.json stays byte-identical to the files
// written before the mode existed.
func executorMode(cfg *config.Config) string {
	if cfg == nil || !cfg.IsMempoolMode() {
		return ""
	}

	return config.ExecutorModeMempool
}
