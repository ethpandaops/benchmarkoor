package scenario

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/ethpandaops/benchmarkoor/pkg/mempool/cldriver"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/observer"
)

// Invariants are hard checks the harness runs on every build and import, in
// every scenario. A failure is a bug even when the scenario tests something
// else.

// gasPerBlob is the blob gas of one blob (EIP-4844).
const gasPerBlob = 131072

// invariantState is the per-scenario state of the invariants.
type invariantState struct {
	// forbidden holds txs that must never be in a payload, with the reason.
	forbidden map[common.Hash]string
	// imported holds the imported payloads whose txs must leave the pool.
	imported []*cldriver.Payload
	// skipBuildValidation turns off the newPayload of every build, for
	// scenarios that time imports (a second newPayload would be "known").
	skipBuildValidation bool
}

// Forbid marks txs that must never be in a payload (invalid, replaced).
func (e *Env) Forbid(reason string, hashes ...common.Hash) {
	if e.inv.forbidden == nil {
		e.inv.forbidden = make(map[common.Hash]string, 16)
	}

	for _, h := range hashes {
		e.inv.forbidden[h] = reason
	}
}

// SkipBuildValidation turns off the newPayload of every build.
func (e *Env) SkipBuildValidation() { e.inv.skipBuildValidation = true }

// CheckBuilt runs the invariants of a built payload.
func (e *Env) CheckBuilt(ctx context.Context, p *cldriver.Payload) {
	ed := p.Envelope.ExecutionPayload

	if p.Attrs != nil {
		mismatch := ""

		switch {
		case ed.Timestamp != p.Attrs.Timestamp:
			mismatch = fmt.Sprintf("timestamp %d, want %d", ed.Timestamp, p.Attrs.Timestamp)
		case ed.Random != p.Attrs.Random:
			mismatch = "prevRandao differs"
		case ed.FeeRecipient != p.Attrs.SuggestedFeeRecipient:
			mismatch = fmt.Sprintf("fee recipient %s", ed.FeeRecipient)
		case !sameWithdrawals(ed.Withdrawals, p.Attrs.Withdrawals):
			mismatch = fmt.Sprintf("%d withdrawals, want %d", len(ed.Withdrawals), len(p.Attrs.Withdrawals))
		case p.Parent != (common.Hash{}) && ed.ParentHash != p.Parent:
			mismatch = "parent hash differs"
		}

		e.Rec.Invariant(mismatch == "", "A built payload matches its build attributes", "match",
			okOr(mismatch))
	}

	seen := make(map[common.Hash]bool, len(p.TxHashes))
	dup := 0
	forbidden := ""

	for _, h := range p.TxHashes {
		if seen[h] {
			dup++
		}

		seen[h] = true

		if r, bad := e.inv.forbidden[h]; bad && forbidden == "" {
			forbidden = fmt.Sprintf("%s (%s)", h.Hex()[:10], r)
		}
	}

	e.Rec.Invariant(dup == 0, "No tx appears twice in a payload", "0", fmt.Sprintf("%d", dup))
	e.Rec.Invariant(forbidden == "", "A tx that must never be included is not in a payload", "none",
		okOr(forbidden))

	over := 0
	for _, tx := range p.Txs {
		if tx.Gas() > e.Chain.MaxTxGas {
			over++
		}
	}

	blobGas := uint64(0)
	if ed.BlobGasUsed != nil {
		blobGas = *ed.BlobGasUsed
	}

	e.Rec.Invariant(ed.GasUsed <= ed.GasLimit && blobGas <= uint64(e.Chain.MaxBlobsPerBlock)*gasPerBlob && over == 0,
		"A payload stays within the gas, blob gas and per-tx gas limits", "within",
		fmt.Sprintf("gas %d/%d, blob gas %d, %d txs over the tx cap", ed.GasUsed, ed.GasLimit, blobGas, over))

	ok, detail := checkBundle(p)
	e.Rec.Invariant(ok, "A payload's blobs bundle matches its blob txs and its cell proofs verify", "valid", detail)

	if !e.inv.skipBuildValidation {
		_, err := e.Driver.NewPayload(ctx, p)
		e.Rec.Invariant(err == nil, "Every built payload is valid (newPayload)", "VALID", ErrText(err))
	}
}

func okOr(s string) string {
	if s == "" {
		return "ok"
	}

	return s
}

func sameWithdrawals(a, b []*types.Withdrawal) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if *a[i] != *b[i] {
			return false
		}
	}

	return true
}

