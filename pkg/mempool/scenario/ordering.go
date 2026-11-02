package scenario

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/ethpandaops/benchmarkoor/pkg/mempool/cldriver"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/txfactory"
)

// ordering checks the payloads a client builds from its pool: nonce order per
// sender, order by tip, filling up to the gas limit, and the blob limit.
type ordering struct{}

func (s *ordering) Name() string { return "payload-ordering" }

func (s *ordering) Description() string {
	return "Builds payloads from the pool. Checks nonce order per sender (hard), order by " +
		"effective tip across senders, filling the block up to the gas limit, and the blob " +
		"limit per block."
}

func (s *ordering) Defaults() Params {
	return Params{
		"order_senders":   12,
		"order_nonces":    4,
		"fill_txs":        1700, // more transfers than one block holds at 30M gas
		"fill_per_sender": 16,
		"blob_txs":        24, // single-blob txs, more than the blob limit
		"timeout_ms":      30000,
	}
}

func (s *ordering) Accounts(p Params) int {
	fillSenders := (p.Int("fill_txs") + p.Int("fill_per_sender") - 1) / p.Int("fill_per_sender")

	return p.Int("order_senders") + fillSenders + p.Int("blob_txs")
}

func (s *ordering) Run(ctx context.Context, env *Env) error {
	if err := s.order(ctx, env); err != nil {
		return fmt.Errorf("ordering: %w", err)
	}

	if err := s.fill(ctx, env); err != nil {
		return fmt.Errorf("fill: %w", err)
	}

	if err := s.blobs(ctx, env); err != nil {
		return fmt.Errorf("blobs: %w", err)
	}

	return nil
}

// tipFor gives distinct tips (in 0.01 gwei steps) without a pattern by sender.
func tipFor(sender, nonce int) *big.Int {
	steps := int64(10 + (sender*37+nonce*11)%97)

	return new(big.Int).Mul(big.NewInt(steps), big.NewInt(10_000_000))
}

func (s *ordering) order(ctx context.Context, env *Env) error {
	p := env.Params
	timeout := p.Duration("timeout_ms")

	accounts, err := env.Accounts(ctx, p.Int("order_senders"))
	if err != nil {
		return err
	}

	txs := make([]*types.Transaction, 0, len(accounts)*p.Int("order_nonces"))

	for i, a := range accounts {
		for j := range p.Int("order_nonces") {
			tx, err := env.Factory.Build(a, txfactory.Template{
				Kind: txfactory.KindDynamicFee,
				Fees: txfactory.Fees{FeeCap: txfactory.GweiN(30), TipCap: tipFor(i, j)},
			})
			if err != nil {
				return err
			}

			txs = append(txs, tx)
		}
	}

	if err := env.MustSend(ctx, txs, "mixed tips from 12 senders"); err != nil {
		return err
	}

	if _, err := env.WaitInPool(ctx, time.Now(), Hashes(txs), timeout); err != nil {
		return err
	}

	head, err := env.Head(ctx)
	if err != nil {
		return err
	}

	payload, err := env.BuildWith(ctx, head, Hashes(txs), buildFull)
	if err != nil {
		return err
	}

	env.Rec.Metric(Metric{
		Key: "build_fcu", Label: "forkchoiceUpdated with attributes", Unit: "ms",
		Value: Ms(payload.FcuDuration), Group: "build", Better: "lower",
	})
	env.Rec.Metric(Metric{
		Key: "build_get_payload", Label: "getPayload", Unit: "ms",
		Value: Ms(payload.GetPayloadDuration), Group: "build", Better: "lower",
	})

	ours := s.ours(payload, txs)
	bad := nonceOrderErrors(ours, env.Factory.Signer())
	env.Rec.Check(SeverityHard, len(bad) == 0, "Txs of one sender are in nonce order, without gaps",
		"in order", fmt.Sprint(bad))

	expected := greedyOrder(txs, payload.Block.BaseFee, env.Factory.Signer())
	pos := firstMismatch(ours, expected)
	env.Rec.Check(SeveritySoft, pos < 0, "Txs are ordered by effective tip (nonce order kept)",
		"the greedy price-and-nonce order", mismatchText(pos))
	env.Rec.Observe("order.actual", "First 12 tips in payload order (gwei, sender:nonce)",
		tipTrace(ours, payload.Block.BaseFee, env.Factory.Signer(), 12))
	env.Rec.Observe("order.expected", "First 12 tips in the greedy order (gwei, sender:nonce)",
		tipTrace(expected, payload.Block.BaseFee, env.Factory.Signer(), 12))

	_, err = env.Drain(ctx, 8)

	return err
}

