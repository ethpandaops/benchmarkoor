package scenario

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/ethereum/go-ethereum/core/types"

	"github.com/ethpandaops/benchmarkoor/pkg/mempool/cldriver"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/observer"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/txfactory"
)

// importFullPool compares the import of a block with an empty pool and with a
// full pool. Both blocks have the same shape: one transfer from each of the
// same senders. The "full" block is built before the filler txs arrive, so
// the filler is in the pool during the import but not in the block.
type importFullPool struct{}

func (s *importFullPool) Name() string { return "import-full-pool" }

func (s *importFullPool) Description() string {
	return "Times newPayload, forkchoiceUpdated and the pool reset (until the pool settles) " +
		"for the same block shape with an almost empty pool and with a full pool of txs " +
		"that the block does not include."
}

func (s *importFullPool) Defaults() Params {
	return Params{
		"block_txs":         200,  // transfers in each measured block
		"filler_senders":    300,  // senders of the filler txs (300x16 + 200 is near the 5120 limit)
		"filler_per_sender": 16,   // filler nonces per sender (at most the account slots)
		"rounds":            10,   // baseline/full pairs; the order alternates
		"quiet_ms":          1000, // wait before each import, in both cases
		"timeout_ms":        60000,
	}
}

func (s *importFullPool) Accounts(p Params) int {
	return p.Int("block_txs") + p.Int("filler_senders")
}

type importSample struct {
	newPayload time.Duration
	fcu        time.Duration
	settle     time.Duration
}

func (s *importFullPool) Run(ctx context.Context, env *Env) error {
	p := env.Params
	timeout := p.Duration("timeout_ms")
	fillerTotal := p.Int("filler_senders") * p.Int("filler_per_sender")

	blockSenders, err := env.Accounts(ctx, p.Int("block_txs"))
	if err != nil {
		return err
	}

	fillers, err := env.Accounts(ctx, p.Int("filler_senders"))
	if err != nil {
		return err
	}

	var base, full []importSample

	for round := range p.Int("rounds") {
		// Alternate the order, so neither case always follows a drain.
		fullFirst := round%2 == 1

		for _, isFull := range []bool{fullFirst, !fullFirst} {
			if !isFull {
				// Baseline: the pool holds only the block txs.
				b, err := s.measure(ctx, env, blockSenders, nil, timeout,
					fmt.Sprintf("round %d baseline", round))
				if err != nil {
					return fmt.Errorf("round %d baseline: %w", round, err)
				}

				base = append(base, b)

				continue
			}

			// Full: the same shape, plus fillerTotal txs in the pool.
			f, err := s.measure(ctx, env, blockSenders, fillers, timeout,
				fmt.Sprintf("round %d full pool", round))
			if err != nil {
				return fmt.Errorf("round %d full: %w", round, err)
			}

			full = append(full, f)

			// Clear the filler.
			if _, err := env.Drain(ctx, 2+fillerTotal/1000); err != nil {
				return fmt.Errorf("round %d drain: %w", round, err)
			}
		}
	}

	for _, set := range []struct {
		name    string
		label   string
		samples []importSample
	}{
		{"empty", "empty pool", base},
		{"full", fmt.Sprintf("%d txs in the pool", fillerTotal), full},
	} {
		for _, x := range set.samples {
			env.Rec.Latency("new_payload."+set.name, "newPayload, "+set.label, set.name, x.newPayload)
			env.Rec.Latency("fcu."+set.name, "forkchoiceUpdated, "+set.label, set.name, x.fcu)
			env.Rec.Latency("settle."+set.name, "Pool settled after the head change, "+set.label, set.name, x.settle)
		}
	}

	// Medians: one slow import (a GC pause, a JIT compile) must not decide
	// the overhead.
	median := func(xs []importSample, f func(importSample) time.Duration) time.Duration {
		v := make([]time.Duration, 0, len(xs))
		for _, x := range xs {
			v = append(v, f(x))
		}

		slices.Sort(v)

		if len(v) == 0 {
			return 0
		}

		if len(v)%2 == 1 {
			return v[len(v)/2]
		}

		return (v[len(v)/2-1] + v[len(v)/2]) / 2
	}

	for _, m := range []struct {
		key   string
		label string
		f     func(importSample) time.Duration
	}{
		{"new_payload", "newPayload", func(x importSample) time.Duration { return x.newPayload }},
		{"fcu", "forkchoiceUpdated", func(x importSample) time.Duration { return x.fcu }},
		{"settle", "pool settle", func(x importSample) time.Duration { return x.settle }},
	} {
		b, f := median(base, m.f), median(full, m.f)

		env.Rec.Metric(Metric{
			Key: m.key + ".empty", Label: m.label + ", empty pool (median)", Unit: "ms",
			Value: Ms(b), Group: m.key, Better: "lower",
		})
		env.Rec.Metric(Metric{
			Key: m.key + ".full", Label: fmt.Sprintf("%s, %d txs in the pool (median)", m.label, fillerTotal),
			Unit: "ms", Value: Ms(f), Group: m.key, Better: "lower",
		})

		if b > 0 {
			env.Rec.Metric(Metric{
				Key: m.key + ".overhead", Label: m.label + ": full-pool overhead", Unit: "%",
				Value: round3(100 * (float64(f) - float64(b)) / float64(b)), Group: m.key, Better: "lower",
			})
		}
	}

	return nil
}

