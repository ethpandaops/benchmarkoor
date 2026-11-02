package scenario

import (
	"context"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/ethpandaops/benchmarkoor/pkg/mempool/cldriver"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/txfactory"
)

// admission measures eth_sendRawTransaction per tx type: the latency of one
// call, the throughput of concurrent calls, the time until a tx is pending,
// and the time until a tx is in a built payload.
type admission struct{}

func (s *admission) Name() string { return "admission" }

func (s *admission) Description() string {
	return "Latency and throughput of eth_sendRawTransaction per tx type " +
		"(legacy, access-list, dynamic-fee, blob with cell proofs, 6-blob, set-code), " +
		"the time until a tx is pending, and the time until it is in a built payload."
}

func (s *admission) Defaults() Params {
	return Params{
		"senders":         16, // senders per tx type and phase
		"txs_per_sender":  16, // nonces per sender (at most the account slots)
		"blob_txs":        32, // single-blob txs per phase
		"blob6_txs":       8,  // 6-blob txs per phase
		"concurrency":     8,  // parallel senders in the throughput phase
		"pending_samples": 32, // txs timed until pending, per type
		"payload_txs":     64, // txs timed until they are in a built payload
		"timeout_ms":      30000,
	}
}

// admissionKind is one measured tx type.
type admissionKind struct {
	key   string
	label string
	kind  txfactory.Kind
	blobs int
}

var admissionKinds = []admissionKind{
	{"legacy", "Legacy", txfactory.KindLegacy, 0},
	{"access-list", "Access list (2930)", txfactory.KindAccessList, 0},
	{"dynamic-fee", "Dynamic fee (1559)", txfactory.KindDynamicFee, 0},
	{"set-code", "Set code (7702)", txfactory.KindSetCode, 0},
	{"blob", "Blob, 1 blob, cell proofs (4844/7594)", txfactory.KindBlob, 1},
	{"blob6", "Blob, 6 blobs, cell proofs (4844/7594)", txfactory.KindBlob, 6},
}

func (s *admission) Accounts(p Params) int {
	perKind := 2*p.Int("senders") + p.Int("pending_samples")

	return len(admissionKinds)*perKind + p.Int("payload_txs")
}

// shape returns the senders and the txs per sender of one phase for a kind.
func (s *admission) shape(p Params, k admissionKind) (senders, perSender int) {
	senders, perSender = p.Int("senders"), p.Int("txs_per_sender")

	switch k.key {
	case "blob":
		perSender = max(1, p.Int("blob_txs")/senders)
	case "blob6":
		perSender = max(1, p.Int("blob6_txs")/senders)
		if p.Int("blob6_txs") < senders {
			senders = p.Int("blob6_txs")
		}
	}

	return senders, perSender
}

func (s *admission) Run(ctx context.Context, env *Env) error {
	p := env.Params
	timeout := p.Duration("timeout_ms")

	for _, k := range admissionKinds {
		if err := s.runKind(ctx, env, k, timeout); err != nil {
			return fmt.Errorf("%s: %w", k.key, err)
		}
	}

	return s.timeToPayload(ctx, env, timeout)
}

// lanes signs perSender txs for each sender: lane i holds the txs of sender i
// in nonce order. Signing (and blob proofs) is done here, not timed.
func (s *admission) lanes(env *Env, accounts []*txfactory.Account, perSender int,
	k admissionKind,
) ([][]*types.Transaction, error) {
	lanes := make([][]*types.Transaction, 0, len(accounts))

	for _, a := range accounts {
		lane := make([]*types.Transaction, 0, perSender)

		for range perSender {
			tx, err := env.Factory.Build(a, txfactory.Template{Kind: k.kind, Blobs: k.blobs})
			if err != nil {
				return nil, err
			}

			lane = append(lane, tx)
		}

		lanes = append(lanes, lane)
	}

	return lanes, nil
}

func flatten(lanes [][]*types.Transaction) []*types.Transaction {
	n := 0
	for _, l := range lanes {
		n += len(l)
	}

	out := make([]*types.Transaction, 0, n)
	for _, l := range lanes {
		out = append(out, l...)
	}

	return out
}

// interleave returns the txs round-robin over lanes, so each sender's nonces
// stay in order but senders alternate.
func interleave(lanes [][]*types.Transaction) []*types.Transaction {
	out := make([]*types.Transaction, 0, len(flatten(lanes)))

	for i := 0; ; i++ {
		added := false

		for _, l := range lanes {
			if i < len(l) {
				out = append(out, l[i])
				added = true
			}
		}

		if !added {
			return out
		}
	}
}

