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

// balanceChanges tests how the pool re-checks txs when a balance changes in
// a block without a reorg: up (a withdrawal funds an account) and down (an
// account delegated to the sweeper is emptied by someone else's tx).
type balanceChanges struct{}

func (s *balanceChanges) Name() string { return "balance-changes" }

func (s *balanceChanges) Description() string {
	return "A withdrawal funds an empty account: its txs, rejected before, are accepted and " +
		"included after the block. An account delegated to a sweeper contract is emptied by " +
		"another sender's call: its pending txs can no longer be paid, must never be included, " +
		"and should leave the pool."
}

func (s *balanceChanges) Defaults() Params {
	return Params{
		"pending_txs": 4, // txs the drained account has pending
		"timeout_ms":  20000,
	}
}

func (s *balanceChanges) Accounts(Params) int { return 3 }

func (s *balanceChanges) Run(ctx context.Context, env *Env) error {
	accs, err := env.Accounts(ctx, 3)
	if err != nil {
		return err
	}

	if err := s.withdrawal(ctx, env, accs[0]); err != nil {
		return fmt.Errorf("withdrawal: %w", err)
	}

	if err := s.drain(ctx, env, accs[0], accs[1], accs[2]); err != nil {
		return fmt.Errorf("drain: %w", err)
	}

	_, err = env.Drain(ctx, 8)

	return err
}

// withdrawal: an account with no balance cannot send. A withdrawal in a block
// credits it; then its txs are accepted and included.
func (s *balanceChanges) withdrawal(ctx context.Context, env *Env, ref *txfactory.Account) error {
	poor := txfactory.NewAccount(txfactory.DeriveKey("withdrawal", ref.Index))
	fees := txfactory.DefaultFees()

	before, err := env.Factory.Build(poor, txfactory.Template{Kind: txfactory.KindDynamicFee, Fees: fees})
	if err != nil {
		return err
	}

	_, sendErr := env.Send(ctx, before)
	env.Rec.Event("send", "a tx from an account with no balance")
	env.Rec.Check(SeveritySoft, sendErr != nil, "A tx from an account with no balance is rejected",
		"rejected", SendText(sendErr))

	poor.SetNonce(0)

	if sendErr == nil {
		// Accepted anyway: it is in the pool and becomes payable below.
		poor.SetNonce(1)
	}

	// 1 ETH, in gwei.
	start := time.Now()

	p, err := env.ImportNext(ctx, cldriver.BuildOptions{
		Withdrawals: []*types.Withdrawal{{Index: 1, Validator: 1, Address: poor.Address, Amount: 1_000_000_000}},
	})
	if err != nil {
		return err
	}

	env.Rec.Event("withdrawal", "block %d credits 1 ETH to the empty account", p.Block.Number)

	bal, err := env.Balance(ctx, poor.Address)
	if err != nil {
		return err
	}

	oneEth := new(big.Int).Mul(big.NewInt(1_000_000_000), txfactory.Gwei)
	env.Rec.Check(SeverityHard, bal.Cmp(oneEth) == 0, "A withdrawal credits the account", "1 ETH",
		fmt.Sprintf("%s wei", bal))

	txs := make([]*types.Transaction, 0, 3)
	if sendErr == nil {
		txs = append(txs, before)
	}

	accepted := 0

	for len(txs) < 3 {
		tx, err := env.Factory.Build(poor, txfactory.Template{Kind: txfactory.KindDynamicFee, Fees: fees})
		if err != nil {
			return err
		}

		if _, err := env.Send(ctx, tx); err != nil {
			env.Rec.Check(SeveritySoft, false, "Txs are accepted once a withdrawal funds the account", "accepted",
				SendText(err))

			break
		}

		accepted++

		txs = append(txs, tx)
	}

	env.Rec.Event("send", "%d txs from the funded account", accepted)

	head, err := env.Head(ctx)
	if err != nil {
		return err
	}

	next, err := env.BuildWith(ctx, head, Hashes(txs), buildFull)
	env.Rec.Check(SeveritySoft, err == nil, "The next payload includes the newly payable txs",
		fmt.Sprintf("%d txs", len(txs)), ErrText(err))

	if err != nil {
		return nil
	}

	env.Rec.Metric(Metric{
		Key: "withdrawal.to_payload", Label: "Time from the withdrawal block until the txs are in a payload",
		Unit: "ms", Value: Ms(time.Since(start)), Group: "withdrawal", Better: "lower",
	})

	_, err = env.Import(ctx, next)

	return err
}

