package executor

import (
	"github.com/sirupsen/logrus"
)

// TxCounts is the per-test container persisted under "tx_counts" in
// summary.json. Each step that actually contains engine_newPayload*
// lines gets a populated array — one element per newPayload in step
// order, holding the transaction count for that block. Steps with no
// newPayload activity are omitted entirely.
//
// JSON shape:
//
//	"tx_counts": {
//	  "setup":   [tx_count_block_1, tx_count_block_2, ...],
//	  "test":    [tx_count_block_1, ...],
//	  "cleanup": [tx_count_block_1, ...]
//	}
type TxCounts struct {
	Setup   []uint64 `json:"setup,omitempty"`
	Test    []uint64 `json:"test,omitempty"`
	Cleanup []uint64 `json:"cleanup,omitempty"`
}

// HasData returns true if any step carries at least one block.
func (t *TxCounts) HasData() bool {
	if t == nil {
		return false
	}

	return len(t.Setup) > 0 || len(t.Test) > 0 || len(t.Cleanup) > 0
}

// ComputeTxCountsForStep returns the transaction count for each
// engine_newPayloadV* line in the given step lines, in step order.
// Lines that fail to parse are skipped silently — the function mirrors
// ComputePayloadSizeBuckets, which is the source-of-truth path.
func ComputeTxCountsForStep(lines []string) []uint64 {
	nps := ExtractNewPayloadLines(lines)

	out := make([]uint64, 0, len(nps))
	for _, np := range nps {
		if np.Payload == nil {
			continue
		}

		out = append(out, uint64(len(np.Payload.Transactions)))
	}

	return out
}

// MergeTxCounts attaches a TxCounts container to tests that don't
// already carry one. The lineProvider callback returns the JSON-RPC
// lines for a given (test, step) pair — callers can fetch from disk
// or from memory as appropriate. Returning nil/empty for a step skips
// that step.
//
// Tests that already carry any tx-count data are left alone (idempotent
// re-run).
func MergeTxCounts(
	log logrus.FieldLogger,
	tests []SuiteTest,
	lineProvider func(testName string, step StepKind) []string,
) {
	for i := range tests {
		t := &tests[i]
		if t.TxCounts.HasData() {
			continue
		}

		var (
			merged  TxCounts
			anyData bool
		)

		for _, step := range []StepKind{StepKindSetup, StepKindTest, StepKindCleanup} {
			lines := lineProvider(t.Name, step)
			if len(lines) == 0 {
				continue
			}

			counts := ComputeTxCountsForStep(lines)
			if len(counts) == 0 {
				continue
			}

			switch step {
			case StepKindSetup:
				merged.Setup = counts
			case StepKindTest:
				merged.Test = counts
			case StepKindCleanup:
				merged.Cleanup = counts
			}

			anyData = true
		}

		if anyData {
			m := merged
			t.TxCounts = &m
		}
	}

	_ = log // signature parity with MergePayloadSizes; not used today.
}
