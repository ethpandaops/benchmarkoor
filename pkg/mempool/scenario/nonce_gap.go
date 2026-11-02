package scenario

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/ethpandaops/benchmarkoor/pkg/mempool/cldriver"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/observer"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/txfactory"
)

// nonceGap checks queued versus pending: txs after a nonce gap are queued,
// and fill the gap promotes them. It also checks the balance check over the
// pending txs of one sender.
type nonceGap struct{}

func (s *nonceGap) Name() string { return "nonce-gap-promotion" }

func (s *nonceGap) Description() string {
	return "Txs after a nonce gap must be queued, never pending or in a payload. Filling the gap " +
		"promotes them (timed). Txs sent in reverse nonce order end up pending. A sender " +
		"whose balance pays for only some of its txs gets the rest rejected or not executable."
}

func (s *nonceGap) Defaults() Params {
	return Params{
		"gap_txs":    8, // queued txs behind the gap (at most the account queue)
		"rounds":     5, // gap/fill rounds, one sender each
		"affordable": 3, // txs the poor sender can pay for
		"timeout_ms": 10000,
	}
}

func (s *nonceGap) Accounts(p Params) int { return p.Int("rounds") + 2 }

func (s *nonceGap) Run(ctx context.Context, env *Env) error {
	p := env.Params
	timeout := p.Duration("timeout_ms")
	gap := p.Int("gap_txs")

	accounts, err := env.Accounts(ctx, p.Int("rounds")+2)
	if err != nil {
		return err
	}

	for round, a := range accounts[:p.Int("rounds")] {
		if err := s.gapRound(ctx, env, a, gap, round, timeout); err != nil {
			return fmt.Errorf("round %d: %w", round, err)
		}
	}

	if err := s.reverse(ctx, env, accounts[p.Int("rounds")], gap, timeout); err != nil {
		return err
	}

	if err := s.overdraft(ctx, env, accounts[p.Int("rounds")+1], timeout); err != nil {
		return err
	}

	_, err = env.Drain(ctx, 16)

	return err
}

// gapRound sends nonces 1..gap (queued), checks that none is pending or
// built, then sends nonce 0 and times the promotion of all of them.
func (s *nonceGap) gapRound(ctx context.Context, env *Env, a *txfactory.Account, gap, round int,
	timeout time.Duration,
) error {
	base := a.Nonce()
	gapped := make([]*types.Transaction, 0, gap)

	for i := 1; i <= gap; i++ {
		tx, err := env.Factory.Build(a, txfactory.Template{
			Kind: txfactory.KindDynamicFee, Nonce: new(base + uint64(i)),
		})
		if err != nil {
			return err
		}

		gapped = append(gapped, tx)
	}

	if err := env.MustSend(ctx, gapped); err != nil {
		return err
	}

	if _, err := env.WaitInPool(ctx, time.Now(), Hashes(gapped), timeout); err != nil {
		return fmt.Errorf("waiting for the gapped txs: %w", err)
	}

	pending, queued, err := env.ContentFrom(ctx, a.Address)

	switch {
	case errors.Is(err, observer.ErrUnsupported):
		if round == 0 {
			env.Rec.Observe("pending_view", "Pending/queued view",
				"not available: the client does not tell pending from queued over RPC")
		}
	case err != nil:
		return err
	default:
		env.Rec.CheckOnce(SeverityHard, len(pending) == 0, "Txs after a nonce gap are not pending",
			"0 pending", fmt.Sprintf("%d pending", len(pending)))
		env.Rec.CheckOnce(SeveritySoft, len(queued) == gap, "Txs after a nonce gap are queued",
			fmt.Sprintf("%d queued", gap), fmt.Sprintf("%d queued", len(queued)))
	}

	payload, err := env.Build(ctx, buildFull)
	if err != nil {
		return err
	}

	built := 0

	for _, tx := range gapped {
		if payload.Contains(tx.Hash()) {
			built++
		}
	}

	env.Rec.CheckOnce(SeverityHard, built == 0, "Gapped txs are not in a built payload", "0",
		fmt.Sprintf("%d", built))

	fill, err := env.Factory.Build(a, txfactory.Template{Kind: txfactory.KindDynamicFee, Nonce: new(base)})
	if err != nil {
		return err
	}

	start := time.Now()
	if _, err := env.Send(ctx, fill); err != nil {
		return fmt.Errorf("sending the gap fill: %w", err)
	}

	env.Rec.Event("send", "round %d: fill the gap at nonce %d", round, base)

	all := append([]*types.Transaction{fill}, gapped...)

	took, viaPayload, err := s.waitAllPending(ctx, env, a, base, Hashes(all), start, timeout)
	env.Rec.CheckOnce(SeverityHard, err == nil, "Filling the gap promotes every queued tx",
		fmt.Sprintf("%d pending", gap+1), ErrText(err))

	if err == nil && viaPayload {
		env.Rec.Latency("promotion_payload",
			fmt.Sprintf("Time until %d queued txs are in a built payload after the gap fill (no pending view)", gap),
			"promotion", took)
	} else if err == nil {
		env.Rec.Latency("promotion", fmt.Sprintf("Time until %d queued txs are pending after the gap fill", gap),
			"promotion", took)
	}

	a.SetNonce(base + uint64(gap) + 1)

	_, err = env.Drain(ctx, 8)

	return err
}

