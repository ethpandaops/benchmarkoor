package scenario

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/ethpandaops/benchmarkoor/pkg/mempool/cldriver"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/txfactory"
)

// sustainedLoad sends txs at a steady rate for a while, and builds a block
// every slot as a CL would. It measures how the pool and the builder behave
// over time: admission latency, inclusion delay, pool size and memory, and
// the Engine API latencies under load.
type sustainedLoad struct{}

func (s *sustainedLoad) Name() string { return "sustained-load" }

func (s *sustainedLoad) Description() string {
	return "Sends txs at a steady rate (mixed legacy and dynamic-fee, mixed tips) for a fixed " +
		"time, with a block every slot built from the pool. Measures admission latency, the " +
		"delay from send to inclusion, pool size and memory over time, and the Engine API " +
		"latencies under load. Every sent tx must end up in a block."
}

func (s *sustainedLoad) Defaults() Params {
	return Params{
		"duration_ms": 60000,
		"rate":        200,  // txs per second
		"senders":     100,  // the load rotates over these senders
		"slot_ms":     2000, // one block per slot (a short slot keeps the run short)
		"build_ms":    1000, // from forkchoiceUpdated to getPayload in a slot
		"timeout_ms":  60000,
	}
}

func (s *sustainedLoad) Accounts(p Params) int { return p.Int("senders") }

// loadTracker records the send time of each accepted tx.
type loadTracker struct {
	mu       sync.Mutex
	sentAt   map[common.Hash]time.Time
	accepted int
	rejected int
	firstErr error
}

func (t *loadTracker) add(h common.Hash, at time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.sentAt[h] = at
	t.accepted++
}

func (t *loadTracker) reject(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.rejected++
	if t.firstErr == nil {
		t.firstErr = err
	}
}

// take removes an included tx and returns its send time.
func (t *loadTracker) take(h common.Hash) (time.Time, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	at, ok := t.sentAt[h]
	if ok {
		delete(t.sentAt, h)
	}

	return at, ok
}

func (t *loadTracker) left() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	return len(t.sentAt)
}

func (s *sustainedLoad) Run(ctx context.Context, env *Env) error {
	p := env.Params
	slot := p.Duration("slot_ms")
	build := p.Duration("build_ms")

	accs, err := env.Accounts(ctx, p.Int("senders"))
	if err != nil {
		return err
	}

	tracker := &loadTracker{sentAt: make(map[common.Hash]time.Time, 4096)}

	loadCtx, stop := context.WithTimeout(ctx, p.Duration("duration_ms"))
	defer stop()

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()

		s.send(loadCtx, env, accs, tracker)
	}()

	start := time.Now()
	blocks, txsInBlocks := 0, 0

	// Build blocks while the load runs, then until the pool is empty.
	for {
		loadDone := loadCtx.Err() != nil
		if loadDone && tracker.left() == 0 {
			break
		}

		if loadDone && blocks > 0 && time.Since(start) > p.Duration("duration_ms")+p.Duration("timeout_ms") {
			break
		}

		n, err := s.block(ctx, env, slot, build, tracker)
		if err != nil {
			stop()
			wg.Wait()

			env.Rec.Check(SeverityHard, false, "Every block built under load is valid", "VALID", ErrText(err))

			return err
		}

		blocks++
		txsInBlocks += n
	}

	wg.Wait()

	elapsed := p.Duration("duration_ms").Seconds()
	env.Rec.Metric(Metric{Key: "rate", Label: "Accepted send rate", Unit: "tx/s",
		Value: round3(float64(tracker.accepted) / elapsed), Group: "load", Better: "higher"})
	env.Rec.Metric(Metric{Key: "rejected", Label: "Rejected sends", Unit: "txs",
		Value: float64(tracker.rejected), Group: "load", Better: "lower"})
	env.Rec.Metric(Metric{Key: "blocks", Label: "Blocks built", Unit: "blocks",
		Value: float64(blocks), Group: "load"})
	env.Rec.Metric(Metric{Key: "txs_per_block", Label: "Txs per block (mean)", Unit: "txs",
		Value: round3(float64(txsInBlocks) / float64(max(1, blocks))), Group: "load"})

	env.Rec.Check(SeveritySoft, tracker.rejected == 0, "Valid txs are accepted under sustained load",
		"0 rejected", fmt.Sprintf("%d rejected (%s)", tracker.rejected, SendText(tracker.firstErr)))
	env.Rec.Check(SeveritySoft, tracker.left() == 0, "Every accepted tx ends up in a block",
		"0 left", fmt.Sprintf("%d left", tracker.left()))

	return nil
}

