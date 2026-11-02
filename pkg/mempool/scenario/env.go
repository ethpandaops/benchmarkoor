package scenario

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"

	"github.com/ethpandaops/benchmarkoor/pkg/mempool/cldriver"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/observer"
	"github.com/ethpandaops/benchmarkoor/pkg/mempool/txfactory"
)

// pollEvery is the poll interval of the wait helpers.
const pollEvery = 5 * time.Millisecond

// syncNonces sets the factory nonces of accounts from the chain head.
func (e *Env) syncNonces(ctx context.Context, accounts []*txfactory.Account) error {
	for start := 0; start < len(accounts); start += observer.BatchSize {
		chunk := accounts[start:min(start+observer.BatchSize, len(accounts))]
		results := make([]hexutil.Uint64, len(chunk))
		batch := make([]rpc.BatchElem, len(chunk))

		for i, a := range chunk {
			batch[i] = rpc.BatchElem{
				Method: "eth_getTransactionCount",
				Args:   []any{a.Address, "latest"},
				Result: &results[i],
			}
		}

		if err := e.Pool.RPC().BatchCallContext(ctx, batch); err != nil {
			return fmt.Errorf("eth_getTransactionCount batch: %w", err)
		}

		for i, a := range chunk {
			if batch[i].Error != nil {
				return fmt.Errorf("eth_getTransactionCount %s: %w", a.Address, batch[i].Error)
			}

			a.SetNonce(uint64(results[i]))
		}
	}

	return nil
}

// Send sends one tx and returns the call time and the RPC error (a rejection
// is an error here, not a failure of the scenario).
func (e *Env) Send(ctx context.Context, tx *types.Transaction) (time.Duration, error) {
	raw, err := tx.MarshalBinary()
	if err != nil {
		return 0, fmt.Errorf("encoding tx: %w", err)
	}

	return e.Pool.SendRaw(ctx, raw)
}

// sendEvent records a "send" event for a batch. what describes the batch;
// without it the event names only the size.
func (e *Env) sendEvent(n int, what []string) {
	count := fmt.Sprintf("%d txs", n)
	if n == 1 {
		count = "1 tx"
	}

	if len(what) > 0 && what[0] != "" {
		e.Rec.Event("send", "%s: %s", what[0], count)

		return
	}

	e.Rec.Event("send", "%s", count)
}

// MustSend sends txs one by one and returns an error on the first rejection.
// It records a "send" event; what (optional) describes the batch.
func (e *Env) MustSend(ctx context.Context, txs []*types.Transaction, what ...string) error {
	e.sendEvent(len(txs), what)

	for _, tx := range txs {
		if _, err := e.Send(ctx, tx); err != nil {
			return fmt.Errorf("sending tx %s (nonce %d): %w", tx.Hash(), tx.Nonce(), err)
		}
	}

	return nil
}

// SendConcurrent sends the txs of each lane in order, with one goroutine per
// lane, and records each call time under key. It returns the wall time. It
// records a "send" event; what (optional) describes the batch.
func (e *Env) SendConcurrent(ctx context.Context, lanes [][]*types.Transaction,
	key, label, group string, what ...string,
) (time.Duration, int, error) {
	n := 0
	for _, l := range lanes {
		n += len(l)
	}

	e.sendEvent(n, what)

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
		rejected int
	)

	start := time.Now()

	for _, lane := range lanes {
		wg.Add(1)

		go func(lane []*types.Transaction) {
			defer wg.Done()

			for _, tx := range lane {
				took, err := e.Send(ctx, tx)
				if err != nil {
					mu.Lock()
					rejected++

					if firstErr == nil {
						firstErr = fmt.Errorf("tx %s: %w", tx.Hash(), err)
					}
					mu.Unlock()

					continue
				}

				if key != "" {
					e.Rec.Latency(key, label, group, took)
				}
			}
		}(lane)
	}

	wg.Wait()

	return time.Since(start), rejected, firstErr
}

// Hashes returns the hashes of txs.
func Hashes(txs []*types.Transaction) []common.Hash {
	out := make([]common.Hash, 0, len(txs))
	for _, tx := range txs {
		out = append(out, tx.Hash())
	}

	return out
}

// WaitInPool waits until every hash is in the pool. It returns the time from
// start.
func (e *Env) WaitInPool(ctx context.Context, start time.Time, hashes []common.Hash,
	timeout time.Duration,
) (time.Duration, error) {
	return e.waitPool(ctx, start, hashes, timeout, true)
}

