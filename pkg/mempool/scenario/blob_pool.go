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

// blobPool tests the blob pool around chain events and the blob fee.
type blobPool struct{}

func (s *blobPool) Name() string { return "blob-pool" }

func (s *blobPool) Description() string {
	return "Blob txs in an orphaned block come back after a reorg with their sidecars, and the " +
		"next payload carries their blobs. Full-blob blocks raise the blob base fee above a " +
		"tx's maxFeePerBlobGas: it must never be included while priced out. Also the number " +
		"of blob txs one sender may have pending."
}

func (s *blobPool) Defaults() Params {
	return Params{
		"reorg_txs":      3,  // blob txs (2 blobs each) in the orphaned block
		"raise_blocks":   20, // max full-blob blocks to raise the blob base fee
		"priced_blocks":  3,  // full-blob blocks while the tx is priced out
		"per_sender_txs": 24, // blob txs one sender tries to have pending
		"timeout_ms":     30000,
	}
}

func (s *blobPool) Accounts(p Params) int { return p.Int("reorg_txs") + 4 }

func (s *blobPool) Run(ctx context.Context, env *Env) error {
	p := env.Params

	accs, err := env.Accounts(ctx, p.Int("reorg_txs")+4)
	if err != nil {
		return err
	}

	n := p.Int("reorg_txs")

	if err := s.reorg(ctx, env, accs[:n]); err != nil {
		return fmt.Errorf("reorg: %w", err)
	}

	if err := s.pricedOut(ctx, env, accs[n], accs[n+1], accs[n+2]); err != nil {
		return fmt.Errorf("blob fee: %w", err)
	}

	if err := s.perSender(ctx, env, accs[n+3]); err != nil {
		return fmt.Errorf("per sender: %w", err)
	}

	return nil
}

// bundleMatches returns true when the blobs bundle of a payload holds exactly
// the blobs of its blob txs, in order.
func bundleMatches(p *cldriver.Payload) (bool, string) {
	want := 0
	for _, tx := range p.Txs {
		want += len(tx.BlobHashes())
	}

	got := 0
	if p.Envelope.BlobsBundle != nil {
		got = len(p.Envelope.BlobsBundle.Commitments)
	}

	return want == got, fmt.Sprintf("%d blobs in txs, %d commitments in the bundle", want, got)
}

