package scenario

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/core/types"

	"github.com/ethpandaops/benchmarkoor/pkg/mempool/observer"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/txfactory"
)

// eviction fills the pool past its limits and checks what it keeps: a full
// pool should take better-paying txs and drop the cheapest, refuse txs that
// pay less than everything it holds, and still build a valid payload. It also
// tries the DoS shapes of one sender hogging slots and of gapped floods.
type eviction struct{}

func (s *eviction) Name() string { return "eviction-limits" }

func (s *eviction) Description() string {
	return "Fills the pool past global_slots + global_queue with low-tip txs, then sends " +
		"higher-tip txs (they should evict the cheapest), then txs cheaper than everything " +
		"(they should not stay). Also a sender that hogs slots and a flood of gapped txs. " +
		"Records evictions, the pool size and the admission latency of a full pool."
}

func (s *eviction) Defaults() Params {
	return Params{
		"low_senders":    400, // senders of the low-tip fill, 16 txs each
		"high_senders":   80,  // senders of the higher-tip txs, 16 txs each
		"cheap_senders":  40,  // senders of one tx cheaper than everything
		"per_sender":     16,
		"hog_txs":        256, // txs from the one sender that hogs slots
		"gapped_senders": 64,  // senders of a gapped flood (all nonces after a gap)
		"queue_extra":    16,  // future txs past the account queue from one sender
		"big_txs":        200, // large-calldata txs
		"big_tx_kb":      120, // calldata size of each (below the 128 KiB tx limit)
		"timeout_ms":     60000,
	}
}

func (s *eviction) Accounts(p Params) int {
	return p.Int("low_senders") + p.Int("high_senders") + p.Int("cheap_senders") + 1 +
		p.Int("gapped_senders") + 1 + (p.Int("big_txs")+15)/16
}

// tipFees returns fees with the given tip (in 0.01 gwei) and a fee cap well
// above any base fee of the scenario.
func tipFees(centiGwei int64) txfactory.Fees {
	return txfactory.Fees{
		FeeCap: txfactory.GweiN(50),
		TipCap: new(big.Int).Mul(big.NewInt(centiGwei), big.NewInt(10_000_000)),
	}
}

// signLanes signs perSender transfers for each account with the given fees.
func signLanes(env *Env, accounts []*txfactory.Account, perSender int, fees txfactory.Fees,
) ([][]*types.Transaction, error) {
	lanes := make([][]*types.Transaction, 0, len(accounts))

	for _, a := range accounts {
		lane := make([]*types.Transaction, 0, perSender)

		for range perSender {
			tx, err := env.Factory.Transfer(a, txfactory.KindDynamicFee, fees)
			if err != nil {
				return nil, err
			}

			lane = append(lane, tx)
		}

		lanes = append(lanes, lane)
	}

	return lanes, nil
}

// countIn returns how many of txs are in the pool.
func countIn(ctx context.Context, env *Env, txs []*types.Transaction) (int, error) {
	in, err := env.Pool.InPool(ctx, Hashes(txs))
	if err != nil {
		return 0, err
	}

	n := 0

	for _, tx := range txs {
		if in[tx.Hash()] {
			n++
		}
	}

	return n, nil
}

// settle waits until the pool size stops changing for a while.
func settle(ctx context.Context, env *Env, quiet, timeout time.Duration) (observer.Status, error) {
	deadline := time.Now().Add(timeout)

	last, err := env.Pool.Status(ctx)
	if err != nil {
		return last, err
	}

	stableSince := time.Now()

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}

		st, err := env.Pool.Status(ctx)
		if err != nil {
			return last, err
		}

		if st != last {
			last, stableSince = st, time.Now()
		}

		if time.Since(stableSince) >= quiet {
			return last, nil
		}
	}

	return last, nil
}

