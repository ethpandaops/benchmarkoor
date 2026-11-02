// Package observer watches the pool of an execution client over JSON-RPC. It
// polls the pool size into a timeline, and answers "is this tx in the pool"
// with a timestamp, so scenarios can time pool state changes.
//
// The observer uses txpool_status, txpool_content, txpool_contentFrom and
// eth_getTransactionByHash. It probes them at Start. Besu has none of the
// txpool_* methods above; the observer then uses txpool_besuStatistics and
// txpool_besuTransactions, which do not tell pending from queued.
package observer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sirupsen/logrus"
)

// Status is the pool size.
type Status struct {
	Pending int
	Queued  int
}

// Content is the set of txs in the pool by state. Blob txs are not listed by
// every client (geth leaves them out); use InPool for those.
type Content struct {
	Pending map[common.Hash]TxRef
	Queued  map[common.Hash]TxRef
}

// TxRef locates a tx in the pool.
type TxRef struct {
	From  common.Address
	Nonce uint64
}

// Has returns true when the hash is pending or queued.
func (c *Content) Has(h common.Hash) bool {
	_, p := c.Pending[h]
	_, q := c.Queued[h]

	return p || q
}

// ErrUnsupported is returned for a query the client cannot answer.
var ErrUnsupported = errors.New("not supported by this client")

// Caps tells which pool views the client gives.
type Caps struct {
	// API is "txpool" (txpool_status/content) or "besu".
	API string
	// QueuedVisible is true when the client tells pending from queued.
	QueuedVisible bool
	// ContentFrom is true when txpool_contentFrom works.
	ContentFrom bool
}

// Sample is one timeline point.
type Sample struct {
	At          time.Time
	Status      Status
	MemoryBytes uint64
	// CPUPercent is the container CPU use; 100 is one full core.
	CPUPercent float64
}

// Resources is the container resource use at one moment.
type Resources struct {
	MemoryBytes uint64
	CPUPercent  float64
}

// ResourceFunc returns the latest container resource use. ok is false when
// it is not known.
type ResourceFunc func() (r Resources, ok bool)

// Observer polls one client.
type Observer interface {
	Start(ctx context.Context) error
	Stop() error

	// Caps returns the pool views the client gives (probed at Start).
	Caps() Caps
	// Status returns the pool size (txpool_status, or the besu equivalent).
	Status(ctx context.Context) (Status, error)
	// ContentFrom returns the pending and queued txs of one sender by nonce,
	// or ErrUnsupported.
	ContentFrom(ctx context.Context, addr common.Address) (pending, queued map[uint64]common.Hash, err error)
	// Content calls txpool_content.
	Content(ctx context.Context) (*Content, error)
	// InPool returns, per hash, true when the tx is in the pool
	// (txpool_content, with a fallback for txs it does not list).
	InPool(ctx context.Context, hashes []common.Hash) (map[common.Hash]bool, error)
	// SendRaw calls eth_sendRawTransaction and returns the call time.
	SendRaw(ctx context.Context, raw []byte) (time.Duration, error)
	// SendRawBatch sends txs in one JSON-RPC batch. errs[i] is the error of
	// txs[i] (nil when accepted).
	SendRawBatch(ctx context.Context, raws [][]byte) ([]error, time.Duration, error)
	// Samples returns the timeline recorded since Start (or the last Reset).
	Samples() []Sample
	// Reset clears the timeline.
	Reset()
	// RPC returns the underlying client for other calls.
	RPC() *rpc.Client
}

// Config configures the observer.
type Config struct {
	RPCURL string
	// Interval between two timeline samples.
	Interval time.Duration
	// Resources returns the container memory and CPU use for each sample.
	// Optional.
	Resources ResourceFunc
}

type observer struct {
	log    logrus.FieldLogger
	cfg    Config
	client *rpc.Client

	caps Caps

	mu      sync.Mutex
	samples []Sample

	done chan struct{}
	wg   sync.WaitGroup
}

// Ensure interface compliance.
var _ Observer = (*observer)(nil)