func (s *blobPool) reorg(ctx context.Context, env *Env, senders []*txfactory.Account) error {
	timeout := env.Params.Duration("timeout_ms")

	parent, err := env.Head(ctx)
	if err != nil {
		return err
	}

	// B1 first, from an empty pool: the sibling without the blob txs.
	b1, err := env.BuildOn(ctx, parent, cldriver.BuildOptions{Salt: 1})
	if err != nil {
		return err
	}

	txs := make([]*types.Transaction, 0, len(senders))

	for _, a := range senders {
		tx, err := env.Factory.Build(a, txfactory.Template{Kind: txfactory.KindBlob, Blobs: 2})
		if err != nil {
			return err
		}

		txs = append(txs, tx)
	}

	if err := env.MustSend(ctx, txs, "blob txs for A1"); err != nil {
		return err
	}

	if _, err := env.WaitInPool(ctx, time.Now(), Hashes(txs), timeout); err != nil {
		return err
	}

	a1, err := env.BuildWith(ctx, parent, Hashes(txs), cldriver.BuildOptions{Salt: 2})
	if err != nil {
		return err
	}

	ok, detail := bundleMatches(a1)
	env.Rec.Check(SeverityHard, ok, "A payload's blobs bundle matches its blob txs", "match", detail)

	if _, err := env.Driver.NewPayload(ctx, b1); err != nil {
		return fmt.Errorf("importing B1 as a side block: %w", err)
	}

	if _, err := env.Import(ctx, a1); err != nil {
		return err
	}

	if _, err := env.WaitGone(ctx, time.Now(), Hashes(txs), timeout); err != nil {
		return fmt.Errorf("waiting for the A1 blob txs to leave the pool: %w", err)
	}

	start := time.Now()

	_, fcuTook, err := env.Driver.ForkchoiceUpdated(ctx, b1.Block.Hash, nil)
	if err != nil {
		return fmt.Errorf("reorg to B1: %w", err)
	}

	env.Rec.Event("reorg", "head A1 -> B1 (A1 had %d blob txs) in %.2fms", len(txs), Ms(fcuTook))

	back, err := env.WaitInPool(ctx, start, Hashes(txs), 10*time.Second)
	env.Rec.Check(SeveritySoft, err == nil, "Blob txs of an orphaned block come back to the pool",
		fmt.Sprintf("%d txs", len(txs)), ErrText(err))

	if err == nil {
		env.Rec.Metric(Metric{
			Key: "reorg.reinjected", Label: "Time until the blob txs are back after the reorg", Unit: "ms",
			Value: Ms(back), Group: "reorg", Better: "lower",
		})
	}

	b2, err := env.BuildOn(ctx, &b1.Block, buildFull)
	if err != nil {
		return err
	}

	in := 0
	for _, tx := range txs {
		if b2.Contains(tx.Hash()) {
			in++
		}
	}

	env.Rec.Check(SeveritySoft, in == len(txs), "The next payload includes the reinjected blob txs (sidecars kept)",
		fmt.Sprintf("%d txs", len(txs)), fmt.Sprintf("%d included", in))

	ok, detail = bundleMatches(b2)
	env.Rec.Check(SeverityHard, ok, "After the reorg, the payload's blobs bundle matches its blob txs", "match", detail)

	_, err = env.Import(ctx, b2)
	env.Rec.Check(SeverityHard, err == nil, "The payload with reinjected blob txs is valid", "VALID", ErrText(err))

	return err
}

// fillBlobs sends blob txs that fill the next block's blob limit and imports
// that block. It returns the block.
func fillBlobs(ctx context.Context, env *Env, a, b *txfactory.Account, fees txfactory.Fees,
) (*cldriver.Payload, error) {
	maxBlobs := env.Chain.MaxBlobsPerBlock
	first := min(6, maxBlobs)

	txs := make([]*types.Transaction, 0, 2)

	tx, err := env.Factory.Build(a, txfactory.Template{Kind: txfactory.KindBlob, Blobs: first, Fees: fees})
	if err != nil {
		return nil, err
	}

	txs = append(txs, tx)

	if rest := maxBlobs - first; rest > 0 {
		tx, err := env.Factory.Build(b, txfactory.Template{Kind: txfactory.KindBlob, Blobs: rest, Fees: fees})
		if err != nil {
			return nil, err
		}

		txs = append(txs, tx)
	}

	if err := env.MustSend(ctx, txs, "fill the blob limit"); err != nil {
		return nil, err
	}

	head, err := env.Head(ctx)
	if err != nil {
		return nil, err
	}

	p, err := env.BuildWith(ctx, head, Hashes(txs), buildFull)
	if err != nil {
		return nil, err
	}

	if _, err := env.Import(ctx, p); err != nil {
		return nil, err
	}

	return p, nil
}