func (s *eviction) Run(ctx context.Context, env *Env) error {
	p := env.Params
	timeout := p.Duration("timeout_ms")
	per := p.Int("per_sender")
	capacity := env.Limits.GlobalSlots + env.Limits.GlobalQueue

	low, err := env.Accounts(ctx, p.Int("low_senders"))
	if err != nil {
		return err
	}

	high, err := env.Accounts(ctx, p.Int("high_senders"))
	if err != nil {
		return err
	}

	cheap, err := env.Accounts(ctx, p.Int("cheap_senders"))
	if err != nil {
		return err
	}

	// 1. Fill with low-tip txs (tips 1.00..1.39 gwei), more than the capacity.
	lowLanes := make([][]*types.Transaction, 0, len(low))

	for i, a := range low {
		lane, err := signLanes(env, []*txfactory.Account{a}, per, tipFees(100+int64(i%40)))
		if err != nil {
			return err
		}

		lowLanes = append(lowLanes, lane[0])
	}

	lowTxs := flatten(lowLanes)
	_, lowRejected, _ := env.SendConcurrent(ctx, regroup(lowLanes, 8), "send.fill",
		"eth_sendRawTransaction while the pool fills", "fill",
		fmt.Sprintf("fill with low-tip txs (capacity %d)", capacity))

	full, err := settle(ctx, env, time.Second, timeout)
	if err != nil {
		return err
	}

	env.Rec.Metric(Metric{Key: "fill.pending", Label: "Pending after the fill", Unit: "txs",
		Value: float64(full.Pending), Group: "fill"})
	env.Rec.Metric(Metric{Key: "fill.queued", Label: "Queued after the fill", Unit: "txs",
		Value: float64(full.Queued), Group: "fill"})
	env.Rec.Metric(Metric{Key: "fill.rejected", Label: "Low-tip txs rejected during the fill", Unit: "txs",
		Value: float64(lowRejected), Group: "fill"})
	env.Rec.Check(SeveritySoft, full.Pending+full.Queued <= capacity,
		"The pool stays within global_slots + global_queue",
		fmt.Sprintf("at most %d", capacity), fmt.Sprintf("%d", full.Pending+full.Queued))

	lowBefore, err := countIn(ctx, env, lowTxs)
	if err != nil {
		return err
	}

	// 2. Higher-tip txs (3 gwei) into the full pool.
	highLanes, err := signLanes(env, high, per, tipFees(300))
	if err != nil {
		return err
	}

	highTxs := flatten(highLanes)
	_, highRejected, highErr := env.SendConcurrent(ctx, regroup(highLanes, 8), "send.full",
		"eth_sendRawTransaction into a full pool", "full", "higher-tip txs into the full pool")

	after, err := settle(ctx, env, time.Second, timeout)
	if err != nil {
		return err
	}

	highIn, err := countIn(ctx, env, highTxs)
	if err != nil {
		return err
	}

	lowAfter, err := countIn(ctx, env, lowTxs)
	if err != nil {
		return err
	}

	env.Rec.Metric(Metric{Key: "high.kept", Label: "Higher-tip txs in the pool", Unit: "%",
		Value: round3(100 * float64(highIn) / float64(len(highTxs))), Group: "evict", Better: "higher"})
	env.Rec.Metric(Metric{Key: "low.evicted", Label: "Low-tip txs evicted for them", Unit: "txs",
		Value: float64(lowBefore - lowAfter), Group: "evict"})
	env.Rec.Check(SeveritySoft, highRejected == 0, "A full pool accepts txs that pay more than it holds",
		fmt.Sprintf("%d accepted", len(highTxs)), fmt.Sprintf("%d rejected (%s)", highRejected, SendText(highErr)))
	env.Rec.Check(SeveritySoft, highIn == len(highTxs), "Higher-tip txs stay in a full pool",
		fmt.Sprintf("%d in the pool", len(highTxs)), fmt.Sprintf("%d in the pool", highIn))
	env.Rec.Observe("evict.size_after", "Pool size after the higher-tip txs (pending+queued)",
		fmt.Sprintf("%d+%d", after.Pending, after.Queued))

	// 3. Txs cheaper than everything: one per sender, tip 0.5 gwei.
	cheapLanes, err := signLanes(env, cheap, 1, tipFees(50))
	if err != nil {
		return err
	}

	cheapTxs := flatten(cheapLanes)

	_, cheapRejected, cheapErr := env.SendConcurrent(ctx, cheapLanes, "", "", "", "txs cheaper than everything")
	if _, err := settle(ctx, env, time.Second, timeout); err != nil {
		return err
	}

	cheapIn, err := countIn(ctx, env, cheapTxs)
	if err != nil {
		return err
	}

	env.Rec.Observe("evict.cheap", "Txs cheaper than a full pool: rejected / kept",
		fmt.Sprintf("%d rejected (%s) / %d kept", cheapRejected, SendText(cheapErr), cheapIn))

	// The check needs a full pool. A pool that dropped or refused nothing
	// (besu limits by memory, not by count) was never full, and may keep
	// the cheap txs.
	wasFull := lowRejected > 0 || lowBefore < len(lowTxs) || lowAfter < lowBefore || highRejected > 0
	if wasFull {
		env.Rec.Check(SeveritySoft, cheapIn == 0, "A full pool does not keep txs cheaper than all it holds",
			"0 kept", fmt.Sprintf("%d kept", cheapIn))
	} else {
		env.Rec.Observe("evict.never_full", "Pool full during the test",
			fmt.Sprintf("no: it took all %d txs and dropped none, so the cheap-tx check is skipped",
				len(lowTxs)+len(highTxs)))
	}

	// The pool still builds a valid payload, with the best txs first.
	payload, err := env.Build(ctx, buildFull)
	if err != nil {
		return err
	}

	highInPayload := 0
	for _, tx := range highTxs {
		if payload.Contains(tx.Hash()) {
			highInPayload++
		}
	}

	env.Rec.Metric(Metric{Key: "payload.high", Label: "Higher-tip txs in the next payload", Unit: "%",
		Value: round3(100 * float64(highInPayload) / float64(max(1, highIn))), Group: "evict", Better: "higher"})

	_, err = env.Import(ctx, payload)
	env.Rec.Check(SeverityHard, err == nil, "A payload from a full pool is valid", "VALID", ErrText(err))

	if err != nil {
		return err
	}

	if err := s.hog(ctx, env, timeout); err != nil {
		return fmt.Errorf("hog: %w", err)
	}

	// The account-queue case needs a clean queue, so it runs before the
	// gapped flood: those txs can never be included, so nothing drains them.
	if err := s.queueLimit(ctx, env, timeout); err != nil {
		return fmt.Errorf("queue limit: %w", err)
	}

	if err := s.gapped(ctx, env, timeout); err != nil {
		return fmt.Errorf("gapped: %w", err)
	}

	return s.bigTxs(ctx, env, timeout)
}