// WaitGone waits until no hash is in the pool.
func (e *Env) WaitGone(ctx context.Context, start time.Time, hashes []common.Hash,
	timeout time.Duration,
) (time.Duration, error) {
	return e.waitPool(ctx, start, hashes, timeout, false)
}

func (e *Env) waitPool(ctx context.Context, start time.Time, hashes []common.Hash,
	timeout time.Duration, want bool,
) (time.Duration, error) {
	if len(hashes) == 0 {
		return 0, nil
	}

	remaining := append([]common.Hash(nil), hashes...)

	return observer.WaitFor(ctx, start, timeout, pollEvery, func(ctx context.Context) (bool, error) {
		in, err := e.Pool.InPool(ctx, remaining)
		if err != nil {
			return false, err
		}

		next := remaining[:0]
		for _, h := range remaining {
			if in[h] != want {
				next = append(next, h)
			}
		}

		remaining = next

		return len(remaining) == 0, nil
	})
}

// WaitStatus waits until the pool status satisfies cond.
func (e *Env) WaitStatus(ctx context.Context, start time.Time, timeout time.Duration,
	cond func(observer.Status) bool,
) (time.Duration, error) {
	return observer.WaitFor(ctx, start, timeout, pollEvery, func(ctx context.Context) (bool, error) {
		st, err := e.Pool.Status(ctx)
		if err != nil {
			return false, err
		}

		return cond(st), nil
	})
}

// Head returns the head block.
func (e *Env) Head(ctx context.Context) (*cldriver.Block, error) {
	return e.Driver.Head(ctx)
}

// Build builds a payload on the current head.
func (e *Env) Build(ctx context.Context, opts cldriver.BuildOptions) (*cldriver.Payload, error) {
	head, err := e.Head(ctx)
	if err != nil {
		return nil, err
	}

	return e.BuildOn(ctx, head, opts)
}

// BuildOn builds a payload on parent and records the event.
func (e *Env) BuildOn(ctx context.Context, parent *cldriver.Block, opts cldriver.BuildOptions,
) (*cldriver.Payload, error) {
	p, err := e.Driver.Build(ctx, parent, opts)
	if err != nil {
		return nil, fmt.Errorf("building on block %d: %w", parent.Number, err)
	}

	e.Rec.Event("getPayload", "block %d: %d txs, %d gas", p.Block.Number, len(p.Txs),
		p.Envelope.ExecutionPayload.GasUsed)

	return p, nil
}

// BuildWith builds on parent until the payload contains every hash in want.
// A client may return a payload before it has added every pending tx, so the
// helper starts a new build (with a new salt) when one is missing.
func (e *Env) BuildWith(ctx context.Context, parent *cldriver.Block, want []common.Hash,
	opts cldriver.BuildOptions,
) (*cldriver.Payload, error) {
	const attempts = 10

	var p *cldriver.Payload

	for i := range attempts {
		o := opts
		o.Salt = opts.Salt*attempts + uint64(i)

		var err error

		p, err = e.BuildOn(ctx, parent, o)
		if err != nil {
			return nil, err
		}

		missing := 0

		for _, h := range want {
			if !p.Contains(h) {
				missing++
			}
		}

		if missing == 0 {
			return p, nil
		}

		e.Log.WithField("missing", missing).Debug("Payload lacks txs; building again")
	}

	return p, fmt.Errorf("payload on block %d lacks some of the %d txs after %d builds",
		parent.Number, len(want), attempts)
}

// Import imports a payload as the new head and records the events.
func (e *Env) Import(ctx context.Context, p *cldriver.Payload) (cldriver.ImportTiming, error) {
	t, err := e.Driver.Import(ctx, p)
	if err != nil {
		return t, fmt.Errorf("importing block %d: %w", p.Block.Number, err)
	}

	e.Rec.Event("newPayload", "block %d (%d txs) in %.2fms", p.Block.Number, len(p.Txs),
		Ms(t.NewPayload))
	e.Rec.Event("fcu", "head = block %d in %.2fms", p.Block.Number, Ms(t.Fcu))

	return t, nil
}