// drain: Z delegates to the sweeper and has pending txs (priced out, so they
// stay). Another sender calls Z, which sends Z's whole balance to the
// caller. Z's txs can no longer be paid.
func (s *balanceChanges) drain(ctx context.Context, env *Env, sponsor, z, drainer *txfactory.Account) error {
	if err := delegate(ctx, env, sponsor, z, SweeperAddress); err != nil {
		return err
	}

	head, err := env.Head(ctx)
	if err != nil {
		return err
	}

	n := env.Params.Int("pending_txs")
	pending := make([]*types.Transaction, 0, n)

	var firstErr error

	env.Rec.Event("send", "Z (delegated to the sweeper): %d priced-out txs", n)

	for range n {
		tx, err := env.Factory.Build(z, txfactory.Template{Kind: txfactory.KindDynamicFee, Fees: pricedOut(head)})
		if err != nil {
			return err
		}

		if _, err := env.Send(ctx, tx); err != nil {
			z.SetNonce(tx.Nonce())

			if firstErr == nil {
				firstErr = err
			}

			break
		}

		pending = append(pending, tx)
	}

	env.Rec.Observe("drain.pending", fmt.Sprintf("Pending txs accepted from the delegated account, of %d", n),
		fmt.Sprintf("%d (%s)", len(pending), SendText(firstErr)))

	if len(pending) == 0 {
		return nil
	}

	call, err := env.Factory.Build(drainer, txfactory.Template{
		Kind: txfactory.KindDynamicFee, To: &z.Address, Value: big.NewInt(0), Data: []byte{}, Gas: 100_000,
		Fees: txfactory.Fees{FeeCap: txfactory.GweiN(50), TipCap: txfactory.GweiN(5)},
	})
	if err != nil {
		return err
	}

	if err := env.MustSend(ctx, []*types.Transaction{call}, "a call to Z that sweeps its balance"); err != nil {
		return err
	}

	p, err := env.BuildWith(ctx, head, []common.Hash{call.Hash()}, buildFull)
	if err != nil {
		return err
	}

	start := time.Now()

	if _, err := env.Import(ctx, p); err != nil {
		return err
	}

	bal, err := env.Balance(ctx, z.Address)
	if err != nil {
		return err
	}

	env.Rec.Check(SeverityHard, bal.Sign() == 0, "The sweeper empties the delegated account", "0 wei",
		fmt.Sprintf("%s wei", bal))

	env.Forbid("sender emptied by the sweeper", Hashes(pending)...)

	gone, goneErr := env.WaitGone(ctx, start, Hashes(pending), 5*time.Second)
	env.Rec.Check(SeveritySoft, goneErr == nil, "The pool drops txs whose sender was emptied",
		fmt.Sprintf("%d dropped", len(pending)), ErrText(goneErr))

	if goneErr == nil {
		env.Rec.Metric(Metric{
			Key: "drain.dropped", Label: "Time until the pool drops the unpayable txs", Unit: "ms",
			Value: Ms(gone), Group: "drain", Better: "lower",
		})
	}

	// Lower the base fee until the txs would be valid by price: with no
	// balance they must still never be in a payload.
	included := 0

	for range 4 {
		blk, err := env.ImportNext(ctx, buildFull)
		if err != nil {
			return err
		}

		for _, tx := range pending {
			if blk.Contains(tx.Hash()) {
				included++
			}
		}
	}

	env.Rec.Check(SeverityHard, included == 0, "Txs of an emptied account are never in a payload",
		"0", fmt.Sprintf("%d", included))

	return nil
}