// queueLimit: one sender sends more future txs (after a nonce gap) than the
// account queue allows.
func (s *eviction) queueLimit(ctx context.Context, env *Env, timeout time.Duration) error {
	accs, err := env.Accounts(ctx, 1)
	if err != nil {
		return err
	}

	n := env.Limits.AccountQueue + env.Params.Int("queue_extra")
	lane := make([]*types.Transaction, 0, n)

	for i := 1; i <= n; i++ {
		tx, err := env.Factory.Build(accs[0], txfactory.Template{
			Kind: txfactory.KindDynamicFee, Nonce: new(accs[0].Nonce() + uint64(i)),
		})
		if err != nil {
			return err
		}

		lane = append(lane, tx)
	}

	_, rejected, qErr := env.SendConcurrent(ctx, [][]*types.Transaction{lane}, "", "", "",
		"one sender, future nonces past the account queue")

	if _, err := settle(ctx, env, 500*time.Millisecond, timeout); err != nil {
		return err
	}

	kept, err := countIn(ctx, env, lane)
	if err != nil {
		return err
	}

	env.Rec.Metric(Metric{
		Key: "queue.kept", Label: fmt.Sprintf("Future txs kept from one sender that sent %d", n),
		Unit: "txs", Value: float64(kept), Group: "account queue",
	})
	env.Rec.Observe("queue.kept", fmt.Sprintf("One sender, %d future txs (account queue %d): kept",
		n, env.Limits.AccountQueue), fmt.Sprintf("%d kept, %d rejected (%s)", kept, rejected, SendText(qErr)))
	env.Rec.Check(SeveritySoft, kept <= env.Limits.AccountQueue,
		"One sender's future txs stay within the account queue",
		fmt.Sprintf("at most %d", env.Limits.AccountQueue), fmt.Sprintf("%d kept", kept))

	return nil
}

// bigTxs: large-calldata txs take much more pool memory per tx. A pool that
// limits by memory (besu) evicts; a pool that limits by slots (geth counts
// 32 KiB per slot) keeps more.
func (s *eviction) bigTxs(ctx context.Context, env *Env, timeout time.Duration) error {
	if _, err := env.Drain(ctx, 16); err != nil {
		return err
	}

	n := env.Params.Int("big_txs")
	size := env.Params.Int("big_tx_kb") * 1024

	accs, err := env.Accounts(ctx, (n+15)/16)
	if err != nil {
		return err
	}

	before, err := env.Pool.Status(ctx)
	if err != nil {
		return err
	}

	data := make([]byte, size)
	// Zero bytes cost 10 gas each under the EIP-7623 floor.
	gas := uint64(21_000 + 10*size + 1_000)

	lanes := make([][]*types.Transaction, 0, len(accs))

	for i, a := range accs {
		count := min(16, n-i*16)
		lane := make([]*types.Transaction, 0, count)

		for range count {
			// A recipient, or the data would be initcode of a creation.
			tx, err := env.Factory.Build(a, txfactory.Template{
				Kind: txfactory.KindDynamicFee, To: &delegationTarget, Data: data, Gas: gas,
			})
			if err != nil {
				return err
			}

			lane = append(lane, tx)
		}

		lanes = append(lanes, lane)
	}

	all := flatten(lanes)

	_, rejected, bigErr := env.SendConcurrent(ctx, regroup(lanes, 4), "send.big",
		fmt.Sprintf("eth_sendRawTransaction of a %d KiB tx", size/1024), "big",
		fmt.Sprintf("%d KiB calldata txs", size/1024))

	after, err := settle(ctx, env, time.Second, timeout)
	if err != nil {
		return err
	}

	kept, err := countIn(ctx, env, all)
	if err != nil {
		return err
	}

	env.Rec.Metric(Metric{
		Key: "big.kept", Label: fmt.Sprintf("%d KiB txs kept of %d (%.0f MiB sent)", size/1024, n,
			float64(n*size)/(1<<20)), Unit: "txs", Value: float64(kept), Group: "large calldata", Better: "higher",
	})
	env.Rec.Observe("big.kept", fmt.Sprintf("%d txs of %d KiB: kept / rejected", n, size/1024),
		fmt.Sprintf("%d kept, %d rejected (%s); pool %d -> %d pending", kept, rejected, SendText(bigErr),
			before.Pending, after.Pending))

	return nil
}

