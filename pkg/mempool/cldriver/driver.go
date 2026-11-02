// Package cldriver drives an execution client through the Engine API, as a
// consensus client would. It controls block production, time and fork choice,
// so a mempool scenario is deterministic: every payload it builds comes from
// the client's own pool (forkchoiceUpdated with attributes, then getPayload),
// never from a list of txs passed in.
package cldriver

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sirupsen/logrus"
)

// SlotSeconds is the time between two blocks the driver builds.
const SlotSeconds = 12

// Fork is one entry of the fork schedule, active from Time.
type Fork struct {
	Name string
	Time uint64
}

// Config configures the driver.
type Config struct {
	EngineURL string
	// JWTSecret is the hex-encoded Engine API secret.
	JWTSecret string
	// Forks is the fork schedule, sorted by Time. The driver picks the Engine
	// API method versions per payload timestamp.
	Forks []Fork
	// FeeRecipient is the suggestedFeeRecipient of built payloads.
	FeeRecipient common.Address
	// PayloadDelay is the wait between forkchoiceUpdated and getPayload.
	// Builders improve a payload over time, so a short delay gives a payload
	// with fewer txs.
	PayloadDelay time.Duration
}

// Block is the part of a header the scenarios need.
type Block struct {
	Hash      common.Hash
	Number    uint64
	Timestamp uint64
	BaseFee   *big.Int
	GasLimit  uint64
	GasUsed   uint64
}

// NextBaseFee returns the EIP-1559 base fee of a child of b (elasticity 2,
// change denominator 8).
func (b *Block) NextBaseFee() *big.Int {
	target := b.GasLimit / 2
	base := new(big.Int).Set(b.BaseFee)

	if target == 0 || b.GasUsed == target {
		return base
	}

	if b.GasUsed > target {
		delta := new(big.Int).Mul(base, new(big.Int).SetUint64(b.GasUsed-target))
		delta.Div(delta, new(big.Int).SetUint64(target))
		delta.Div(delta, big.NewInt(8))

		if delta.Sign() == 0 {
			delta.SetInt64(1)
		}

		return base.Add(base, delta)
	}

	delta := new(big.Int).Mul(base, new(big.Int).SetUint64(target-b.GasUsed))
	delta.Div(delta, new(big.Int).SetUint64(target))
	delta.Div(delta, big.NewInt(8))

	return base.Sub(base, delta)
}

// Payload is a built payload that is not imported yet.
type Payload struct {
	Envelope   *engine.ExecutionPayloadEnvelope
	BeaconRoot common.Hash
	Block      Block
	// TxHashes are the hashes of the payload txs, in block order.
	TxHashes []common.Hash
	// Txs are the decoded payload txs, in block order.
	Txs []*types.Transaction
	// FcuDuration and GetPayloadDuration time the two build calls.
	FcuDuration        time.Duration
	GetPayloadDuration time.Duration
}

// Contains returns true when the payload includes the tx.
func (p *Payload) Contains(hash common.Hash) bool {
	for _, h := range p.TxHashes {
		if h == hash {
			return true
		}
	}

	return false
}

// ImportTiming times the import of a payload.
type ImportTiming struct {
	NewPayload time.Duration
	Fcu        time.Duration
}

// BuildOptions changes how a payload is built.
type BuildOptions struct {
	// Delay overrides Config.PayloadDelay when not zero.
	Delay time.Duration
	// Timestamp overrides parent.Timestamp+SlotSeconds when not zero.
	Timestamp uint64
	// Salt makes the prevRandao and the beacon root differ between two
	// payloads built on the same parent (sibling chains).
	Salt uint64
}