func (s *admission) runKind(ctx context.Context, env *Env, k admissionKind, timeout time.Duration) error {
	p := env.Params
	senders, perSender := s.shape(p, k)

	// Phase 1: one call at a time.
	accounts, err := env.Accounts(ctx, p.Int("senders"))
	if err != nil {
		return err
	}

	lanes, err := s.lanes(env, accounts[:senders], perSender, k)
	if err != nil {
		return err
	}

	txs := interleave(lanes)
	env.Rec.Event("send", "%s: %d txs, one at a time", k.key, len(txs))

	start := time.Now()
	rejected := 0

	for _, tx := range txs {
		took, err := env.Send(ctx, tx)
		if err != nil {
			rejected++

			env.Log.WithError(err).WithField("kind", k.key).Debug("Tx rejected")

			continue
		}

		env.Rec.Latency("send."+k.key, "eth_sendRawTransaction: "+k.label, k.key, took)
	}

	serial := time.Since(start)

	env.Rec.Check(SeverityHard, rejected == 0, fmt.Sprintf("%s: valid txs are accepted", k.label),
		fmt.Sprintf("%d accepted", len(txs)), fmt.Sprintf("%d rejected", rejected))

	allIn, err := env.WaitInPool(ctx, start, Hashes(txs), timeout)
	env.Rec.Check(SeverityHard, err == nil, fmt.Sprintf("%s: accepted txs are in the pool", k.label),
		"all in the pool", ErrText(err))
	env.Rec.Metric(Metric{
		Key: "serial_tps." + k.key, Label: "Serial throughput: " + k.label, Unit: "tx/s",
		Value: round3(float64(len(txs)-rejected) / serial.Seconds()), Group: k.key, Better: "higher",
	})
	env.Rec.Metric(Metric{
		Key: "all_in_pool." + k.key, Label: "Time until all serial txs are in the pool: " + k.label,
		Unit: "ms", Value: Ms(allIn), Group: k.key, Better: "lower",
	})

	if _, err := env.Drain(ctx, 64); err != nil {
		return fmt.Errorf("draining after the serial phase: %w", err)
	}

	// Phase 2: concurrent senders.
	accounts, err = env.Accounts(ctx, p.Int("senders"))
	if err != nil {
		return err
	}

	lanes, err = s.lanes(env, accounts[:senders], perSender, k)
	if err != nil {
		return err
	}

	workers := regroup(lanes, p.Int("concurrency"))
	all := flatten(lanes)

	env.Rec.Event("send", "%s: %d txs, %d concurrent senders", k.key, len(all), len(workers))

	wall, rejected, firstErr := env.SendConcurrent(ctx, workers, "send_concurrent."+k.key,
		"eth_sendRawTransaction under load: "+k.label, k.key)
	env.Rec.Check(SeverityHard, rejected == 0,
		fmt.Sprintf("%s: valid txs are accepted under load", k.label),
		fmt.Sprintf("%d accepted", len(all)), fmt.Sprintf("%d rejected (%s)", rejected, ErrText(firstErr)))
	env.Rec.Metric(Metric{
		Key: "concurrent_tps." + k.key, Label: "Concurrent throughput: " + k.label, Unit: "tx/s",
		Value: round3(float64(len(all)-rejected) / wall.Seconds()), Group: k.key, Better: "higher",
	})

	if _, err := env.WaitInPool(ctx, time.Now(), Hashes(all), timeout); err != nil {
		return fmt.Errorf("waiting for the concurrent txs: %w", err)
	}

	if _, err := env.Drain(ctx, 64); err != nil {
		return fmt.Errorf("draining after the concurrent phase: %w", err)
	}

	// Phase 3: the time until one tx is pending, on an empty pool.
	return s.timeToPending(ctx, env, k, timeout)
}

// regroup spreads the lanes over n workers. A worker sends its lanes one
// after the other, so each sender's nonces stay in order.
func regroup(lanes [][]*types.Transaction, n int) [][]*types.Transaction {
	n = max(1, min(n, len(lanes)))
	out := make([][]*types.Transaction, n)

	for i, l := range lanes {
		out[i%n] = append(out[i%n], l...)
	}

	return out
}

