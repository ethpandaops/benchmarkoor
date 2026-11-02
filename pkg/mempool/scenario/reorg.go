package scenario

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/ethpandaops/benchmarkoor/pkg/mempool/cldriver"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/txfactory"
)

// reorg builds two sibling blocks A1 and B1 on the same parent from the pool,
// makes A1 the head, then switches the head to B1.
//
// A builder takes every pending tx that fits, so the two blocks get different
// content by two means:
//   - Order: B1 is built first. Txs sent after that are only in A1.
//   - Gas: X and Y each burn more than half the gas limit (a call to an
//     endless loop uses its whole gas limit). X has the higher
//     tip, so A1 takes X and leaves Y out; B1 was built before X existed.
//
// After the switch to B1:
//   - X and the A-only txs are valid on B and must come back (reinjection).
//   - Y and the common txs are in B1 and must leave the pool.
//   - T (A1) and T' (B1) share a sender and nonce: T is invalid on B and
//     must not come back.
//   - E (sent while A1 was the head) is in neither block and must stay.
type reorg struct{}

func (s *reorg) Name() string { return "reorg-reinjection" }

func (s *reorg) Description() string {
	return "Builds sibling blocks A1 and B1 from the pool, sets the head to A1, then reorgs to " +
		"B1. Checks that A-only txs come back, B-included txs leave, a tx that B made invalid " +
		"is dropped and other pending txs stay. Times each."
}

func (s *reorg) Defaults() Params {
	return Params{
		"common_txs":    8, // txs in both blocks
		"a_only_txs":    8, // small txs only in A1
		"bystander_txs": 8, // txs sent while A1 is the head
		"iterations":    3,
		"big_tx_gas":    0, // gas of X and Y; 0 = just over half the gas limit
		"timeout_ms":    30000,
	}
}

func (s *reorg) perIteration(p Params) int {
	return p.Int("common_txs") + p.Int("a_only_txs") + p.Int("bystander_txs") + 3
}

func (s *reorg) Accounts(p Params) int { return p.Int("iterations") * s.perIteration(p) }

func (s *reorg) Run(ctx context.Context, env *Env) error {
	for i := range env.Params.Int("iterations") {
		if err := s.iteration(ctx, env, i); err != nil {
			return fmt.Errorf("iteration %d: %w", i, err)
		}
	}

	return nil
}

// bigGas returns the gas limit of X and Y: two of them must not fit in one
// block, and one must fit under the EIP-7825 cap.
func (s *reorg) bigGas(env *Env) (uint64, error) {
	gas := uint64(env.Params.Int("big_tx_gas"))
	if gas == 0 {
		gas = env.Chain.GasLimit/2 + 1_000_000
	}

	if gas > env.Chain.MaxTxGas || 2*gas <= env.Chain.GasLimit {
		return 0, fmt.Errorf("big tx gas %d: need gas limit/2 < gas <= %d (gas limit %d)",
			gas, env.Chain.MaxTxGas, env.Chain.GasLimit)
	}

	return gas, nil
}

