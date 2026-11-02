// Package txfactory makes signed transactions for mempool scenarios. It owns
// the pre-funded keys (derived from a seed, so the genesis can fund them),
// tracks their nonces, and builds every tx type: legacy, access list, dynamic
// fee, blob (with cell proofs) and set-code (EIP-7702), plus invalid variants.
package txfactory

import (
	"crypto/ecdsa"
	"encoding/binary"
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/holiman/uint256"
)

// Gwei is 1e9 wei.
var Gwei = big.NewInt(1_000_000_000)

// GweiN returns n gwei.
func GweiN(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), Gwei)
}

// Account is one pre-funded key.
type Account struct {
	Index   int
	Key     *ecdsa.PrivateKey
	Address common.Address

	mu    sync.Mutex
	nonce uint64
}

// Nonce returns the next nonce the factory will use.
func (a *Account) Nonce() uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.nonce
}

// SetNonce sets the next nonce.
func (a *Account) SetNonce(n uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.nonce = n
}

// takeNonce returns the next nonce and moves it forward.
func (a *Account) takeNonce() uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()

	n := a.nonce
	a.nonce++

	return n
}

// DeriveKey returns the private key of account i for a seed. The derivation
// is fixed, so that the genesis and the factory agree on the addresses.
func DeriveKey(seed string, i int) *ecdsa.PrivateKey {
	var idx [8]byte

	binary.BigEndian.PutUint64(idx[:], uint64(i))

	for attempt := byte(0); ; attempt++ {
		h := crypto.Keccak256([]byte("benchmarkoor-mempool"), []byte(seed), idx[:], []byte{attempt})

		key, err := crypto.ToECDSA(h)
		if err == nil {
			return key
		}
	}
}

// Addresses returns the addresses of the first n accounts of a seed.
func Addresses(seed string, n int) []common.Address {
	out := make([]common.Address, 0, n)
	for i := range n {
		out = append(out, crypto.PubkeyToAddress(DeriveKey(seed, i).PublicKey))
	}

	return out
}

// Factory makes and signs txs.
type Factory interface {
	// ChainID returns the chain id the factory signs for.
	ChainID() *big.Int
	// Accounts returns the accounts [from, from+n). Each scenario uses its
	// own range, so scenarios never share a sender.
	Accounts(from, n int) ([]*Account, error)
	// Signer returns the signer for the chain.
	Signer() types.Signer

	// Transfer makes a signed value transfer of the given type with the next
	// nonce of from.
	Transfer(from *Account, kind Kind, fees Fees) (*types.Transaction, error)
	// Build makes a signed tx from a template. It does not change the nonce
	// of from unless Template.Nonce is nil.
	Build(from *Account, t Template) (*types.Transaction, error)
	// Sign signs a tx body with the key of from.
	Sign(from *Account, inner types.TxData) (*types.Transaction, error)
}

// Kind is a tx type.
type Kind string

const (
	KindLegacy     Kind = "legacy"
	KindAccessList Kind = "access-list"
	KindDynamicFee Kind = "dynamic-fee"
	KindBlob       Kind = "blob"
	KindSetCode    Kind = "set-code"
)

// AllKinds lists every tx type, in type order.
var AllKinds = []Kind{KindLegacy, KindAccessList, KindDynamicFee, KindBlob, KindSetCode}

// Fees are the fee fields. For a legacy or access-list tx the gas price is
// FeeCap.
type Fees struct {
	FeeCap     *big.Int
	TipCap     *big.Int
	BlobFeeCap *big.Int
}

// DefaultFees are the fees the scenarios use when they do not care: high
// enough for any base fee the scenarios reach.
func DefaultFees() Fees {
	return Fees{FeeCap: GweiN(20), TipCap: GweiN(1), BlobFeeCap: GweiN(10)}
}

// Bump returns the fees raised by pct percent (rounded up).
func (f Fees) Bump(pct int64) Fees {
	bump := func(v *big.Int) *big.Int {
		if v == nil {
			return nil
		}

		n := new(big.Int).Mul(v, big.NewInt(100+pct))
		n.Add(n, big.NewInt(99))

		return n.Div(n, big.NewInt(100))
	}

	return Fees{FeeCap: bump(f.FeeCap), TipCap: bump(f.TipCap), BlobFeeCap: bump(f.BlobFeeCap)}
}

// Template is a tx description. Zero fields take a default.
type Template struct {
	Kind  Kind
	Nonce *uint64
	To    *common.Address // nil for a contract creation (not blob or set-code)
	Value *big.Int
	Gas   uint64
	Data  []byte
	Fees  Fees
	// ChainID overrides the factory chain id (to make a wrong-chain tx).
	ChainID *big.Int
	// Blobs is the number of blobs of a blob tx (default 1).
	Blobs int
	// SidecarVersion is the blob sidecar version (default 1, cell proofs).
	SidecarVersion *byte
	// AuthCount is the number of authorizations of a set-code tx
	// (default 1). Zero with NoAuth set gives an empty list.
	AuthCount int
	NoAuth    bool
}

