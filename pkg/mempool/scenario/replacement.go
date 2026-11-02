package scenario

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/ethpandaops/benchmarkoor/pkg/mempool/txfactory"
)

// replacement checks the price-bump rules for a tx with the same sender and
// nonce as a tx in the pool.
type replacement struct{}

func (s *replacement) Name() string { return "replacement" }

func (s *replacement) Description() string {
	return "Replaces a pool tx (same sender and nonce) with different fee bumps. Below the " +
		"price bump the pool must keep the old tx; at or above it, the new tx must take its " +
		"place. Both the fee cap and the tip must rise. Blob txs need the blob price bump."
}

func (s *replacement) Defaults() Params {
	return Params{"timeout_ms": 10000}
}

// replacementCase is one replacement attempt.
type replacementCase struct {
	key    string
	label  string
	kind   txfactory.Kind // kind of the original tx
	queued bool           // the original sits behind a nonce gap
	// build returns the replacement for an original with fees f.
	build func(f txfactory.Fees) (txfactory.Kind, txfactory.Fees)
	// accept is the expected outcome under the pinned price bumps. The
	// bump rules are pool policy, so a different outcome is a finding.
	accept bool
}

func (s *replacement) cases(limits PoolLimits) []replacementCase {
	bump := int64(limits.PriceBump)
	blobBump := int64(limits.BlobPriceBump)
	dyn := txfactory.KindDynamicFee

	same := func(k txfactory.Kind) func(txfactory.Fees) (txfactory.Kind, txfactory.Fees) {
		return func(f txfactory.Fees) (txfactory.Kind, txfactory.Fees) { return k, f }
	}
	bumped := func(k txfactory.Kind, pct int64) func(txfactory.Fees) (txfactory.Kind, txfactory.Fees) {
		return func(f txfactory.Fees) (txfactory.Kind, txfactory.Fees) { return k, f.Bump(pct) }
	}

	return []replacementCase{
		{"same_fees", "Same fees, different content", dyn, false, same(dyn), false},
		{"below_bump", fmt.Sprintf("Fee cap and tip +%d%% (below the %d%% bump)", bump/2, bump),
			dyn, false, bumped(dyn, bump/2), false},
		{"at_bump", fmt.Sprintf("Fee cap and tip +%d%%", bump), dyn, false, bumped(dyn, bump), true},
		{"double", "Fee cap and tip +100%", dyn, false, bumped(dyn, 100), true},
		{"fee_cap_only", fmt.Sprintf("Only the fee cap +%d%%", bump), dyn, false,
			func(f txfactory.Fees) (txfactory.Kind, txfactory.Fees) {
				b := f.Bump(bump)

				return dyn, txfactory.Fees{FeeCap: b.FeeCap, TipCap: f.TipCap}
			}, false},
		{"tip_only", fmt.Sprintf("Only the tip +%d%%", bump), dyn, false,
			func(f txfactory.Fees) (txfactory.Kind, txfactory.Fees) {
				b := f.Bump(bump)

				return dyn, txfactory.Fees{FeeCap: f.FeeCap, TipCap: b.TipCap}
			}, false},
		// A legacy tx pays its gas price as the tip, so the replacement
		// bumps the tip from the gas price too.
		{"legacy_to_dynamic", fmt.Sprintf("Legacy replaced by dynamic-fee, gas price +%d%% as fee cap and tip", bump),
			txfactory.KindLegacy, false,
			func(f txfactory.Fees) (txfactory.Kind, txfactory.Fees) {
				b := f.Bump(bump)

				return dyn, txfactory.Fees{FeeCap: b.FeeCap, TipCap: b.FeeCap}
			}, true},
		{"queued_at_bump", fmt.Sprintf("Queued tx, fee cap and tip +%d%%", bump), dyn, true,
			bumped(dyn, bump), true},
		{"blob_at_tx_bump", fmt.Sprintf("Blob tx, all fees +%d%% (below the %d%% blob bump)", bump, blobBump),
			txfactory.KindBlob, false, bumped(txfactory.KindBlob, bump), blobBump <= bump},
		{"blob_at_blob_bump", fmt.Sprintf("Blob tx, all fees +%d%%", blobBump), txfactory.KindBlob, false,
			bumped(txfactory.KindBlob, blobBump), true},
		{"blob_to_dynamic", "Blob tx replaced by a dynamic-fee tx, fees +100%", txfactory.KindBlob, false,
			bumped(dyn, 100), false},
	}
}