// checkBundle checks the blobs bundle of a payload: one commitment and blob
// per blob of its txs, in order, 128 cell proofs per blob, and the proofs
// verify.
func checkBundle(p *cldriver.Payload) (bool, string) {
	want := make([]common.Hash, 0, 8)
	for _, tx := range p.Txs {
		want = append(want, tx.BlobHashes()...)
	}

	b := p.Envelope.BlobsBundle
	if b == nil {
		if len(want) == 0 {
			return true, "no blobs"
		}

		return false, fmt.Sprintf("%d blobs in txs, no bundle", len(want))
	}

	if len(b.Commitments) != len(want) || len(b.Blobs) != len(want) {
		return false, fmt.Sprintf("%d blobs in txs, %d commitments, %d blobs in the bundle",
			len(want), len(b.Commitments), len(b.Blobs))
	}

	if len(b.Proofs) != len(want)*kzg4844.CellProofsPerBlob {
		return false, fmt.Sprintf("%d proofs for %d blobs, want %d per blob", len(b.Proofs), len(want),
			kzg4844.CellProofsPerBlob)
	}

	if len(want) == 0 {
		return true, "no blobs"
	}

	blobs := make([]kzg4844.Blob, len(want))
	commitments := make([]kzg4844.Commitment, len(want))
	proofs := make([]kzg4844.Proof, len(b.Proofs))

	for i := range want {
		copy(blobs[i][:], b.Blobs[i])
		copy(commitments[i][:], b.Commitments[i])

		if kzg4844.CalcBlobHashV1(sha256.New(), &commitments[i]) != want[i] {
			return false, fmt.Sprintf("commitment %d does not match the tx's versioned hash", i)
		}
	}

	for i := range b.Proofs {
		copy(proofs[i][:], b.Proofs[i])
	}

	if err := kzg4844.VerifyCellProofs(blobs, commitments, proofs); err != nil {
		return false, fmt.Sprintf("cell proofs do not verify: %v", err)
	}

	return true, fmt.Sprintf("%d blobs verified", len(want))
}

// afterImport runs the invariants of an imported payload: the head moved,
// and blockValue equals the priority fees the receipts show.
func (e *Env) afterImport(ctx context.Context, p *cldriver.Payload) {
	head, err := e.Head(ctx)
	if err == nil {
		e.Rec.Invariant(head.Hash == p.Block.Hash, "After newPayload and forkchoiceUpdated, the head is the new block",
			p.Block.Hash.Hex()[:10], head.Hash.Hex()[:10])
	}

	if p.Envelope.BlockValue != nil {
		value, err := e.priorityFees(ctx, p)
		if err == nil {
			e.Rec.Invariant(value.Cmp(p.Envelope.BlockValue) == 0,
				"getPayload's blockValue equals the priority fees paid", value.String()+" wei",
				p.Envelope.BlockValue.String()+" wei")
		}
	}

	e.inv.imported = append(e.inv.imported, p)
}

// priorityFees sums (effectiveGasPrice - baseFee) * gasUsed over the receipts
// of a block: what the fee recipient earns.
func (e *Env) priorityFees(ctx context.Context, p *cldriver.Payload) (*big.Int, error) {
	total := new(big.Int)
	base := p.Block.BaseFee

	for start := 0; start < len(p.TxHashes); start += observer.BatchSize {
		chunk := p.TxHashes[start:min(start+observer.BatchSize, len(p.TxHashes))]

		type receipt struct {
			GasUsed           hexutil.Uint64 `json:"gasUsed"`
			EffectiveGasPrice *hexutil.Big   `json:"effectiveGasPrice"`
		}

		out := make([]*receipt, len(chunk))
		batch := make([]rpc.BatchElem, len(chunk))

		for i, h := range chunk {
			batch[i] = rpc.BatchElem{Method: "eth_getTransactionReceipt", Args: []any{h}, Result: &out[i]}
		}

		if err := e.Pool.RPC().BatchCallContext(ctx, batch); err != nil {
			return nil, err
		}

		for i := range chunk {
			if batch[i].Error != nil || out[i] == nil || out[i].EffectiveGasPrice == nil {
				return nil, fmt.Errorf("no receipt for %s", chunk[i])
			}

			tip := new(big.Int).Sub(out[i].EffectiveGasPrice.ToInt(), base)
			total.Add(total, tip.Mul(tip, new(big.Int).SetUint64(uint64(out[i].GasUsed))))
		}
	}

	return total, nil
}