type factory struct {
	chainID *big.Int
	signer  types.Signer
	seed    string
	total   int

	mu       sync.Mutex
	accounts map[int]*Account

	blobMu    sync.Mutex
	blobCache map[blobKey]*types.BlobTxSidecar
}

type blobKey struct {
	n       int
	version byte
	salt    uint64
}

// Ensure interface compliance.
var _ Factory = (*factory)(nil)

// New returns a factory for total accounts derived from seed.
func New(chainID uint64, seed string, total int) Factory {
	id := new(big.Int).SetUint64(chainID)

	return &factory{
		chainID:   id,
		signer:    types.LatestSignerForChainID(id),
		seed:      seed,
		total:     total,
		accounts:  make(map[int]*Account, 256),
		blobCache: make(map[blobKey]*types.BlobTxSidecar, 16),
	}
}

func (f *factory) ChainID() *big.Int    { return new(big.Int).Set(f.chainID) }
func (f *factory) Signer() types.Signer { return f.signer }

func (f *factory) Accounts(from, n int) ([]*Account, error) {
	if from < 0 || n < 0 || from+n > f.total {
		return nil, fmt.Errorf("accounts [%d, %d) out of range: the genesis funds %d accounts",
			from, from+n, f.total)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]*Account, 0, n)

	for i := from; i < from+n; i++ {
		a, ok := f.accounts[i]
		if !ok {
			key := DeriveKey(f.seed, i)
			a = &Account{Index: i, Key: key, Address: crypto.PubkeyToAddress(key.PublicKey)}
			f.accounts[i] = a
		}

		out = append(out, a)
	}

	return out, nil
}

// sink is the receiver of transfers: an address that is not a sender.
var sink = common.HexToAddress("0x000000000000000000000000000000000000dEaD")

func (f *factory) Transfer(from *Account, kind Kind, fees Fees) (*types.Transaction, error) {
	return f.Build(from, Template{Kind: kind, Fees: fees})
}

func (f *factory) Sign(from *Account, inner types.TxData) (*types.Transaction, error) {
	tx, err := types.SignNewTx(from.Key, f.signer, inner)
	if err != nil {
		return nil, fmt.Errorf("signing: %w", err)
	}

	return tx, nil
}

func (f *factory) Build(from *Account, t Template) (*types.Transaction, error) {
	var nonce uint64
	if t.Nonce != nil {
		nonce = *t.Nonce
	} else {
		nonce = from.takeNonce()
	}

	fees := t.Fees
	if fees.FeeCap == nil {
		fees = DefaultFees()
	}

	to := t.To
	if to == nil && (t.Kind == KindBlob || t.Kind == KindSetCode || t.Data == nil) {
		to = &sink
	}

	value := t.Value
	if value == nil {
		value = big.NewInt(1)
	}

	chainID := f.chainID
	if t.ChainID != nil {
		chainID = t.ChainID
	}

	gas := t.Gas
	if gas == 0 {
		gas = intrinsicGuess(t)
	}

	signer := types.LatestSignerForChainID(chainID)

	var inner types.TxData

	switch t.Kind {
	case KindLegacy, "":
		inner = &types.LegacyTx{
			Nonce: nonce, GasPrice: fees.FeeCap, Gas: gas, To: to, Value: value, Data: t.Data,
		}
	case KindAccessList:
		inner = &types.AccessListTx{
			ChainID: chainID, Nonce: nonce, GasPrice: fees.FeeCap, Gas: gas, To: to,
			Value: value, Data: t.Data,
			AccessList: types.AccessList{{Address: sink, StorageKeys: []common.Hash{{}}}},
		}
	case KindDynamicFee:
		inner = &types.DynamicFeeTx{
			ChainID: chainID, Nonce: nonce, GasTipCap: fees.TipCap, GasFeeCap: fees.FeeCap,
			Gas: gas, To: to, Value: value, Data: t.Data,
		}
	case KindBlob:
		n := t.Blobs
		if n == 0 {
			n = 1
		}

		version := types.BlobSidecarVersion1
		if t.SidecarVersion != nil {
			version = *t.SidecarVersion
		}

		sidecar, err := f.sidecar(n, version, uint64(from.Index)<<32|nonce)
		if err != nil {
			return nil, err
		}

		blobFee := fees.BlobFeeCap
		if blobFee == nil {
			blobFee = DefaultFees().BlobFeeCap
		}

		inner = &types.BlobTx{
			ChainID: u256(chainID), Nonce: nonce, GasTipCap: u256(fees.TipCap),
			GasFeeCap: u256(fees.FeeCap), Gas: gas, To: *to, Value: u256(value), Data: t.Data,
			BlobFeeCap: u256(blobFee), BlobHashes: sidecar.BlobHashes(), Sidecar: sidecar,
		}
	case KindSetCode:
		auths, err := f.authorizations(from, t, chainID)
		if err != nil {
			return nil, err
		}

		inner = &types.SetCodeTx{
			ChainID: u256(chainID), Nonce: nonce, GasTipCap: u256(fees.TipCap),
			GasFeeCap: u256(fees.FeeCap), Gas: gas, To: *to, Value: u256(value), Data: t.Data,
			AuthList: auths,
		}
	default:
		return nil, fmt.Errorf("unknown tx kind %q", t.Kind)
	}

	tx, err := types.SignNewTx(from.Key, signer, inner)
	if err != nil {
		return nil, fmt.Errorf("signing a %s tx: %w", t.Kind, err)
	}

	return tx, nil
}