// Drain builds and imports blocks until the pool has no pending tx, or until
// maxBlocks. It returns the number of blocks.
func (e *Env) Drain(ctx context.Context, maxBlocks int) (int, error) {
	for i := range maxBlocks {
		st, err := e.Pool.Status(ctx)
		if err != nil {
			return i, err
		}

		if st.Pending == 0 {
			return i, nil
		}

		p, err := e.Build(ctx, cldriver.BuildOptions{})
		if err != nil {
			return i, err
		}

		if len(p.Txs) == 0 {
			// The pending count can lag behind a head change: the client
			// resets the pool in the background. Wait for it once.
			if _, err := e.WaitStatus(ctx, time.Now(), 5*time.Second, func(s observer.Status) bool {
				return s.Pending == 0
			}); err == nil {
				return i, nil
			}

			return i, fmt.Errorf("the pool has %d pending txs but the payload is empty",
				st.Pending)
		}

		if _, err := e.Import(ctx, p); err != nil {
			return i, err
		}

		// Let the pool reset after the head change.
		if _, err := e.WaitGone(ctx, time.Now(), p.TxHashes, 10*time.Second); err != nil {
			return i, fmt.Errorf("waiting for the included txs to leave the pool: %w", err)
		}
	}

	st, err := e.Pool.Status(ctx)
	if err != nil {
		return maxBlocks, err
	}

	if st.Pending != 0 {
		return maxBlocks, fmt.Errorf("pool still has %d pending txs after %d blocks",
			st.Pending, maxBlocks)
	}

	return maxBlocks, nil
}

// ContentFrom returns the pending and queued nonces of one sender, or
// observer.ErrUnsupported when the client cannot tell.
func (e *Env) ContentFrom(ctx context.Context, addr common.Address,
) (pending, queued map[uint64]common.Hash, err error) {
	return e.Pool.ContentFrom(ctx, addr)
}

// SendText returns "accepted" for a send without error, else the error.
func SendText(err error) string {
	if err == nil {
		return "accepted"
	}

	return ErrText(err)
}

// ErrText returns a short form of an error for an assertion ("ok" if nil).
func ErrText(err error) string {
	if err == nil {
		return "ok"
	}

	var rpcErr rpc.Error
	if errors.As(err, &rpcErr) {
		return strings.TrimSpace(rpcErr.Error())
	}

	return strings.TrimSpace(err.Error())
}

// buildFull builds with the default payload delay.
var buildFull = cldriver.BuildOptions{}

func u256Of(v *big.Int) *uint256.Int {
	u, _ := uint256.FromBig(v)

	return u
}

// Code returns the code of an account (eth_getCode at latest).
func (e *Env) Code(ctx context.Context, addr common.Address) ([]byte, error) {
	var code hexutil.Bytes
	if err := e.Pool.RPC().CallContext(ctx, &code, "eth_getCode", addr, "latest"); err != nil {
		return nil, fmt.Errorf("eth_getCode: %w", err)
	}

	return code, nil
}

// DelegatedTo returns the delegation target of an EIP-7702 delegated account,
// or the zero address when the account has no delegation.
func (e *Env) DelegatedTo(ctx context.Context, addr common.Address) (common.Address, error) {
	code, err := e.Code(ctx, addr)
	if err != nil {
		return common.Address{}, err
	}

	if len(code) == 23 && code[0] == 0xef && code[1] == 0x01 && code[2] == 0x00 {
		return common.BytesToAddress(code[3:]), nil
	}

	return common.Address{}, nil
}

// Balance returns the balance of an account at latest.
func (e *Env) Balance(ctx context.Context, addr common.Address) (*big.Int, error) {
	var b hexutil.Big
	if err := e.Pool.RPC().CallContext(ctx, &b, "eth_getBalance", addr, "latest"); err != nil {
		return nil, fmt.Errorf("eth_getBalance: %w", err)
	}

	return b.ToInt(), nil
}

// BlobBaseFee returns the blob base fee of the next block (eth_blobBaseFee).
func (e *Env) BlobBaseFee(ctx context.Context) (*big.Int, error) {
	var b hexutil.Big
	if err := e.Pool.RPC().CallContext(ctx, &b, "eth_blobBaseFee"); err != nil {
		return nil, fmt.Errorf("eth_blobBaseFee: %w", err)
	}

	return b.ToInt(), nil
}

// ImportNext builds a block on the head with opts, imports it, and returns it.
func (e *Env) ImportNext(ctx context.Context, opts cldriver.BuildOptions) (*cldriver.Payload, error) {
	p, err := e.Build(ctx, opts)
	if err != nil {
		return nil, err
	}

	if _, err := e.Import(ctx, p); err != nil {
		return nil, err
	}

	return p, nil
}