// checkImported checks that the txs of imported payloads left the pool. A
// tx counts only while it is mined on the canonical chain (a reorg can bring
// it back on purpose). It waits a little for a background pool reset.
func (e *Env) checkImported(ctx context.Context) {
	if len(e.inv.imported) == 0 {
		return
	}

	hashes := make([]common.Hash, 0, 64)
	for _, p := range e.inv.imported {
		hashes = append(hashes, p.TxHashes...)
	}

	e.inv.imported = e.inv.imported[:0]

	if len(hashes) == 0 {
		return
	}

	deadline := time.Now().Add(3 * time.Second)

	for {
		left, err := e.minedButPooled(ctx, hashes)
		if err != nil {
			return
		}

		if len(left) == 0 {
			e.Rec.Invariant(true, "Included txs leave the pool", "0 in the pool", "0")

			return
		}

		if time.Now().After(deadline) {
			e.Rec.Invariant(false, "Included txs leave the pool", "0 in the pool",
				fmt.Sprintf("%d mined txs still in the pool (e.g. %s)", len(left), left[0].Hex()[:10]))

			return
		}

		hashes = left

		if sleepCtx(ctx, 50*time.Millisecond) != nil {
			return
		}
	}
}

// minedButPooled returns the hashes that are mined on the canonical chain and
// still listed by the pool.
func (e *Env) minedButPooled(ctx context.Context, hashes []common.Hash) ([]common.Hash, error) {
	content, err := e.Pool.Content(ctx)
	if err != nil {
		return nil, err
	}

	pooled := make([]common.Hash, 0, len(hashes))

	for _, h := range hashes {
		if content.Has(h) {
			pooled = append(pooled, h)
		}
	}

	if len(pooled) == 0 {
		return nil, nil
	}

	out := make([]common.Hash, 0, len(pooled))

	for start := 0; start < len(pooled); start += observer.BatchSize {
		chunk := pooled[start:min(start+observer.BatchSize, len(pooled))]
		res := make([]*struct {
			BlockHash *common.Hash `json:"blockHash"`
		}, len(chunk))
		batch := make([]rpc.BatchElem, len(chunk))

		for i, h := range chunk {
			batch[i] = rpc.BatchElem{Method: "eth_getTransactionByHash", Args: []any{h}, Result: &res[i]}
		}

		if err := e.Pool.RPC().BatchCallContext(ctx, batch); err != nil {
			return nil, err
		}

		for i, h := range chunk {
			if batch[i].Error == nil && res[i] != nil && res[i].BlockHash != nil {
				out = append(out, h)
			}
		}
	}

	return out, nil
}

// maxConsistencySenders caps the senders checked by PoolConsistency.
const maxConsistencySenders = 300

// PoolConsistency checks the pool against the chain after it settles: no tx
// below its sender's nonce, and each sender's pending txs contiguous from the
// account nonce. It also compares txpool_status with txpool_content (soft:
// the two calls are not atomic).
func (e *Env) PoolConsistency(ctx context.Context) {
	if !e.Pool.Caps().QueuedVisible {
		return
	}

	if _, err := settle(ctx, e, 300*time.Millisecond, 5*time.Second); err != nil {
		return
	}

	st, err := e.Pool.Status(ctx)
	if err != nil {
		return
	}

	content, err := e.Pool.Content(ctx)
	if err != nil {
		return
	}

	nonBlob := len(content.Pending) + len(content.Queued)
	e.Rec.CheckOnce(SeveritySoft, st.Queued == len(content.Queued) && st.Pending >= len(content.Pending),
		"Invariant: txpool_status agrees with txpool_content",
		fmt.Sprintf("%d pending (+blob txs), %d queued", len(content.Pending), len(content.Queued)),
		fmt.Sprintf("%d pending, %d queued", st.Pending, st.Queued))

	if nonBlob == 0 {
		return
	}

	pending := make(map[common.Address][]uint64, 64)
	queued := make(map[common.Address][]uint64, 64)

	for _, ref := range content.Pending {
		pending[ref.From] = append(pending[ref.From], ref.Nonce)
	}

	for _, ref := range content.Queued {
		queued[ref.From] = append(queued[ref.From], ref.Nonce)
	}

	senders := make([]common.Address, 0, len(pending)+len(queued))
	for a := range pending {
		senders = append(senders, a)
	}

	for a := range queued {
		if _, ok := pending[a]; !ok {
			senders = append(senders, a)
		}
	}

	if len(senders) > maxConsistencySenders {
		senders = senders[:maxConsistencySenders]
	}

	nonces, err := e.stateNonces(ctx, senders)
	if err != nil {
		return
	}

	stale, gapped := "", ""

	for _, a := range senders {
		base := nonces[a]

		for _, n := range append(append([]uint64{}, pending[a]...), queued[a]...) {
			if n < base && stale == "" {
				stale = fmt.Sprintf("%s nonce %d < account nonce %d", a.Hex()[:10], n, base)
			}
		}

		have := make(map[uint64]bool, len(pending[a]))
		for _, n := range pending[a] {
			have[n] = true
		}

		for i := range len(pending[a]) {
			if !have[base+uint64(i)] && gapped == "" {
				gapped = fmt.Sprintf("%s pending txs not contiguous from nonce %d", a.Hex()[:10], base)
			}
		}
	}

	e.Rec.Invariant(stale == "", "The pool holds no tx below its sender's nonce", "none", okOr(stale))
	e.Rec.Invariant(gapped == "", "Each sender's pending txs are contiguous from its account nonce", "contiguous",
		okOr(gapped))
}