// New returns an observer. Start dials the client and starts the sampler.
func New(log logrus.FieldLogger, cfg Config) Observer {
	if cfg.Interval == 0 {
		cfg.Interval = 100 * time.Millisecond
	}

	return &observer{
		log:     log.WithField("package", "observer"),
		cfg:     cfg,
		samples: make([]Sample, 0, 1024),
		done:    make(chan struct{}),
	}
}

func (o *observer) Start(ctx context.Context) error {
	client, err := rpc.DialContext(ctx, o.cfg.RPCURL)
	if err != nil {
		return fmt.Errorf("dialing %s: %w", o.cfg.RPCURL, err)
	}

	o.client = client

	if err := o.probe(ctx); err != nil {
		return err
	}

	o.log.WithFields(logrus.Fields{
		"api": o.caps.API, "queued_visible": o.caps.QueuedVisible, "content_from": o.caps.ContentFrom,
	}).Info("Pool observer ready")

	o.wg.Add(1)

	go o.sample(ctx)

	return nil
}

func (o *observer) Stop() error {
	close(o.done)
	o.wg.Wait()

	if o.client != nil {
		o.client.Close()
	}

	return nil
}

func (o *observer) RPC() *rpc.Client { return o.client }

func (o *observer) Caps() Caps { return o.caps }

// probe picks the pool API of the client.
func (o *observer) probe(ctx context.Context) error {
	o.caps = Caps{API: "txpool", QueuedVisible: true}
	if _, err := o.Status(ctx); err == nil {
		var dummy json.RawMessage

		err := o.client.CallContext(ctx, &dummy, "txpool_contentFrom", common.Address{})
		o.caps.ContentFrom = err == nil

		return nil
	}

	o.caps = Caps{API: "besu"}
	if _, err := o.Status(ctx); err != nil {
		return fmt.Errorf("the client serves neither txpool_status nor txpool_besuStatistics: %w", err)
	}

	return nil
}

func (o *observer) sample(ctx context.Context) {
	defer o.wg.Done()

	ticker := time.NewTicker(o.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-o.done:
			return
		case <-ticker.C:
		}

		callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		st, err := o.Status(callCtx)

		cancel()

		if err != nil {
			o.log.WithError(err).Debug("Pool sample failed")

			continue
		}

		s := Sample{At: time.Now(), Status: st}
		if o.cfg.Resources != nil {
			if r, ok := o.cfg.Resources(); ok {
				s.MemoryBytes, s.CPUPercent = r.MemoryBytes, r.CPUPercent
			}
		}

		o.mu.Lock()
		o.samples = append(o.samples, s)
		o.mu.Unlock()
	}
}

func (o *observer) Samples() []Sample {
	o.mu.Lock()
	defer o.mu.Unlock()

	out := make([]Sample, len(o.samples))
	copy(out, o.samples)

	return out
}

func (o *observer) Reset() {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.samples = o.samples[:0]
}

func (o *observer) Status(ctx context.Context) (Status, error) {
	if o.caps.API == "besu" {
		var res struct {
			LocalCount  flexUint `json:"localCount"`
			RemoteCount flexUint `json:"remoteCount"`
		}

		if err := o.client.CallContext(ctx, &res, "txpool_besuStatistics"); err != nil {
			return Status{}, fmt.Errorf("txpool_besuStatistics: %w", err)
		}

		// Besu does not tell pending from queued: all count as pending.
		return Status{Pending: int(res.LocalCount + res.RemoteCount)}, nil
	}

	var res struct {
		Pending flexUint `json:"pending"`
		Queued  flexUint `json:"queued"`
	}

	if err := o.client.CallContext(ctx, &res, "txpool_status"); err != nil {
		return Status{}, fmt.Errorf("txpool_status: %w", err)
	}

	return Status{Pending: int(res.Pending), Queued: int(res.Queued)}, nil
}

// flexUint reads a quantity written as a hex string, a decimal string or a
// JSON number: clients differ.
type flexUint uint64