// Driver acts as the consensus client of one execution client.
type Driver interface {
	Start(ctx context.Context) error
	Stop() error

	// Head returns the head block of the client.
	Head(ctx context.Context) (*Block, error)
	// BlockByHash returns a block of the client.
	BlockByHash(ctx context.Context, hash common.Hash) (*Block, error)
	// ForkchoiceUpdated sets the head (and safe/finalized to zero). With
	// attrs it also starts a payload build.
	ForkchoiceUpdated(ctx context.Context, head common.Hash, attrs *engine.PayloadAttributes,
	) (*engine.ForkChoiceResponse, time.Duration, error)
	// Build starts a payload build on parent, waits, and gets the payload.
	// The head of the client must be parent.
	Build(ctx context.Context, parent *Block, opts BuildOptions) (*Payload, error)
	// GetPayload fetches the current version of a payload that is building.
	GetPayload(ctx context.Context, id engine.PayloadID, timestamp uint64,
	) (*engine.ExecutionPayloadEnvelope, time.Duration, error)
	// NewPayload sends the payload to the client without a head change.
	NewPayload(ctx context.Context, p *Payload) (time.Duration, error)
	// Import sends the payload and makes it the head.
	Import(ctx context.Context, p *Payload) (ImportTiming, error)
	// Attributes returns the payload attributes for a child of parent.
	Attributes(parent *Block, opts BuildOptions) *engine.PayloadAttributes
}

type driver struct {
	log    logrus.FieldLogger
	cfg    Config
	client *rpc.Client
	// builds makes the attributes of every build unique. A client returns
	// the cached payload for attributes it has seen, and a resolved payload
	// does not take new txs.
	builds atomic.Uint64
}

// Ensure interface compliance.
var _ Driver = (*driver)(nil)

// New returns a driver. Start dials the client.
func New(log logrus.FieldLogger, cfg Config) Driver {
	return &driver{
		log: log.WithField("package", "cldriver"),
		cfg: cfg,
	}
}

// Start dials the Engine API endpoint.
func (d *driver) Start(ctx context.Context) error {
	if len(d.cfg.Forks) == 0 {
		return errors.New("fork schedule is empty")
	}

	secret, err := hex.DecodeString(strings.TrimPrefix(d.cfg.JWTSecret, "0x"))
	if err != nil {
		return fmt.Errorf("decoding the JWT secret: %w", err)
	}

	client, err := rpc.DialOptions(ctx, d.cfg.EngineURL, rpc.WithHTTPAuth(
		func(h http.Header) error {
			h.Set("Authorization", "Bearer "+jwtToken(secret, time.Now()))

			return nil
		},
	))
	if err != nil {
		return fmt.Errorf("dialing the engine API: %w", err)
	}

	d.client = client

	return nil
}

// Stop closes the connection.
func (d *driver) Stop() error {
	if d.client != nil {
		d.client.Close()
	}

	return nil
}

type rpcHeader struct {
	Hash      common.Hash    `json:"hash"`
	Number    hexutil.Uint64 `json:"number"`
	Timestamp hexutil.Uint64 `json:"timestamp"`
	BaseFee   *hexutil.Big   `json:"baseFeePerGas"`
	GasLimit  hexutil.Uint64 `json:"gasLimit"`
	GasUsed   hexutil.Uint64 `json:"gasUsed"`
}

func (h *rpcHeader) block() *Block {
	b := &Block{
		Hash:      h.Hash,
		Number:    uint64(h.Number),
		Timestamp: uint64(h.Timestamp),
		GasLimit:  uint64(h.GasLimit),
		GasUsed:   uint64(h.GasUsed),
		BaseFee:   new(big.Int),
	}

	if h.BaseFee != nil {
		b.BaseFee = h.BaseFee.ToInt()
	}

	return b
}

// Head returns the head block. The engine port also serves eth_*.
func (d *driver) Head(ctx context.Context) (*Block, error) {
	var h *rpcHeader
	if err := d.client.CallContext(ctx, &h, "eth_getBlockByNumber", "latest", false); err != nil {
		return nil, fmt.Errorf("eth_getBlockByNumber: %w", err)
	}

	if h == nil {
		return nil, errors.New("no head block")
	}

	return h.block(), nil
}

// BlockByHash returns one block.
func (d *driver) BlockByHash(ctx context.Context, hash common.Hash) (*Block, error) {
	var h *rpcHeader
	if err := d.client.CallContext(ctx, &h, "eth_getBlockByHash", hash, false); err != nil {
		return nil, fmt.Errorf("eth_getBlockByHash: %w", err)
	}

	if h == nil {
		return nil, fmt.Errorf("block %s not found", hash)
	}

	return h.block(), nil
}