// ours returns the payload txs that the scenario sent, in payload order.
func (s *ordering) ours(p *cldriver.Payload, sent []*types.Transaction) []*types.Transaction {
	mine := make(map[common.Hash]bool, len(sent))
	for _, tx := range sent {
		mine[tx.Hash()] = true
	}

	out := make([]*types.Transaction, 0, len(sent))

	for _, tx := range p.Txs {
		if mine[tx.Hash()] {
			out = append(out, tx)
		}
	}

	return out
}

// nonceOrderErrors returns the senders whose txs are out of nonce order or
// have a gap in the payload.
func nonceOrderErrors(txs []*types.Transaction, signer types.Signer) []string {
	last := make(map[common.Address]uint64, 16)
	bad := make([]string, 0)

	for _, tx := range txs {
		from, err := types.Sender(signer, tx)
		if err != nil {
			bad = append(bad, err.Error())

			continue
		}

		if prev, ok := last[from]; ok && tx.Nonce() != prev+1 {
			bad = append(bad, fmt.Sprintf("%s: nonce %d after %d", from.Hex()[:10], tx.Nonce(), prev))
		}

		last[from] = tx.Nonce()
	}

	return bad
}

// greedyOrder returns the order a price-sorted builder makes: at each step,
// the sender whose next tx has the highest effective tip. Ties keep the send
// order.
func greedyOrder(txs []*types.Transaction, baseFee *big.Int, signer types.Signer) []*types.Transaction {
	queues := make(map[common.Address][]*types.Transaction, 16)
	senders := make([]common.Address, 0, 16)

	for _, tx := range txs {
		from, _ := types.Sender(signer, tx)
		if _, ok := queues[from]; !ok {
			senders = append(senders, from)
		}

		queues[from] = append(queues[from], tx)
	}

	out := make([]*types.Transaction, 0, len(txs))

	for len(out) < len(txs) {
		var (
			best    common.Address
			bestTip *big.Int
		)

		for _, from := range senders {
			q := queues[from]
			if len(q) == 0 {
				continue
			}

			tip, err := q[0].EffectiveGasTip(baseFee)
			if err != nil {
				continue
			}

			if bestTip == nil || tip.Cmp(bestTip) > 0 {
				best, bestTip = from, tip
			}
		}

		if bestTip == nil {
			break
		}

		out = append(out, queues[best][0])
		queues[best] = queues[best][1:]
	}

	return out
}

// tipTrace lists the effective tip and sender:nonce of the first n txs.
func tipTrace(txs []*types.Transaction, baseFee *big.Int, signer types.Signer, n int) string {
	ids := make(map[common.Address]int, 16)
	parts := make([]string, 0, n)

	for _, tx := range txs[:min(n, len(txs))] {
		from, _ := types.Sender(signer, tx)
		if _, ok := ids[from]; !ok {
			ids[from] = len(ids)
		}

		tip, _ := tx.EffectiveGasTip(baseFee)
		gwei := new(big.Float).Quo(new(big.Float).SetInt(tip), big.NewFloat(1e9))
		parts = append(parts, fmt.Sprintf("%s(s%d:%d)", gwei.Text('f', 2), ids[from], tx.Nonce()))
	}

	return strings.Join(parts, " ")
}

func firstMismatch(got, want []*types.Transaction) int {
	for i := range max(len(got), len(want)) {
		if i >= len(got) || i >= len(want) || got[i].Hash() != want[i].Hash() {
			return i
		}
	}

	return -1
}

func mismatchText(pos int) string {
	if pos < 0 {
		return "same order"
	}

	return fmt.Sprintf("differs from position %d", pos)
}