func (s *reorg) iteration(ctx context.Context, env *Env, iter int) error {
	p := env.Params
	timeout := p.Duration("timeout_ms")

	bigGas, err := s.bigGas(env)
	if err != nil {
		return err
	}

	accounts, err := env.Accounts(ctx, s.perIteration(p))
	if err != nil {
		return err
	}

	nc, na := p.Int("common_txs"), p.Int("a_only_txs")
	commonAcc := accounts[:nc]
	aOnlyAcc := accounts[nc : nc+na]
	byAcc := accounts[nc+na : len(accounts)-3]
	victim, xAcc, yAcc := accounts[len(accounts)-3], accounts[len(accounts)-2], accounts[len(accounts)-1]

	mid := txfactory.Fees{FeeCap: txfactory.GweiN(20), TipCap: txfactory.GweiN(2)}
	low := txfactory.Fees{FeeCap: txfactory.GweiN(10), TipCap: txfactory.GweiN(1)}
	lowest := txfactory.Fees{FeeCap: txfactory.GweiN(20), TipCap: big2(5, 8)}
	high := txfactory.Fees{FeeCap: txfactory.GweiN(20), TipCap: txfactory.GweiN(5)}

	transfers := func(accs []*txfactory.Account) ([]*types.Transaction, error) {
		out := make([]*types.Transaction, 0, len(accs))

		for _, a := range accs {
			tx, err := env.Factory.Transfer(a, txfactory.KindDynamicFee, mid)
			if err != nil {
				return nil, err
			}

			out = append(out, tx)
		}

		return out, nil
	}

	parent, err := env.Head(ctx)
	if err != nil {
		return err
	}

	// 1. Build B1 from: common, T', Y.
	common1, err := transfers(commonAcc)
	if err != nil {
		return err
	}

	victimNonce := victim.Nonce()

	tPrime, err := env.Factory.Build(victim, txfactory.Template{Kind: txfactory.KindDynamicFee, Fees: low})
	if err != nil {
		return err
	}

	y, err := env.Factory.Build(yAcc, burn(lowest, bigGas))
	if err != nil {
		return err
	}

	bTxs := append(append([]*types.Transaction{}, common1...), tPrime, y)
	if err := env.MustSend(ctx, bTxs); err != nil {
		return err
	}

	if _, err := env.WaitInPool(ctx, time.Now(), Hashes(bTxs), timeout); err != nil {
		return err
	}

	b1, err := env.BuildWith(ctx, parent, Hashes(bTxs), cldriver.BuildOptions{Salt: uint64(iter)*2 + 1})
	if err != nil {
		return fmt.Errorf("building B1: %w", err)
	}

	// 2. Build A1 from: common, T (replaces T'), X, A-only. Y does not fit.
	t, err := env.Factory.Build(victim, txfactory.Template{
		Kind: txfactory.KindDynamicFee, Nonce: new(victimNonce), Fees: mid, Value: big.NewInt(2),
	})
	if err != nil {
		return err
	}

	x, err := env.Factory.Build(xAcc, burn(high, bigGas))
	if err != nil {
		return err
	}

	aOnly, err := transfers(aOnlyAcc)
	if err != nil {
		return err
	}

	aNew := append([]*types.Transaction{t, x}, aOnly...)
	if err := env.MustSend(ctx, aNew); err != nil {
		return err
	}

	if _, err := env.WaitInPool(ctx, time.Now(), Hashes(aNew), timeout); err != nil {
		return err
	}

	aWant := append(append([]common.Hash{}, Hashes(common1)...), Hashes(aNew)...)

	a1, err := env.BuildWith(ctx, parent, aWant, cldriver.BuildOptions{Salt: uint64(iter)*2 + 2})
	if err != nil {
		return fmt.Errorf("building A1: %w", err)
	}

	if a1.Contains(y.Hash()) {
		return fmt.Errorf("A1 includes Y: the gas limit (%d) holds both big txs", a1.Block.GasLimit)
	}

	// 3. Import both, head = A1.
	if _, err := env.Driver.NewPayload(ctx, b1); err != nil {
		return fmt.Errorf("importing B1 as a side block: %w", err)
	}

	if _, err := env.Import(ctx, a1); err != nil {
		return err
	}

	if _, err := env.WaitGone(ctx, time.Now(), aWant, timeout); err != nil {
		return fmt.Errorf("waiting for the A1 txs to leave the pool: %w", err)
	}

	// 4. Bystanders while A1 is the head.
	bystanders, err := transfers(byAcc)
	if err != nil {
		return err
	}

	if err := env.MustSend(ctx, bystanders); err != nil {
		return err
	}

	if _, err := env.WaitInPool(ctx, time.Now(), Hashes(bystanders), timeout); err != nil {
		return err
	}

	// 5. Reorg to B1.
	start := time.Now()

	_, fcuTook, err := env.Driver.ForkchoiceUpdated(ctx, b1.Block.Hash, nil)
	if err != nil {
		return fmt.Errorf("reorg to B1: %w", err)
	}

	env.Rec.Event("reorg", "head A1 -> B1 (block %d) in %.2fms", b1.Block.Number, Ms(fcuTook))
	env.Rec.Latency("reorg_fcu", "forkchoiceUpdated for a 1-block reorg", "reorg", fcuTook)

	bIncluded := append(Hashes(common1), y.Hash())

	gone, err := env.WaitGone(ctx, start, bIncluded, timeout)
	env.Rec.CheckOnce(SeverityHard, err == nil, "Txs included in B1 leave the pool", "gone", ErrText(err))

	if err == nil {
		env.Rec.Latency("reorg_removed", "Time until B-included txs leave the pool", "reorg", gone)
	}

	reinjected := append([]common.Hash{x.Hash()}, Hashes(aOnly)...)

	back, err := env.WaitInPool(ctx, start, reinjected, timeout)
	env.Rec.CheckOnce(SeveritySoft, err == nil, "A-only txs come back to the pool after the reorg",
		fmt.Sprintf("%d txs", len(reinjected)), ErrText(err))

	if err == nil {
		env.Rec.Latency("reorg_reinjected", "Time until A-only txs are back in the pool", "reorg", back)
	}

	settled := max(gone, back)
	env.Rec.Latency("reorg_settled", "Time until the pool settles after the reorg", "reorg", settled)
	env.Rec.Event("settled", "iteration %d: pool settled %.1fms after the reorg", iter, Ms(settled))

	in, err := env.Pool.InPool(ctx, append(append([]common.Hash{t.Hash()}, Hashes(bystanders)...), Hashes(common1)...))
	if err != nil {
		return err
	}

	env.Rec.CheckOnce(SeverityHard, !in[t.Hash()], "A tx that B1 made invalid (same nonce) is not in the pool",
		"not in the pool", fmt.Sprintf("in the pool: %v", in[t.Hash()]))

	lost := 0

	for _, tx := range bystanders {
		if !in[tx.Hash()] {
			lost++
		}
	}

	env.Rec.CheckOnce(SeveritySoft, lost == 0, "Pending txs from neither block stay in the pool",
		fmt.Sprintf("%d txs", len(bystanders)), fmt.Sprintf("%d lost", lost))

	// 6. The next block on B1 takes the reinjected txs and never T.
	b2, err := env.BuildOn(ctx, &b1.Block, cldriver.BuildOptions{})
	if err != nil {
		return fmt.Errorf("building B2: %w", err)
	}

	env.Rec.CheckOnce(SeverityHard, !b2.Contains(t.Hash()), "The invalidated tx is not in the next payload",
		"not included", fmt.Sprintf("included: %v", b2.Contains(t.Hash())))

	missing := 0

	for _, h := range reinjected {
		if !b2.Contains(h) {
			missing++
		}
	}

	env.Rec.CheckOnce(SeveritySoft, missing == 0, "The next payload includes the reinjected txs",
		fmt.Sprintf("%d txs", len(reinjected)), fmt.Sprintf("%d missing", missing))

	_, err = env.Import(ctx, b2)
	env.Rec.CheckOnce(SeverityHard, err == nil, "The next block on B1 is valid", "VALID", ErrText(err))

	if err != nil {
		return err
	}

	// The victim nonce was used once on the canonical chain (by T').
	victim.SetNonce(victimNonce + 1)

	_, err = env.Drain(ctx, 8)

	return err
}

// burn returns a call to the burner contract that uses all of gas. A builder
// subtracts the gas a tx uses, not its gas limit, so only a tx that uses its
// gas keeps another big tx out of the block.
func burn(fees txfactory.Fees, gas uint64) txfactory.Template {
	return txfactory.Template{
		Kind: txfactory.KindDynamicFee, Fees: fees, Gas: gas,
		To: &BurnerAddress, Value: big.NewInt(0), Data: []byte{},
	}
}

// big2 returns num/den gwei.
func big2(num, den int64) *big.Int {
	v := new(big.Int).Mul(big.NewInt(num), txfactory.Gwei)

	return v.Div(v, big.NewInt(den))
}