// methods are the Engine API method names for one fork.
type methods struct {
	fcu        string
	getPayload string
	newPayload string
}

var forkMethods = map[string]methods{
	"prague": {"engine_forkchoiceUpdatedV3", "engine_getPayloadV4", "engine_newPayloadV4"},
	"osaka":  {"engine_forkchoiceUpdatedV3", "engine_getPayloadV5", "engine_newPayloadV4"},
}

func (d *driver) methodsAt(timestamp uint64) (methods, error) {
	name := d.cfg.Forks[0].Name

	for _, f := range d.cfg.Forks {
		if timestamp >= f.Time {
			name = f.Name
		}
	}

	m, ok := forkMethods[name]
	if !ok {
		return methods{}, fmt.Errorf("fork %q is not supported by the driver", name)
	}

	return m, nil
}

// Attributes returns the payload attributes of a child of parent.
func (d *driver) Attributes(parent *Block, opts BuildOptions) *engine.PayloadAttributes {
	ts := opts.Timestamp
	if ts == 0 {
		ts = parent.Timestamp + SlotSeconds
	}

	var seed [32]byte

	binary.BigEndian.PutUint64(seed[0:], parent.Number+1)
	binary.BigEndian.PutUint64(seed[8:], ts)
	binary.BigEndian.PutUint64(seed[16:], opts.Salt)
	binary.BigEndian.PutUint64(seed[24:], d.builds.Add(1))

	beaconRoot := crypto.Keccak256Hash([]byte("beacon"), seed[:])

	return &engine.PayloadAttributes{
		Timestamp:             ts,
		Random:                crypto.Keccak256Hash([]byte("randao"), seed[:]),
		SuggestedFeeRecipient: d.cfg.FeeRecipient,
		Withdrawals:           []*types.Withdrawal{},
		BeaconRoot:            &beaconRoot,
	}
}

// ForkchoiceUpdated calls engine_forkchoiceUpdated.
func (d *driver) ForkchoiceUpdated(
	ctx context.Context, head common.Hash, attrs *engine.PayloadAttributes,
) (*engine.ForkChoiceResponse, time.Duration, error) {
	ts := uint64(0)
	if attrs != nil {
		ts = attrs.Timestamp
	} else {
		ts = d.cfg.Forks[len(d.cfg.Forks)-1].Time
	}

	m, err := d.methodsAt(ts)
	if err != nil {
		return nil, 0, err
	}

	state := engine.ForkchoiceStateV1{HeadBlockHash: head}

	var resp engine.ForkChoiceResponse

	start := time.Now()
	err = d.client.CallContext(ctx, &resp, m.fcu, state, attrs)
	took := time.Since(start)

	if err != nil {
		return nil, took, fmt.Errorf("%s: %w", m.fcu, err)
	}

	if resp.PayloadStatus.Status != engine.VALID {
		return &resp, took, fmt.Errorf("%s: status %s (%s)", m.fcu,
			resp.PayloadStatus.Status, deref(resp.PayloadStatus.ValidationError))
	}

	return &resp, took, nil
}

// GetPayload calls engine_getPayload.
func (d *driver) GetPayload(
	ctx context.Context, id engine.PayloadID, timestamp uint64,
) (*engine.ExecutionPayloadEnvelope, time.Duration, error) {
	m, err := d.methodsAt(timestamp)
	if err != nil {
		return nil, 0, err
	}

	var env engine.ExecutionPayloadEnvelope

	start := time.Now()
	err = d.client.CallContext(ctx, &env, m.getPayload, id)
	took := time.Since(start)

	if err != nil {
		return nil, took, fmt.Errorf("%s: %w", m.getPayload, err)
	}

	return &env, took, nil
}