func (s *ordering) fill(ctx context.Context, env *Env) error {
	p := env.Params
	timeout := p.Duration("timeout_ms")
	perSender := p.Int("fill_per_sender")
	nSenders := (p.Int("fill_txs") + perSender - 1) / perSender

	accounts, err := env.Accounts(ctx, nSenders)
	if err != nil {
		return err
	}

	lanes := make([][]*types.Transaction, 0, nSenders)

	for i, a := range accounts {
		lane := make([]*types.Transaction, 0, perSender)

		for j := range perSender {
			tx, err := env.Factory.Build(a, txfactory.Template{
				Kind: txfactory.KindDynamicFee,
				Fees: txfactory.Fees{FeeCap: txfactory.GweiN(30), TipCap: tipFor(i, j)},
			})
			if err != nil {
				return err
			}

			lane = append(lane, tx)
		}

		lanes = append(lanes, lane)
	}

	all := flatten(lanes)

	if _, rejected, err := env.SendConcurrent(ctx, regroup(lanes, 8), "", "", "", "more txs than a block holds"); rejected > 0 {
		return fmt.Errorf("%d fill txs rejected: %w", rejected, err)
	}

	if _, err := env.WaitInPool(ctx, time.Now(), Hashes(all), timeout); err != nil {
		return err
	}

	payload, err := env.Build(ctx, buildFull)
	if err != nil {
		return err
	}

	used := payload.Envelope.ExecutionPayload.GasUsed
	limit := payload.Block.GasLimit

	env.Rec.Metric(Metric{
		Key: "fill_ratio", Label: "Gas used / gas limit of a payload built from a full pool",
		Unit: "%", Value: round3(100 * float64(used) / float64(limit)), Group: "fill", Better: "higher",
	})
	env.Rec.Metric(Metric{
		Key: "fill_txs", Label: "Txs in a payload built from a full pool", Unit: "txs",
		Value: float64(len(payload.Txs)), Group: "fill", Better: "higher",
	})
	env.Rec.Metric(Metric{
		Key: "fill_get_payload", Label: "getPayload of a full block", Unit: "ms",
		Value: Ms(payload.GetPayloadDuration), Group: "fill", Better: "lower",
	})
	env.Rec.Check(SeveritySoft, used+21_000 > limit, "A payload from a full pool fills the gas limit",
		fmt.Sprintf("more than %d gas", limit-21_000), fmt.Sprintf("%d gas", used))

	bad := nonceOrderErrors(s.ours(payload, all), env.Factory.Signer())
	env.Rec.Check(SeverityHard, len(bad) == 0, "Full block: txs of one sender are in nonce order, without gaps",
		"in order", fmt.Sprint(bad))

	_, err = env.Drain(ctx, 8)

	return err
}

func (s *ordering) blobs(ctx context.Context, env *Env) error {
	p := env.Params
	timeout := p.Duration("timeout_ms")

	accounts, err := env.Accounts(ctx, p.Int("blob_txs"))
	if err != nil {
		return err
	}

	txs := make([]*types.Transaction, 0, len(accounts))

	for _, a := range accounts {
		tx, err := env.Factory.Build(a, txfactory.Template{Kind: txfactory.KindBlob, Blobs: 1})
		if err != nil {
			return err
		}

		txs = append(txs, tx)
	}

	if err := env.MustSend(ctx, txs, "single-blob txs"); err != nil {
		return err
	}

	if _, err := env.WaitInPool(ctx, time.Now(), Hashes(txs), timeout); err != nil {
		return err
	}

	payload, err := env.Build(ctx, buildFull)
	if err != nil {
		return err
	}

	blobs := 0
	for _, tx := range payload.Txs {
		blobs += len(tx.BlobHashes())
	}

	maxBlobs := env.Chain.MaxBlobsPerBlock

	env.Rec.Check(SeverityHard, blobs <= maxBlobs, "A payload holds at most the blob limit",
		fmt.Sprintf("at most %d blobs", maxBlobs), fmt.Sprintf("%d blobs", blobs))
	env.Rec.Check(SeveritySoft, blobs == maxBlobs, "A payload from a full blob pool fills the blob limit",
		fmt.Sprintf("%d blobs", maxBlobs), fmt.Sprintf("%d blobs", blobs))
	env.Rec.Metric(Metric{
		Key: "blob_get_payload", Label: "getPayload with blobs", Unit: "ms",
		Value: Ms(payload.GetPayloadDuration), Group: "blobs", Better: "lower",
	})

	_, err = env.Drain(ctx, 8)

	return err
}