func (s *admission) timeToPending(ctx context.Context, env *Env, k admissionKind,
	timeout time.Duration,
) error {
	n := env.Params.Int("pending_samples")
	if k.kind == txfactory.KindBlob {
		n = min(n, 2*env.Chain.MaxBlobsPerBlock/max(1, k.blobs))
	}

	accounts, err := env.Accounts(ctx, env.Params.Int("pending_samples"))
	if err != nil {
		return err
	}

	sent := make([]*types.Transaction, 0, n)

	for _, a := range accounts[:n] {
		tx, err := env.Factory.Build(a, txfactory.Template{Kind: k.kind, Blobs: k.blobs})
		if err != nil {
			return err
		}

		start := time.Now()
		if _, err := env.Send(ctx, tx); err != nil {
			env.Rec.Check(SeverityHard, false, fmt.Sprintf("%s: a single tx is accepted", k.label),
				"accepted", SendText(err))

			continue
		}

		// Blob txs are never queued (geth leaves them out of txpool_content),
		// so "known and not mined" means pending for them. A client without a
		// pending view (besu) is timed until the tx is in the pool.
		var took time.Duration

		key, label := "pending."+k.key, "Time until pending: "+k.label

		switch {
		case k.kind == txfactory.KindBlob:
			took, err = env.WaitInPool(ctx, start, []common.Hash{tx.Hash()}, timeout)
		case !env.Pool.Caps().ContentFrom:
			key, label = "in_pool."+k.key, "Time until in the pool (no pending view): "+k.label
			took, err = env.WaitInPool(ctx, start, []common.Hash{tx.Hash()}, timeout)
		default:
			took, err = s.waitPending(ctx, env, start, a, tx, timeout)
		}

		if err != nil {
			env.Rec.Check(SeverityHard, false, fmt.Sprintf("%s: a single tx becomes pending", k.label),
				"pending", ErrText(err))

			continue
		}

		env.Rec.Latency(key, label, k.key, took)
		sent = append(sent, tx)
	}

	env.Rec.Event("pending", "%s: %d txs timed until pending", k.key, len(sent))

	if _, err := env.Drain(ctx, 64); err != nil {
		return fmt.Errorf("draining after the pending phase: %w", err)
	}

	return nil
}

// waitPending waits until the tx is in the pending map of its sender.
func (s *admission) waitPending(ctx context.Context, env *Env, start time.Time,
	a *txfactory.Account, tx *types.Transaction, timeout time.Duration,
) (time.Duration, error) {
	deadline := start.Add(timeout)

	for {
		pending, _, err := env.ContentFrom(ctx, a.Address)
		if err != nil {
			return 0, err
		}

		if pending[tx.Nonce()] == tx.Hash() {
			return time.Since(start), nil
		}

		if time.Now().After(deadline) {
			return 0, fmt.Errorf("tx %s not pending after %s", tx.Hash(), timeout)
		}

		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
}

// payloadPoll is the wait between a build start and its getPayload while the
// scenario polls for a payload with the new txs. Shorter waits measure the
// harness: some clients (besu) return an early payload for each new build.
const payloadPoll = 25 * time.Millisecond

// timeToPayload sends txs on an empty pool and starts a build right after the
// last send. It measures how long until a built payload holds all of them.
func (s *admission) timeToPayload(ctx context.Context, env *Env, timeout time.Duration) error {
	n := env.Params.Int("payload_txs")

	accounts, err := env.Accounts(ctx, n)
	if err != nil {
		return err
	}

	txs := make([]*types.Transaction, 0, n)

	for _, a := range accounts {
		tx, err := env.Factory.Transfer(a, txfactory.KindDynamicFee, txfactory.DefaultFees())
		if err != nil {
			return err
		}

		txs = append(txs, tx)
	}

	head, err := env.Head(ctx)
	if err != nil {
		return err
	}

	if err := env.MustSend(ctx, txs); err != nil {
		return err
	}

	start := time.Now()
	want := Hashes(txs)

	env.Rec.Event("send", "%d dynamic-fee txs, then build at once", n)

	for attempt := uint64(0); ; attempt++ {
		payload, err := env.BuildOn(ctx, head, cldriver.BuildOptions{Delay: payloadPoll, Salt: 1000 + attempt})
		if err != nil {
			return err
		}

		missing := 0

		for _, h := range want {
			if !payload.Contains(h) {
				missing++
			}
		}

		if missing == 0 {
			took := time.Since(start)
			env.Rec.Metric(Metric{
				Key: "time_to_payload", Label: fmt.Sprintf("Time until %d new txs are in a built payload", n),
				Unit: "ms", Value: Ms(took), Group: "payload", Better: "lower",
			})
			env.Rec.Metric(Metric{
				Key: "builds_to_payload", Label: "Builds until the payload holds every new tx",
				Unit: "builds", Value: float64(attempt + 1), Group: "payload", Better: "lower",
			})

			break
		}

		if time.Since(start) > timeout {
			env.Rec.Check(SeverityHard, false, "Pending txs reach a built payload",
				fmt.Sprintf("%d txs", n), fmt.Sprintf("%d missing after %s", missing, timeout))

			break
		}
	}

	_, err = env.Drain(ctx, 16)

	return err
}
