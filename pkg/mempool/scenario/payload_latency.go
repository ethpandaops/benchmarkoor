package scenario

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/core/types"

	"github.com/ethpandaops/benchmarkoor/pkg/mempool/cldriver"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/txfactory"
)

// payloadLatency measures how fresh a built payload is, the way a CL uses
// the builder: one forkchoiceUpdated with attributes at the start of a
// window, txs arriving through the window, and one getPayload at its end.
// A builder that rebuilds often includes txs that arrived late.
type payloadLatency struct{}

func (s *payloadLatency) Name() string { return "payload-latency" }

func (s *payloadLatency) Description() string {
	return "Starts one payload build, sends txs at a steady pace through a window, and gets " +
		"the payload once at the end (as a CL does). Records which txs made it in, and how " +
		"old the newest included tx is when the payload is delivered."
}

func (s *payloadLatency) Defaults() Params {
	return Params{
		"rounds":     3,
		"window_ms":  4000, // from forkchoiceUpdated to getPayload
		"txs":        40,   // txs per round, spread evenly over the window
		"timeout_ms": 30000,
	}
}

func (s *payloadLatency) Accounts(p Params) int { return p.Int("rounds") * p.Int("txs") }

func (s *payloadLatency) Run(ctx context.Context, env *Env) error {
	// This scenario times imports: a second newPayload of a build would be
	// "already known" and distort them.
	env.SkipBuildValidation()

	p := env.Params
	window := p.Duration("window_ms")
	n := p.Int("txs")

	var included, sent int

	for round := range p.Int("rounds") {
		in, err := s.round(ctx, env, round, window, n)
		if err != nil {
			return fmt.Errorf("round %d: %w", round, err)
		}

		included += in
		sent += n
	}

	env.Rec.Metric(Metric{
		Key: "inclusion", Label: "Txs sent during the window that are in the delivered payload",
		Unit: "%", Value: round3(100 * float64(included) / float64(max(1, sent))),
		Group: "payload", Better: "higher",
	})

	return nil
}

func (s *payloadLatency) round(ctx context.Context, env *Env, round int, window time.Duration, n int,
) (int, error) {
	accs, err := env.Accounts(ctx, n)
	if err != nil {
		return 0, err
	}

	// Sign first, so the pace of the sends is not the pace of the signing.
	txs := make([]*types.Transaction, 0, n)

	for _, a := range accs {
		tx, err := env.Factory.Transfer(a, txfactory.KindDynamicFee, txfactory.DefaultFees())
		if err != nil {
			return 0, err
		}

		txs = append(txs, tx)
	}

	head, err := env.Head(ctx)
	if err != nil {
		return 0, err
	}

	attrs := env.Driver.Attributes(head, cldriver.BuildOptions{Salt: uint64(round)})

	resp, fcuTook, err := env.Driver.ForkchoiceUpdated(ctx, head.Hash, attrs)
	if err != nil {
		return 0, err
	}

	if resp.PayloadID == nil {
		return 0, fmt.Errorf("no payload id")
	}

	start := time.Now()
	env.Rec.Event("fcu", "round %d: build started in %.2fms", round, Ms(fcuTook))

	offsets := make([]time.Duration, n)
	step := window / time.Duration(n)

	env.Rec.Event("send", "round %d: %d txs spread over %s", round, n, window)

	for i, tx := range txs {
		at := time.Duration(i) * step

		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(time.Until(start.Add(at))):
		}

		offsets[i] = time.Since(start)

		if _, err := env.Send(ctx, tx); err != nil {
			return 0, fmt.Errorf("sending tx %d: %w", i, err)
		}
	}

	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-time.After(time.Until(start.Add(window))):
	}

	delivered := time.Since(start)

	envelope, getTook, err := env.Driver.GetPayload(ctx, *resp.PayloadID, attrs.Timestamp)
	if err != nil {
		return 0, err
	}

	payload, err := cldriver.NewPayloadFromEnvelope(envelope, *attrs.BeaconRoot)
	if err != nil {
		return 0, err
	}

	payload.Attrs, payload.Parent = attrs, head.Hash
	env.CheckBuilt(ctx, payload)

	env.Rec.Latency("get_payload", "getPayload at the end of the window", "payload", getTook)

	in := 0
	newest := time.Duration(-1)
	missed := make([]string, 0, n)

	for i, tx := range txs {
		if payload.Contains(tx.Hash()) {
			in++
			newest = max(newest, offsets[i])
		} else {
			missed = append(missed, fmt.Sprintf("%d", offsets[i].Milliseconds()))
		}
	}

	if newest >= 0 {
		env.Rec.Latency("staleness", "Age of the newest included tx when the payload is delivered",
			"payload", delivered-newest)
	} else {
		env.Rec.Latency("staleness", "Age of the newest included tx when the payload is delivered",
			"payload", delivered)
	}

	env.Rec.Observe(fmt.Sprintf("round%d", round),
		fmt.Sprintf("Round %d: included of sent; send offsets (ms) of the missed txs", round),
		fmt.Sprintf("%d of %d; missed: %s", in, n, strings.Join(missed, " ")))

	if _, err := env.Import(ctx, payload); err != nil {
		return 0, err
	}

	// Clear the txs that missed the payload for the next round.
	if _, err := env.Drain(ctx, 4); err != nil {
		return 0, err
	}

	return in, nil
}