// authorizations signs AuthCount EIP-7702 authorizations by fresh keys (not
// the sender), each delegating to the sink address.
func (f *factory) authorizations(from *Account, t Template, chainID *big.Int,
) ([]types.SetCodeAuthorization, error) {
	if t.NoAuth {
		return []types.SetCodeAuthorization{}, nil
	}

	n := t.AuthCount
	if n == 0 {
		n = 1
	}

	out := make([]types.SetCodeAuthorization, 0, n)

	for i := range n {
		key := DeriveKey(f.seed+"/authority", from.Index*64+i)

		auth, err := types.SignSetCode(key, types.SetCodeAuthorization{
			ChainID: *u256(chainID), Address: sink, Nonce: 0,
		})
		if err != nil {
			return nil, fmt.Errorf("signing an authorization: %w", err)
		}

		out = append(out, auth)
	}

	return out, nil
}

// sidecar returns a blob sidecar with n blobs. Blobs are cached per salt
// bucket, since KZG proofs are slow and the content does not matter.
func (f *factory) sidecar(n int, version byte, salt uint64) (*types.BlobTxSidecar, error) {
	// Keep 16 distinct blob contents. Distinct txs from one sender with the
	// same blobs are fine: the pool keys blob txs by hash, not by content.
	key := blobKey{n: n, version: version, salt: salt % 16}

	f.blobMu.Lock()
	defer f.blobMu.Unlock()

	if sc, ok := f.blobCache[key]; ok {
		return sc, nil
	}

	blobs := make([]kzg4844.Blob, n)
	commitments := make([]kzg4844.Commitment, n)
	proofs := make([]kzg4844.Proof, 0, n*kzg4844.CellProofsPerBlob)

	for i := range n {
		fillBlob(&blobs[i], key.salt*64+uint64(i))

		c, err := kzg4844.BlobToCommitment(&blobs[i])
		if err != nil {
			return nil, fmt.Errorf("blob commitment: %w", err)
		}

		commitments[i] = c

		if version == types.BlobSidecarVersion0 {
			p, err := kzg4844.ComputeBlobProof(&blobs[i], c)
			if err != nil {
				return nil, fmt.Errorf("blob proof: %w", err)
			}

			proofs = append(proofs, p)

			continue
		}

		cellProofs, err := kzg4844.ComputeCellProofs(&blobs[i])
		if err != nil {
			return nil, fmt.Errorf("cell proofs: %w", err)
		}

		proofs = append(proofs, cellProofs...)
	}

	sc := types.NewBlobTxSidecar(version, blobs, commitments, proofs)
	f.blobCache[key] = sc

	return sc, nil
}

// fillBlob writes valid field elements: the top byte of each 32-byte
// element stays zero, so every element is below the BLS modulus.
func fillBlob(b *kzg4844.Blob, salt uint64) {
	var seed [8]byte

	binary.BigEndian.PutUint64(seed[:], salt)

	for i := 0; i < len(b); i += 32 {
		h := crypto.Keccak256(seed[:], []byte{byte(i >> 8), byte(i)})
		copy(b[i+1:i+32], h[:31])
	}
}

// intrinsicGuess returns a gas limit that covers the intrinsic gas of t. It
// is exact for a tx without data, so the cost the pool checks (gas limit *
// fee cap) is what a scenario expects.
func intrinsicGuess(t Template) uint64 {
	gas := uint64(21_000)

	switch t.Kind {
	case KindAccessList:
		gas += 2400 + 1900
	case KindSetCode:
		n := t.AuthCount
		if n == 0 && !t.NoAuth {
			n = 1
		}

		gas += uint64(n) * 25_000
	}

	if len(t.Data) == 0 {
		return gas
	}

	if t.To == nil && t.Kind != KindBlob && t.Kind != KindSetCode {
		gas += 32_000 + uint64(len(t.Data)+31)/32*2
	}

	// Calldata at the EIP-7623 floor price covers both the standard and the
	// floor cost.
	return gas + uint64(len(t.Data))*40 + 5_000
}

func u256(v *big.Int) *uint256.Int {
	if v == nil {
		return new(uint256.Int)
	}

	u, _ := uint256.FromBig(v)

	return u
}

// NewAccount wraps a key that is not one of the genesis accounts (for
// example an account a scenario funds itself). Its nonce starts at 0.
func NewAccount(key *ecdsa.PrivateKey) *Account {
	return &Account{Index: -1, Key: key, Address: crypto.PubkeyToAddress(key.PublicKey)}
}