// send sends txs at the configured rate until ctx ends. It rotates over the
// senders, so each sender has only a few txs in flight.
func (s *sustainedLoad) send(ctx context.Context, env *Env, accs []*txfactory.Account, t *loadTracker) {
	interval := time.Second / time.Duration(max(1, env.Params.Int("rate")))
	ticker := time.NewTicker(interval)

	defer ticker.Stop()

	for i := 0; ; i++ {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		a := accs[i%len(accs)]
		kind := txfactory.KindDynamicFee

		if i%5 == 0 {
			kind = txfactory.KindLegacy
		}

		// Tips from 1.00 to 1.99 gwei.
		fees := txfactory.Fees{
			FeeCap: txfactory.GweiN(50),
			TipCap: new(big.Int).Mul(big.NewInt(int64(100+i%100)), big.NewInt(10_000_000)),
		}
		if kind == txfactory.KindLegacy {
			fees.FeeCap = new(big.Int).Add(txfactory.GweiN(10), fees.TipCap)
		}

		tx, err := env.Factory.Transfer(a, kind, fees)
		if err != nil {
			t.reject(err)

			continue
		}

		at := time.Now()

		took, err := env.Send(ctx, tx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}

			// Take the nonce back, so the sender has no gap.
			a.SetNonce(tx.Nonce())
			t.reject(err)

			continue
		}

		t.add(tx.Hash(), at)
		env.Rec.Latency("send", "eth_sendRawTransaction under sustained load", "load", took)
	}
}

// block runs one slot: start a build, wait, get the payload, import it.
func (s *sustainedLoad) block(ctx context.Context, env *Env, slot, build time.Duration, t *loadTracker,
) (int, error) {
	slotStart := time.Now()

	head, err := env.Head(ctx)
	if err != nil {
		return 0, err
	}

	attrs := env.Driver.Attributes(head, cldriver.BuildOptions{})

	resp, fcuTook, err := env.Driver.ForkchoiceUpdated(ctx, head.Hash, attrs)
	if err != nil {
		return 0, err
	}

	if resp.PayloadID == nil {
		return 0, fmt.Errorf("no payload id on block %d", head.Number)
	}

	env.Rec.Latency("fcu_build", "forkchoiceUpdated with attributes under load", "engine", fcuTook)

	if err := sleepCtx(ctx, time.Until(slotStart.Add(build))); err != nil {
		return 0, err
	}

	envelope, getTook, err := env.Driver.GetPayload(ctx, *resp.PayloadID, attrs.Timestamp)
	if err != nil {
		return 0, err
	}

	env.Rec.Latency("get_payload", "getPayload under load", "engine", getTook)

	payload, err := cldriver.NewPayloadFromEnvelope(envelope, *attrs.BeaconRoot)
	if err != nil {
		return 0, err
	}

	timing, err := env.Driver.Import(ctx, payload)
	if err != nil {
		return 0, fmt.Errorf("importing block %d: %w", payload.Block.Number, err)
	}

	importedAt := time.Now()

	env.Rec.Latency("new_payload", "newPayload under load", "engine", timing.NewPayload)
	env.Rec.Latency("fcu_head", "forkchoiceUpdated (head) under load", "engine", timing.Fcu)

	for _, h := range payload.TxHashes {
		if at, ok := t.take(h); ok {
			env.Rec.Latency("inclusion", "Delay from send until the tx is in the new head", "load",
				importedAt.Sub(at))
		}
	}

	return len(payload.Txs), sleepCtx(ctx, time.Until(slotStart.Add(slot)))
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