func (e *Env) stateNonces(ctx context.Context, addrs []common.Address) (map[common.Address]uint64, error) {
	out := make(map[common.Address]uint64, len(addrs))

	for start := 0; start < len(addrs); start += observer.BatchSize {
		chunk := addrs[start:min(start+observer.BatchSize, len(addrs))]
		res := make([]hexutil.Uint64, len(chunk))
		batch := make([]rpc.BatchElem, len(chunk))

		for i, a := range chunk {
			batch[i] = rpc.BatchElem{Method: "eth_getTransactionCount", Args: []any{a, "latest"}, Result: &res[i]}
		}

		if err := e.Pool.RPC().BatchCallContext(ctx, batch); err != nil {
			return nil, err
		}

		for i, a := range chunk {
			if batch[i].Error != nil {
				return nil, batch[i].Error
			}

			out[a] = uint64(res[i])
		}
	}

	return out, nil
}

// CheckBlobsAvailable checks engine_getBlobsV2 for blob txs in the pool: the
// CL must get every blob with 128 cell proofs that verify.
func (e *Env) CheckBlobsAvailable(ctx context.Context, txs []*types.Transaction, when string) {
	hashes := make([]common.Hash, 0, 8)
	for _, tx := range txs {
		hashes = append(hashes, tx.BlobHashes()...)
	}

	if len(hashes) == 0 {
		return
	}

	out, err := e.Driver.GetBlobsV2(ctx, hashes)
	if err != nil {
		e.Rec.Invariant(false, "engine_getBlobsV2 returns the blobs of pool txs", "all blobs", ErrText(err))

		return
	}

	detail := verifyBlobsV2(hashes, out)
	e.Rec.Invariant(detail == "", "engine_getBlobsV2 returns the blobs of pool txs ("+when+")", "all blobs, proofs verify",
		okOr(detail))
}

func verifyBlobsV2(hashes []common.Hash, out []*engine.BlobAndProofV2) string {
	if out == nil {
		return "null (a blob is missing)"
	}

	if len(out) != len(hashes) {
		return fmt.Sprintf("%d entries for %d blobs", len(out), len(hashes))
	}

	for i, b := range out {
		if b == nil {
			return fmt.Sprintf("blob %d missing", i)
		}

		var blob kzg4844.Blob

		copy(blob[:], b.Blob)

		c, err := kzg4844.BlobToCommitment(&blob)
		if err != nil {
			return err.Error()
		}

		if kzg4844.CalcBlobHashV1(sha256.New(), &c) != hashes[i] {
			return fmt.Sprintf("blob %d does not match its versioned hash", i)
		}

		if len(b.CellProofs) != kzg4844.CellProofsPerBlob {
			return fmt.Sprintf("blob %d has %d proofs", i, len(b.CellProofs))
		}

		proofs := make([]kzg4844.Proof, len(b.CellProofs))
		for j := range b.CellProofs {
			copy(proofs[j][:], b.CellProofs[j])
		}

		if err := kzg4844.VerifyCellProofs([]kzg4844.Blob{blob}, []kzg4844.Commitment{c}, proofs); err != nil {
			return fmt.Sprintf("blob %d: cell proofs do not verify", i)
		}
	}

	return ""
}

// CheckRPCRoundTrip checks that eth_getTransactionByHash returns the tx that
// was sent (same hash, so the same signed fields).
func (e *Env) CheckRPCRoundTrip(ctx context.Context, txs []*types.Transaction) {
	for _, tx := range txs {
		var got *types.Transaction
		if err := e.Pool.RPC().CallContext(ctx, &got, "eth_getTransactionByHash", tx.Hash()); err != nil {
			e.Rec.Invariant(false, "eth_getTransactionByHash returns the tx that was sent", "same tx", ErrText(err))

			return
		}

		ok := got != nil && got.Hash() == tx.Hash() && got.Nonce() == tx.Nonce() && got.Type() == tx.Type()
		e.Rec.Invariant(ok, "eth_getTransactionByHash returns the tx that was sent", "same tx",
			fmt.Sprintf("type %d nonce %d", tx.Type(), tx.Nonce()))
	}
}

// FinalInvariants runs the invariants that need a settled pool. The executor
// calls it after each scenario.
func (e *Env) FinalInvariants(ctx context.Context) {
	e.checkImported(ctx)
	e.PoolConsistency(ctx)
}