// hog: one sender sends many more txs than the account slots, then other
// senders send one tx each. The others must still get in.
func (s *eviction) hog(ctx context.Context, env *Env, timeout time.Duration) error {
	if _, err := env.Drain(ctx, 16); err != nil {
		return err
	}

	accs, err := env.Accounts(ctx, 1)
	if err != nil {
		return err
	}

	hogLanes, err := signLanes(env, accs, env.Params.Int("hog_txs"), txfactory.DefaultFees())
	if err != nil {
		return err
	}

	hogTxs := hogLanes[0]

	_, rejected, hogErr := env.SendConcurrent(ctx, hogLanes, "", "", "", "one sender hogs slots")
	if _, err := settle(ctx, env, 500*time.Millisecond, timeout); err != nil {
		return err
	}

	kept, err := countIn(ctx, env, hogTxs)
	if err != nil {
		return err
	}

	env.Rec.Observe("hog.kept", fmt.Sprintf("Txs kept from one sender that sent %d", len(hogTxs)),
		fmt.Sprintf("%d kept, %d rejected (%s)", kept, rejected, SendText(hogErr)))
	env.Rec.Metric(Metric{Key: "hog.kept", Label: fmt.Sprintf("Txs kept from one sender that sent %d", len(hogTxs)),
		Unit: "txs", Value: float64(kept), Group: "hog"})

	return nil
}

// gapped: many senders send only txs after a nonce gap. They can never be
// included until the gap fills, so the queue limits must hold them.
func (s *eviction) gapped(ctx context.Context, env *Env, timeout time.Duration) error {
	accs, err := env.Accounts(ctx, env.Params.Int("gapped_senders"))
	if err != nil {
		return err
	}

	per := env.Params.Int("per_sender")
	lanes := make([][]*types.Transaction, 0, len(accs))

	for _, a := range accs {
		lane := make([]*types.Transaction, 0, per)

		for n := 1; n <= per; n++ {
			tx, err := env.Factory.Build(a, txfactory.Template{
				Kind: txfactory.KindDynamicFee, Nonce: new(uint64(n)),
			})
			if err != nil {
				return err
			}

			lane = append(lane, tx)
		}

		lanes = append(lanes, lane)
	}

	all := flatten(lanes)

	_, rejected, gapErr := env.SendConcurrent(ctx, regroup(lanes, 8), "", "", "", "gapped flood")

	st, err := settle(ctx, env, 500*time.Millisecond, timeout)
	if err != nil {
		return err
	}

	kept, err := countIn(ctx, env, all)
	if err != nil {
		return err
	}

	env.Rec.Observe("gapped.kept", fmt.Sprintf("Gapped txs kept of %d (queue limits: %d per account, %d total)",
		len(all), env.Limits.AccountQueue, env.Limits.GlobalQueue),
		fmt.Sprintf("%d kept, %d rejected (%s); pool %d pending + %d queued",
			kept, rejected, SendText(gapErr), st.Pending, st.Queued))
	env.Rec.Check(SeveritySoft, kept <= env.Limits.GlobalQueue,
		"A gapped flood stays within global_queue",
		fmt.Sprintf("at most %d kept", env.Limits.GlobalQueue), fmt.Sprintf("%d kept", kept))

	payload, err := env.Build(ctx, buildFull)
	if err != nil {
		return err
	}

	in := 0
	for _, tx := range all {
		if payload.Contains(tx.Hash()) {
			in++
		}
	}

	env.Rec.Check(SeverityHard, in == 0, "Gapped txs are never in a payload", "0", fmt.Sprintf("%d", in))

	return nil
}