// pricedOut raises the blob base fee above a tx's maxFeePerBlobGas. Under
// EIP-7918 the blob base fee does not fall again with empty blocks while it
// is below its reserve price, so the case checks only that the priced-out tx
// is never included, and whether the pool keeps it.
func (s *blobPool) pricedOut(ctx context.Context, env *Env, fa, fb, sender *txfactory.Account) error {
	p := env.Params
	fillFees := txfactory.Fees{FeeCap: txfactory.GweiN(50), TipCap: txfactory.GweiN(2), BlobFeeCap: txfactory.GweiN(10)}

	blobCap, err := env.BlobBaseFee(ctx)
	if err != nil {
		return err
	}

	raised := 0

	for {
		fee, err := env.BlobBaseFee(ctx)
		if err != nil {
			return err
		}

		if fee.Cmp(blobCap) > 0 {
			break
		}

		if raised >= p.Int("raise_blocks") {
			env.Rec.Observe("blob_fee.raise", "Blob base fee after full-blob blocks",
				fmt.Sprintf("still %s wei after %d blocks; case skipped", fee, raised))

			return nil
		}

		if _, err := fillBlobs(ctx, env, fa, fb, fillFees); err != nil {
			return err
		}

		raised++
	}

	fee, err := env.BlobBaseFee(ctx)
	if err != nil {
		return err
	}

	env.Rec.Event("basefee", "blob base fee %s -> %s wei after %d full-blob blocks", blobCap, fee, raised)

	tx, err := env.Factory.Build(sender, txfactory.Template{
		Kind: txfactory.KindBlob, Blobs: 1,
		Fees: txfactory.Fees{FeeCap: txfactory.GweiN(50), TipCap: txfactory.GweiN(5), BlobFeeCap: new(big.Int).Set(blobCap)},
	})
	if err != nil {
		return err
	}

	_, sendErr := env.Send(ctx, tx)
	env.Rec.Event("send", "blob tx with maxFeePerBlobGas %s wei (blob base fee %s)", blobCap, fee)
	env.Rec.Observe("blob_fee.admission", "Blob tx with maxFeePerBlobGas below the blob base fee", SendText(sendErr))

	if sendErr != nil {
		return nil
	}

	included := 0

	for range p.Int("priced_blocks") {
		blk, err := fillBlobs(ctx, env, fa, fb, fillFees)
		if err != nil {
			return err
		}

		if blk.Contains(tx.Hash()) {
			included++
		}
	}

	env.Rec.Check(SeverityHard, included == 0, "A blob tx priced out by the blob base fee is never in a payload",
		"0", fmt.Sprintf("%d", included))

	in, err := env.Pool.InPool(ctx, []common.Hash{tx.Hash()})
	if err != nil {
		return err
	}

	env.Rec.Observe("blob_fee.kept", "Priced-out blob tx kept in the pool", fmt.Sprintf("%v", in[tx.Hash()]))

	return nil
}

// perSender: one sender sends many blob txs. Pools limit blob txs per
// account (geth: 16); the count kept is a pool policy.
func (s *blobPool) perSender(ctx context.Context, env *Env, a *txfactory.Account) error {
	n := env.Params.Int("per_sender_txs")
	txs := make([]*types.Transaction, 0, n)

	for range n {
		tx, err := env.Factory.Build(a, txfactory.Template{Kind: txfactory.KindBlob, Blobs: 1})
		if err != nil {
			return err
		}

		txs = append(txs, tx)
	}

	accepted := 0

	var firstErr error

	env.Rec.Event("send", "one sender: %d blob txs", n)

	for _, tx := range txs {
		if _, err := env.Send(ctx, tx); err != nil {
			if firstErr == nil {
				firstErr = err
			}

			continue
		}

		accepted++
	}

	if _, err := settle(ctx, env, 500*time.Millisecond, env.Params.Duration("timeout_ms")); err != nil {
		return err
	}

	kept, err := countIn(ctx, env, txs)
	if err != nil {
		return err
	}

	env.Rec.Metric(Metric{
		Key: "per_sender.kept", Label: fmt.Sprintf("Blob txs kept from one sender that sent %d", n),
		Unit: "txs", Value: float64(kept), Group: "per sender",
	})
	env.Rec.Observe("per_sender", fmt.Sprintf("One sender sends %d blob txs: accepted / kept", n),
		fmt.Sprintf("%d / %d (%s)", accepted, kept, SendText(firstErr)))

	// No drain: the priced-out blob tx of the case before can never be
	// included (under EIP-7918 the blob base fee does not fall back), and
	// this is the last case.
	return nil
}