func (f *flexUint) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		v, err := parseNonce(s)
		if err != nil {
			return err
		}

		*f = flexUint(v)

		return nil
	}

	var n uint64
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf("quantity %s: %w", b, err)
	}

	*f = flexUint(n)

	return nil
}

type rpcPoolTx struct {
	Hash common.Hash `json:"hash"`
}

func (o *observer) Content(ctx context.Context) (*Content, error) {
	if o.caps.API == "besu" {
		var txs []rpcPoolTx
		if err := o.client.CallContext(ctx, &txs, "txpool_besuTransactions"); err != nil {
			return nil, fmt.Errorf("txpool_besuTransactions: %w", err)
		}

		c := &Content{
			Pending: make(map[common.Hash]TxRef, len(txs)),
			Queued:  make(map[common.Hash]TxRef),
		}
		for _, tx := range txs {
			c.Pending[tx.Hash] = TxRef{}
		}

		return c, nil
	}

	var res struct {
		Pending map[common.Address]map[string]rpcPoolTx `json:"pending"`
		Queued  map[common.Address]map[string]rpcPoolTx `json:"queued"`
	}

	if err := o.client.CallContext(ctx, &res, "txpool_content"); err != nil {
		return nil, fmt.Errorf("txpool_content: %w", err)
	}

	c := &Content{
		Pending: make(map[common.Hash]TxRef, len(res.Pending)*4),
		Queued:  make(map[common.Hash]TxRef, len(res.Queued)*4),
	}

	fill := func(dst map[common.Hash]TxRef, src map[common.Address]map[string]rpcPoolTx) error {
		for from, byNonce := range src {
			for nonceStr, tx := range byNonce {
				nonce, err := parseNonce(nonceStr)
				if err != nil {
					return err
				}

				dst[tx.Hash] = TxRef{From: from, Nonce: nonce}
			}
		}

		return nil
	}

	if err := fill(c.Pending, res.Pending); err != nil {
		return nil, err
	}

	if err := fill(c.Queued, res.Queued); err != nil {
		return nil, err
	}

	return c, nil
}

func parseNonce(s string) (uint64, error) {
	if strings.HasPrefix(s, "0x") {
		v, err := hexutil.DecodeUint64(s)
		if err != nil {
			return 0, fmt.Errorf("pool nonce %q: %w", s, err)
		}

		return v, nil
	}

	var v uint64
	if _, err := fmt.Sscan(s, &v); err != nil {
		return 0, fmt.Errorf("pool nonce %q: %w", s, err)
	}

	return v, nil
}

func (o *observer) ContentFrom(ctx context.Context, addr common.Address,
) (pending, queued map[uint64]common.Hash, err error) {
	if !o.caps.ContentFrom {
		return nil, nil, ErrUnsupported
	}

	var res struct {
		Pending map[string]rpcPoolTx `json:"pending"`
		Queued  map[string]rpcPoolTx `json:"queued"`
	}

	if err := o.client.CallContext(ctx, &res, "txpool_contentFrom", addr); err != nil {
		return nil, nil, fmt.Errorf("txpool_contentFrom: %w", err)
	}

	conv := func(src map[string]rpcPoolTx) (map[uint64]common.Hash, error) {
		out := make(map[uint64]common.Hash, len(src))

		for k, v := range src {
			n, err := parseNonce(k)
			if err != nil {
				return nil, err
			}

			out[n] = v.Hash
		}

		return out, nil
	}

	if pending, err = conv(res.Pending); err != nil {
		return nil, nil, err
	}

	if queued, err = conv(res.Queued); err != nil {
		return nil, nil, err
	}

	return pending, queued, nil
}

type rpcTx struct {
	BlockHash *common.Hash `json:"blockHash"`
}

// BatchSize is the most calls in one JSON-RPC batch. Erigon refuses batches
// above 100 by default (--rpc.batch.limit).
const BatchSize = 100