// waitAllPending waits until the txs (nonces base..base+len-1) are pending.
// A client without a pending view is timed until a built payload holds them
// all (viaPayload is then true).
func (s *nonceGap) waitAllPending(ctx context.Context, env *Env, a *txfactory.Account,
	base uint64, hashes []common.Hash, start time.Time, timeout time.Duration,
) (took time.Duration, viaPayload bool, err error) {
	if !env.Pool.Caps().ContentFrom {
		took, err = waitInPayload(ctx, env, hashes, start, timeout)

		return took, true, err
	}

	n := len(hashes)

	for {
		pending, _, err := env.ContentFrom(ctx, a.Address)
		if err != nil {
			return 0, false, err
		}

		ok := true

		for i := range n {
			if _, in := pending[base+uint64(i)]; !in {
				ok = false

				break
			}
		}

		if ok {
			return time.Since(start), false, nil
		}

		if time.Since(start) > timeout {
			return 0, false, fmt.Errorf("%d of %d pending after %s", len(pending), n, timeout)
		}

		select {
		case <-ctx.Done():
			return 0, false, ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
}

// waitInPayload builds on the head until a payload holds every hash. It does
// not import the payload.
func waitInPayload(ctx context.Context, env *Env, hashes []common.Hash, start time.Time,
	timeout time.Duration,
) (time.Duration, error) {
	head, err := env.Head(ctx)
	if err != nil {
		return 0, err
	}

	for {
		p, err := env.BuildOn(ctx, head, cldriver.BuildOptions{Delay: 50 * time.Millisecond})
		if err != nil {
			return 0, err
		}

		missing := 0

		for _, h := range hashes {
			if !p.Contains(h) {
				missing++
			}
		}

		if missing == 0 {
			return time.Since(start), nil
		}

		if time.Since(start) > timeout {
			return 0, fmt.Errorf("%d of %d txs not in a payload after %s", missing, len(hashes), timeout)
		}
	}
}

// reverse sends nonces in reverse order. All must end up pending.
func (s *nonceGap) reverse(ctx context.Context, env *Env, a *txfactory.Account, n int,
	timeout time.Duration,
) error {
	base := a.Nonce()
	txs := make([]*types.Transaction, 0, n)

	for i := n - 1; i >= 0; i-- {
		tx, err := env.Factory.Build(a, txfactory.Template{
			Kind: txfactory.KindDynamicFee, Nonce: new(base + uint64(i)),
		})
		if err != nil {
			return err
		}

		txs = append(txs, tx)
	}

	start := time.Now()
	if err := env.MustSend(ctx, txs); err != nil {
		return err
	}

	took, _, err := s.waitAllPending(ctx, env, a, base, Hashes(txs), start, timeout)
	env.Rec.Check(SeverityHard, err == nil, "Txs sent in reverse nonce order all become pending",
		fmt.Sprintf("%d pending", n), ErrText(err))

	if err == nil {
		env.Rec.Metric(Metric{
			Key: "reverse_order_pending", Label: fmt.Sprintf("Time until %d txs sent in reverse order are pending", n),
			Unit: "ms", Value: Ms(took), Group: "promotion", Better: "lower",
		})
	}

	a.SetNonce(base + uint64(n))

	_, err = env.Drain(ctx, 8)

	return err
}

// overdraft funds a fresh account with enough for "affordable" txs, then
// sends more. The pool checks the cost at the fee cap over all pending txs of
// the sender, so it should admit only "affordable" of them.
func (s *nonceGap) overdraft(ctx context.Context, env *Env, funder *txfactory.Account,
	timeout time.Duration,
) error {
	n := env.Params.Int("affordable")
	fees := txfactory.DefaultFees()

	poor := txfactory.NewAccount(txfactory.DeriveKey("overdraft", funder.Index))

	// Each tx costs 21000 gas at the fee cap, plus 1 wei of value.
	perTx := new(big.Int).Mul(big.NewInt(21_000), fees.FeeCap)
	perTx.Add(perTx, big.NewInt(1))

	fund, err := env.Factory.Build(funder, txfactory.Template{
		Kind: txfactory.KindDynamicFee, To: &poor.Address,
		Value: new(big.Int).Mul(perTx, big.NewInt(int64(n))),
	})
	if err != nil {
		return err
	}

	if err := env.MustSend(ctx, []*types.Transaction{fund}); err != nil {
		return err
	}

	if _, err := env.Drain(ctx, 4); err != nil {
		return err
	}

	accepted := 0
	sent := make([]*types.Transaction, 0, n+3)

	for range n + 3 {
		tx, err := env.Factory.Build(poor, txfactory.Template{Kind: txfactory.KindDynamicFee, Fees: fees})
		if err != nil {
			return err
		}

		_, sendErr := env.Send(ctx, tx)
		if sendErr == nil {
			accepted++
			sent = append(sent, tx)
		} else {
			env.Rec.Observe("overdraft.error", "Error for a tx the balance cannot pay", SendText(sendErr))
		}
	}

	env.Rec.Observe("overdraft.accepted", fmt.Sprintf("Txs accepted from a sender that can pay for %d of %d", n, n+3),
		fmt.Sprintf("%d", accepted))
	env.Rec.Check(SeveritySoft, accepted == n, "The pool admits only the txs the balance can pay for",
		fmt.Sprintf("%d accepted", n), fmt.Sprintf("%d accepted", accepted))

	// No payload check here: a payload may validly hold more of these txs,
	// since execution charges the effective gas price, not the fee cap.
	_, err = env.WaitInPool(ctx, time.Now(), Hashes(sent), timeout)

	return err
}