func (s *replacement) Accounts(Params) int { return 16 }

func (s *replacement) Run(ctx context.Context, env *Env) error {
	timeout := env.Params.Duration("timeout_ms")
	cases := s.cases(env.Limits)

	accounts, err := env.Accounts(ctx, len(cases))
	if err != nil {
		return err
	}

	for i, c := range cases {
		env.Rec.Event("send", "%s: original, then replacement", c.key)

		if err := s.runCase(ctx, env, accounts[i], c, timeout); err != nil {
			return fmt.Errorf("%s: %w", c.key, err)
		}
	}

	_, err = env.Drain(ctx, 8)

	return err
}

func (s *replacement) runCase(ctx context.Context, env *Env, a *txfactory.Account,
	c replacementCase, timeout time.Duration,
) error {
	fees := txfactory.Fees{FeeCap: txfactory.GweiN(10), TipCap: txfactory.GweiN(1), BlobFeeCap: txfactory.GweiN(5)}

	nonce := a.Nonce()
	if c.queued {
		nonce++
	}

	original, err := env.Factory.Build(a, txfactory.Template{Kind: c.kind, Nonce: new(nonce), Fees: fees})
	if err != nil {
		return err
	}

	if _, err := env.Send(ctx, original); err != nil {
		return fmt.Errorf("sending the original: %w", err)
	}

	if _, err := env.WaitInPool(ctx, time.Now(), []common.Hash{original.Hash()}, timeout); err != nil {
		return fmt.Errorf("waiting for the original: %w", err)
	}

	kind, newFees := c.build(fees)

	// Different content: value 2 instead of 1.
	repl, err := env.Factory.Build(a, txfactory.Template{
		Kind: kind, Nonce: new(nonce), Fees: newFees, Value: big.NewInt(2),
	})
	if err != nil {
		return err
	}

	start := time.Now()
	_, sendErr := env.Send(ctx, repl)
	accepted := sendErr == nil

	env.Rec.Observe("replace."+c.key, c.label, SendText(sendErr))

	want := "rejected"
	if c.accept {
		want = "accepted"
	}

	env.Rec.Check(SeveritySoft, accepted == c.accept, "Replacement: "+c.label, want, SendText(sendErr))

	if accepted {
		// The new tx must take the place of the old one, not sit next to it.
		took, err := env.WaitGone(ctx, start, []common.Hash{original.Hash()}, timeout)
		env.Rec.Check(SeverityHard, err == nil, "Replaced tx leaves the pool: "+c.label,
			"gone", ErrText(err))

		if err == nil {
			env.Rec.Latency("replace_visible", "Time until a replacement takes the place of the old tx",
				"replacement", took)
		}

		in, err := env.Pool.InPool(ctx, []common.Hash{repl.Hash()})
		if err != nil {
			return err
		}

		env.Rec.Check(SeverityHard, in[repl.Hash()], "Accepted replacement is in the pool: "+c.label,
			"in the pool", fmt.Sprintf("%v", in[repl.Hash()]))
	} else {
		in, err := env.Pool.InPool(ctx, []common.Hash{original.Hash()})
		if err != nil {
			return err
		}

		env.Rec.Check(SeverityHard, in[original.Hash()], "Original stays after a rejected replacement: "+c.label,
			"in the pool", fmt.Sprintf("%v", in[original.Hash()]))
	}

	// Fill the gap of a queued case, so the drain can clear the sender.
	if c.queued {
		fill, err := env.Factory.Build(a, txfactory.Template{
			Kind: txfactory.KindDynamicFee, Nonce: new(nonce - 1), Fees: fees,
		})
		if err != nil {
			return err
		}

		if _, err := env.Send(ctx, fill); err != nil {
			return fmt.Errorf("sending the gap fill: %w", err)
		}
	}

	a.SetNonce(nonce + 1)

	return nil
}