// InPool reads the pool itself (txpool_content), not the chain: after a head
// change, a mined tx can stay in the pool until the client resets the pool in
// the background, and eth_getTransactionByHash finds it in the chain at once.
// A hash that txpool_content does not list (geth leaves blob txs out) falls
// back to eth_getTransactionByHash: known and not mined means in the pool.
// So a mined blob tx counts as gone even if the pool still holds it.
func (o *observer) InPool(ctx context.Context, hashes []common.Hash) (map[common.Hash]bool, error) {
	out := make(map[common.Hash]bool, len(hashes))

	content, err := o.Content(ctx)
	if err != nil {
		return nil, err
	}

	rest := make([]common.Hash, 0, len(hashes))

	for _, h := range hashes {
		if content.Has(h) {
			out[h] = true
		} else {
			rest = append(rest, h)
		}
	}

	known, err := o.knownUnmined(ctx, rest)
	if err != nil {
		return nil, err
	}

	for _, h := range rest {
		out[h] = known[h]
	}

	return out, nil
}

// knownUnmined returns, per hash, true when the client knows the tx and it is
// not in a block (eth_getTransactionByHash, batched).
func (o *observer) knownUnmined(ctx context.Context, hashes []common.Hash) (map[common.Hash]bool, error) {
	out := make(map[common.Hash]bool, len(hashes))

	for start := 0; start < len(hashes); start += BatchSize {
		end := min(start+BatchSize, len(hashes))
		chunk := hashes[start:end]

		results := make([]*rpcTx, len(chunk))
		batch := make([]rpc.BatchElem, len(chunk))

		for i, h := range chunk {
			batch[i] = rpc.BatchElem{
				Method: "eth_getTransactionByHash",
				Args:   []any{h},
				Result: &results[i],
			}
		}

		if err := o.client.BatchCallContext(ctx, batch); err != nil {
			return nil, fmt.Errorf("eth_getTransactionByHash batch: %w", err)
		}

		for i, h := range chunk {
			// Geth answers an unknown hash with this error while its tx
			// indexer runs (always on a fresh chain): the tx is not known.
			if batch[i].Error != nil && strings.Contains(batch[i].Error.Error(), "indexing is in progress") {
				out[h] = false

				continue
			}

			if batch[i].Error != nil {
				return nil, fmt.Errorf("eth_getTransactionByHash %s: %w", h, batch[i].Error)
			}

			out[h] = results[i] != nil && results[i].BlockHash == nil
		}
	}

	return out, nil
}

func (o *observer) SendRaw(ctx context.Context, raw []byte) (time.Duration, error) {
	var hash common.Hash

	start := time.Now()
	err := o.client.CallContext(ctx, &hash, "eth_sendRawTransaction", hexutil.Bytes(raw))

	return time.Since(start), err
}

func (o *observer) SendRawBatch(ctx context.Context, raws [][]byte) ([]error, time.Duration, error) {
	batch := make([]rpc.BatchElem, len(raws))
	hashes := make([]common.Hash, len(raws))

	for i, raw := range raws {
		batch[i] = rpc.BatchElem{
			Method: "eth_sendRawTransaction",
			Args:   []any{hexutil.Bytes(raw)},
			Result: &hashes[i],
		}
	}

	start := time.Now()
	err := o.client.BatchCallContext(ctx, batch)
	took := time.Since(start)

	if err != nil {
		return nil, took, fmt.Errorf("eth_sendRawTransaction batch: %w", err)
	}

	errs := make([]error, len(raws))
	for i := range batch {
		errs[i] = batch[i].Error
	}

	return errs, took, nil
}

// ErrTimeout is returned by WaitFor when the condition does not hold in time.
var ErrTimeout = errors.New("timeout")

// WaitFor polls cond until it returns true. It returns the time from start
// until the first poll that saw the condition hold.
func WaitFor(ctx context.Context, start time.Time, timeout, every time.Duration,
	cond func(ctx context.Context) (bool, error),
) (time.Duration, error) {
	deadline := time.Now().Add(timeout)

	for {
		ok, err := cond(ctx)
		if err != nil {
			return 0, err
		}

		if ok {
			return time.Since(start), nil
		}

		if time.Now().After(deadline) {
			return time.Since(start), ErrTimeout
		}

		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(every):
		}
	}
}