// Build starts a build on parent and returns the payload after the delay.
func (d *driver) Build(ctx context.Context, parent *Block, opts BuildOptions) (*Payload, error) {
	attrs := d.Attributes(parent, opts)

	resp, fcuTook, err := d.ForkchoiceUpdated(ctx, parent.Hash, attrs)
	if err != nil {
		return nil, err
	}

	if resp.PayloadID == nil {
		return nil, fmt.Errorf("no payload id: the client did not build on %s", parent.Hash)
	}

	delay := opts.Delay
	if delay == 0 {
		delay = d.cfg.PayloadDelay
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(delay):
	}

	env, getTook, err := d.GetPayload(ctx, *resp.PayloadID, attrs.Timestamp)
	if err != nil {
		return nil, err
	}

	p, err := newPayload(env, *attrs.BeaconRoot)
	if err != nil {
		return nil, err
	}

	p.FcuDuration = fcuTook
	p.GetPayloadDuration = getTook

	return p, nil
}

// NewPayloadFromEnvelope wraps an envelope from GetPayload.
func NewPayloadFromEnvelope(env *engine.ExecutionPayloadEnvelope, beaconRoot common.Hash,
) (*Payload, error) {
	return newPayload(env, beaconRoot)
}

func newPayload(env *engine.ExecutionPayloadEnvelope, beaconRoot common.Hash) (*Payload, error) {
	ed := env.ExecutionPayload
	if ed == nil {
		return nil, errors.New("payload envelope has no execution payload")
	}

	p := &Payload{
		Envelope:   env,
		BeaconRoot: beaconRoot,
		Block: Block{
			Hash:      ed.BlockHash,
			Number:    ed.Number,
			Timestamp: ed.Timestamp,
			BaseFee:   ed.BaseFeePerGas,
			GasLimit:  ed.GasLimit,
			GasUsed:   ed.GasUsed,
		},
		TxHashes: make([]common.Hash, 0, len(ed.Transactions)),
		Txs:      make([]*types.Transaction, 0, len(ed.Transactions)),
	}

	for i, raw := range ed.Transactions {
		var tx types.Transaction
		if err := tx.UnmarshalBinary(raw); err != nil {
			return nil, fmt.Errorf("decoding payload tx %d: %w", i, err)
		}

		p.Txs = append(p.Txs, &tx)
		p.TxHashes = append(p.TxHashes, tx.Hash())
	}

	return p, nil
}

// NewPayload calls engine_newPayload.
func (d *driver) NewPayload(ctx context.Context, p *Payload) (time.Duration, error) {
	m, err := d.methodsAt(p.Block.Timestamp)
	if err != nil {
		return 0, err
	}

	versionedHashes := make([]common.Hash, 0, len(p.Txs))
	for _, tx := range p.Txs {
		versionedHashes = append(versionedHashes, tx.BlobHashes()...)
	}

	requests := make([]hexutil.Bytes, 0, len(p.Envelope.Requests))
	for _, r := range p.Envelope.Requests {
		requests = append(requests, r)
	}

	var status engine.PayloadStatusV1

	start := time.Now()
	err = d.client.CallContext(ctx, &status, m.newPayload,
		p.Envelope.ExecutionPayload, versionedHashes, p.BeaconRoot, requests)
	took := time.Since(start)

	if err != nil {
		return took, fmt.Errorf("%s: %w", m.newPayload, err)
	}

	if status.Status != engine.VALID {
		return took, fmt.Errorf("%s: status %s (%s)", m.newPayload, status.Status,
			deref(status.ValidationError))
	}

	return took, nil
}

// Import sends the payload and makes it the head.
func (d *driver) Import(ctx context.Context, p *Payload) (ImportTiming, error) {
	var t ImportTiming

	took, err := d.NewPayload(ctx, p)
	t.NewPayload = took

	if err != nil {
		return t, err
	}

	_, took, err = d.ForkchoiceUpdated(ctx, p.Block.Hash, nil)
	t.Fcu = took

	return t, err
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

// jwtToken returns an Engine API JWT (HS256 with an iat claim).
func jwtToken(secret []byte, now time.Time) string {
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

	claims, _ := json.Marshal(map[string]int64{"iat": now.Unix()})
	msg := header + "." + enc.EncodeToString(claims)

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(msg))

	return msg + "." + enc.EncodeToString(mac.Sum(nil))
}