// measure sends one tx per block sender, builds the block, optionally fills
// the pool, then imports the block and times it until the pool settles.
func (s *importFullPool) measure(ctx context.Context, env *Env, senders, fillers []*txfactory.Account,
	timeout time.Duration, label string,
) (importSample, error) {
	var out importSample

	blockTxs := make([]*types.Transaction, 0, len(senders))

	for _, a := range senders {
		tx, err := env.Factory.Transfer(a, txfactory.KindDynamicFee, txfactory.DefaultFees())
		if err != nil {
			return out, err
		}

		blockTxs = append(blockTxs, tx)
	}

	if err := env.MustSend(ctx, blockTxs, label+", block txs"); err != nil {
		return out, fmt.Errorf("sending the block txs: %w", err)
	}

	if _, err := env.WaitInPool(ctx, time.Now(), Hashes(blockTxs), timeout); err != nil {
		return out, err
	}

	head, err := env.Head(ctx)
	if err != nil {
		return out, err
	}

	payload, err := env.BuildWith(ctx, head, Hashes(blockTxs), cldriver.BuildOptions{})
	if err != nil {
		return out, err
	}

	env.Rec.CheckOnce(SeverityHard, len(payload.Txs) == len(blockTxs), "The measured block holds exactly the block txs",
		fmt.Sprintf("%d txs", len(blockTxs)), fmt.Sprintf("%d txs (%s)", len(payload.Txs), label))

	// The filler arrives after the build: in the pool, not in the block.
	fillerTxs := make([]*types.Transaction, 0, len(fillers)*env.Params.Int("filler_per_sender"))

	if len(fillers) > 0 {
		lanes := make([][]*types.Transaction, 0, len(fillers))

		for _, a := range fillers {
			lane := make([]*types.Transaction, 0, env.Params.Int("filler_per_sender"))

			for range env.Params.Int("filler_per_sender") {
				tx, err := env.Factory.Transfer(a, txfactory.KindDynamicFee, txfactory.DefaultFees())
				if err != nil {
					return out, err
				}

				lane = append(lane, tx)
			}

			lanes = append(lanes, lane)
		}

		fillerTxs = flatten(lanes)
		if _, rejected, err := env.SendConcurrent(ctx, regroup(lanes, 8), "", "", "", label+", filler"); rejected > 0 {
			return out, fmt.Errorf("%d filler txs rejected: %w", rejected, err)
		}

		want := len(blockTxs) + len(fillerTxs)
		if _, err := env.WaitStatus(ctx, time.Now(), timeout, func(st observer.Status) bool {
			return st.Pending >= want
		}); err != nil {
			return out, fmt.Errorf("waiting for %d pending txs: %w", want, err)
		}
	}

	// The same quiet period in both cases: background work from the sends
	// settles before the measured import.
	select {
	case <-ctx.Done():
		return out, ctx.Err()
	case <-time.After(env.Params.Duration("quiet_ms")):
	}

	start := time.Now()

	t, err := env.Import(ctx, payload)
	if err != nil {
		return out, err
	}

	out.newPayload = t.NewPayload
	out.fcu = t.Fcu

	// Settled: the pending count drops to the filler count, which means the
	// block txs left the pool. Some clients reset the pool in the background
	// after the head change, so the fcu return is not the end.
	settle, err := env.WaitStatus(ctx, start, timeout, func(st observer.Status) bool {
		return st.Pending == len(fillerTxs)
	})
	if !env.Rec.CheckOnce(SeverityHard, err == nil, "The pool settles to the filler count after the import",
		fmt.Sprintf("%d pending", len(fillerTxs)), ErrText(err)) {
		return out, nil
	}

	in, err := env.Pool.InPool(ctx, Hashes(blockTxs))
	if err != nil {
		return out, err
	}

	left := 0

	for _, tx := range blockTxs {
		if in[tx.Hash()] {
			left++
		}
	}

	env.Rec.CheckOnce(SeverityHard, left == 0, "Included txs leave the pool", "0 in the pool",
		fmt.Sprintf("%d in the pool", left))

	out.settle = settle
	env.Rec.Event("settled", "%s: pool settled %.1fms after newPayload", label, Ms(out.settle))

	return out, nil
}
